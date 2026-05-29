package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VectorSigmaOmega/photon/internal/config"
	dbrepo "github.com/VectorSigmaOmega/photon/internal/db"
	"github.com/VectorSigmaOmega/photon/internal/imageproc"
	"github.com/VectorSigmaOmega/photon/internal/observability"
	"github.com/VectorSigmaOmega/photon/internal/queue"
	"github.com/VectorSigmaOmega/photon/internal/storage"
)

const (
	staleRecoveryBatchSize = 100
	staleRecoveryInterval  = 30 * time.Second
	staleRecoveryReason    = "worker lease expired before completion"
)

type Runner struct {
	cfg        config.WorkerConfig
	jobs       *dbrepo.JobsRepository
	log        *slog.Logger
	maxRetries int
	metrics    *observability.WorkerMetrics
	db         *sql.DB
	processor  *imageproc.Processor
	queue      *queue.RedisQueue
	storage    *storage.MinIOClient
}

func NewRunner(
	cfg config.WorkerConfig,
	log *slog.Logger,
	db *sql.DB,
	redisQueue *queue.RedisQueue,
	maxRetries int,
	storageClient *storage.MinIOClient,
	metrics *observability.WorkerMetrics,
) *Runner {
	return &Runner{
		cfg:        cfg,
		jobs:       dbrepo.NewJobsRepository(db),
		log:        log,
		maxRetries: maxRetries,
		metrics:    metrics,
		db:         db,
		processor:  imageproc.NewProcessor(),
		queue:      redisQueue,
		storage:    storageClient,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	var inFlight int64
	r.log.Info(
		"worker runtime initialized",
		"concurrency", r.cfg.Concurrency,
		"poll_timeout", r.cfg.PollTimeout.String(),
		"queue_key", r.queue.QueueKey(),
		"dlq_key", r.queue.DLQKey(),
	)

	var wg sync.WaitGroup
	slots := make(chan struct{}, r.cfg.Concurrency)
	r.refreshQueueMetrics(ctx)
	if r.metrics != nil {
		r.metrics.SetConcurrencyCapacity(r.queue.QueueKey(), r.cfg.Concurrency)
		r.metrics.SetConcurrencyInUse(0)
	}
	go r.pollQueueMetrics(ctx)
	go r.recoverStaleJobsLoop(ctx)
	r.recoverStaleJobs(ctx)

	for {
		select {
		case <-ctx.Done():
			r.log.Info("worker shutdown requested")
			wg.Wait()
			return nil
		case slots <- struct{}{}:
		}

		jobID, ok, err := r.queue.Dequeue(ctx, r.cfg.PollTimeout)
		if err != nil {
			<-slots
			if ctx.Err() != nil {
				r.log.Info("worker shutdown requested")
				wg.Wait()
				return nil
			}

			r.log.Error("dequeue job", "err", err)
			continue
		}

		if !ok {
			<-slots
			continue
		}

		wg.Add(1)
		active := atomic.AddInt64(&inFlight, 1)
		if r.metrics != nil {
			r.metrics.SetConcurrencyInUse(active)
		}
		go func(jobID string) {
			defer wg.Done()
			defer func() {
				active := atomic.AddInt64(&inFlight, -1)
				if r.metrics != nil {
					r.metrics.SetConcurrencyInUse(active)
				}
				<-slots
			}()

			if err := r.processJob(jobID); err != nil {
				r.log.Error("process job", "job_id", jobID, "err", err)
			}
		}(jobID)
	}
}

func (r *Runner) processJob(jobID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.JobTimeout)
	defer cancel()

	job, attempt, err := r.jobs.StartJobAttempt(ctx, jobID)
	if errors.Is(err, dbrepo.ErrJobNotFound) {
		r.log.Warn("skipping queued job because metadata is missing", "job_id", jobID)
		return nil
	}

	if errors.Is(err, dbrepo.ErrInvalidJobStatus) {
		r.log.Info("skipping queued job because it is no longer claimable", "job_id", jobID)
		return nil
	}

	if err != nil {
		return err
	}

	jobLog := r.log.With("job_id", job.ID, "attempt", attempt.AttemptNumber)
	startedAt := time.Now()

	jobLog.Info(
		"processing job",
		"source_object_key", job.SourceObjectKey,
		"output_format", job.OutputFormat,
	)

	outputs, err := r.runProcessing(ctx, job)
	if err != nil {
		return r.handleProcessingFailure(ctx, jobLog, job, attempt.AttemptNumber, startedAt, err)
	}

	if err := r.jobs.CompleteJobAttemptWithOutputs(ctx, job.ID, attempt.AttemptNumber, outputs); err != nil {
		return r.handleProcessingFailure(ctx, jobLog, job, attempt.AttemptNumber, startedAt, fmt.Errorf("persist completion: %w", err))
	}

	if r.metrics != nil {
		r.metrics.IncJobOutcome(string(dbrepo.JobStatusCompleted))
		r.metrics.ObserveProcessingDuration(string(dbrepo.JobStatusCompleted), time.Since(startedAt))
	}

	jobLog.Info("job completed", "output_count", len(outputs))
	return nil
}

func (r *Runner) runProcessing(
	ctx context.Context,
	job dbrepo.Job,
) ([]dbrepo.ReplaceJobOutputsInput, error) {
	transforms, err := imageproc.ParseTransforms(job.RequestedTransforms)
	if err != nil {
		return nil, fmt.Errorf("parse requested transforms: %w", err)
	}

	workDir, err := os.MkdirTemp("", "photon-worker-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	sourcePath := filepath.Join(workDir, "source"+filepath.Ext(job.SourceObjectKey))
	if filepath.Ext(sourcePath) == "" {
		sourcePath += ".img"
	}

	if err := r.storage.DownloadFile(ctx, job.SourceObjectKey, sourcePath); err != nil {
		return nil, fmt.Errorf("download source object: %w", err)
	}

	outputDir := filepath.Join(workDir, "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}

	generated, err := r.processor.Generate(ctx, sourcePath, outputDir, job.OutputFormat, transforms)
	if err != nil {
		return nil, err
	}

	outputs := make([]dbrepo.ReplaceJobOutputsInput, 0, len(generated))
	for _, generatedFile := range generated {
		objectKey := fmt.Sprintf("outputs/%s/%s.%s", job.ID, generatedFile.VariantName, imageproc.NormalizeFormat(job.OutputFormat))

		stored, err := r.storage.UploadFile(ctx, objectKey, generatedFile.Path, generatedFile.ContentType)
		if err != nil {
			return nil, fmt.Errorf("upload output %s: %w", generatedFile.VariantName, err)
		}

		outputs = append(outputs, dbrepo.ReplaceJobOutputsInput{
			VariantName: generatedFile.VariantName,
			ObjectKey:   stored.Key,
			ContentType: stored.ContentType,
			SizeBytes:   stored.SizeBytes,
		})
	}

	if len(outputs) == 0 {
		return nil, errors.New("no outputs generated")
	}

	return outputs, nil
}

func (r *Runner) handleProcessingFailure(
	ctx context.Context,
	jobLog *slog.Logger,
	job dbrepo.Job,
	attemptNumber int,
	startedAt time.Time,
	processErr error,
) error {
	reason := processErr.Error()

	if attemptNumber >= r.maxRetries {
		if err := r.jobs.DeadLetterJob(ctx, job.ID, attemptNumber, reason); err != nil {
			return fmt.Errorf("processing failed: %w; dead-letter job: %v", processErr, err)
		}

		if err := r.queue.EnqueueDLQ(ctx, queue.DLQEntry{
			Attempt:       attemptNumber,
			FailedAt:      time.Now().UTC(),
			FailureReason: reason,
			JobID:         job.ID,
		}); err != nil {
			return fmt.Errorf("processing failed: %w; enqueue dlq entry: %v", processErr, err)
		}

		if r.metrics != nil {
			r.metrics.IncJobOutcome(string(dbrepo.JobStatusDeadLettered))
			r.metrics.ObserveProcessingDuration(string(dbrepo.JobStatusDeadLettered), time.Since(startedAt))
		}

		jobLog.Error(
			"job dead-lettered",
			"err", processErr,
		)
		return nil
	}

	if err := r.jobs.RequeueJobAfterFailure(ctx, job.ID, attemptNumber, reason); err != nil {
		return fmt.Errorf("processing failed: %w; requeue job state: %v", processErr, err)
	}

	if err := r.queue.Enqueue(ctx, job.ID); err != nil {
		compensationReason := fmt.Sprintf("%s; retry enqueue failed: %v", reason, err)
		if compensateErr := r.jobs.MarkJobFailedIfQueued(ctx, job.ID, compensationReason); compensateErr != nil {
			return fmt.Errorf("processing failed: %w; requeue job: %v; restore failed state: %v", processErr, err, compensateErr)
		}

		if r.metrics != nil {
			r.metrics.IncJobOutcome(string(dbrepo.JobStatusFailed))
			r.metrics.ObserveProcessingDuration(string(dbrepo.JobStatusFailed), time.Since(startedAt))
		}

		jobLog.Error(
			"automatic retry enqueue failed; job left failed",
			"retry_enqueue_err", err,
		)
		return nil
	}

	if r.metrics != nil {
		r.metrics.IncRetry()
		r.metrics.ObserveProcessingDuration("retried", time.Since(startedAt))
	}

	jobLog.Warn(
		"job requeued after failure",
		"max_retries", r.maxRetries,
		"err", processErr,
	)
	return nil
}

func (r *Runner) recoverStaleJobsLoop(ctx context.Context) {
	ticker := time.NewTicker(staleRecoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.recoverStaleJobs(ctx)
		}
	}
}

func (r *Runner) recoverStaleJobs(ctx context.Context) {
	cutoff := time.Now().Add(-r.cfg.JobTimeout)

	for {
		recovered, err := r.jobs.RecoverStaleProcessingJobs(ctx, cutoff, staleRecoveryBatchSize, staleRecoveryReason)
		if err != nil {
			r.log.Warn("recover stale processing jobs", "err", err, "cutoff", cutoff.UTC().Format(time.RFC3339))
			return
		}

		if len(recovered) == 0 {
			return
		}

		for _, jobID := range recovered {
			if err := r.queue.Enqueue(ctx, jobID); err != nil {
				compensationReason := fmt.Sprintf("%s; recovery enqueue failed: %v", staleRecoveryReason, err)
				if compensateErr := r.jobs.MarkJobFailedIfQueued(ctx, jobID, compensationReason); compensateErr != nil {
					r.log.Error(
						"requeue recovered job",
						"job_id", jobID,
						"err", err,
						"compensate_err", compensateErr,
					)
					continue
				}

				if r.metrics != nil {
					r.metrics.IncJobOutcome(string(dbrepo.JobStatusFailed))
				}

				r.log.Error(
					"recovered job could not be re-enqueued; job left failed",
					"job_id", jobID,
					"err", err,
				)
				continue
			}

			r.log.Warn(
				"recovered stale processing job",
				"job_id", jobID,
			)
		}

		if len(recovered) < staleRecoveryBatchSize {
			return
		}
	}
}

func (r *Runner) pollQueueMetrics(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refreshQueueMetrics(ctx)
		}
	}
}

func (r *Runner) refreshQueueMetrics(ctx context.Context) {
	if r.metrics == nil {
		return
	}

	sampleCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	queueDepth, err := r.queue.QueueDepth(sampleCtx)
	if err != nil {
		r.log.Warn("sample queue depth", "queue_key", r.queue.QueueKey(), "err", err)
		return
	}

	dlqDepth, err := r.queue.DLQDepth(sampleCtx)
	if err != nil {
		r.log.Warn("sample dlq depth", "dlq_key", r.queue.DLQKey(), "err", err)
		return
	}

	r.metrics.SetQueueDepth(queueDepth)
	r.metrics.SetDLQDepth(dlqDepth)
}
