package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/VectorSigmaOmega/photon/internal/config"
	dbrepo "github.com/VectorSigmaOmega/photon/internal/db"
	"github.com/VectorSigmaOmega/photon/internal/imageproc"
	"github.com/VectorSigmaOmega/photon/internal/observability"
	"github.com/VectorSigmaOmega/photon/internal/queue"
	"github.com/VectorSigmaOmega/photon/internal/storage"
	"github.com/google/uuid"
)

type Server struct {
	db                 *sql.DB
	downloadPresignTTL time.Duration
	jobs               *dbrepo.JobsRepository
	log                *slog.Logger
	metrics            *observability.Metrics
	queue              *queue.RedisQueue
	server             *http.Server
	storage            *storage.MinIOClient
	uploadPresignTTL   time.Duration
}

func New(
	cfg config.HTTPServerConfig,
	log *slog.Logger,
	database *sql.DB,
	metrics *observability.Metrics,
	redisQueue *queue.RedisQueue,
	storageClient *storage.MinIOClient,
	storageCfg config.StorageConfig,
) *Server {
	s := &Server{
		db:                 database,
		downloadPresignTTL: storageCfg.DownloadPresignTTL,
		jobs:               dbrepo.NewJobsRepository(database),
		log:                log,
		metrics:            metrics,
		queue:              redisQueue,
		storage:            storageClient,
		uploadPresignTTL:   storageCfg.UploadPresignTTL,
	}

	rateLimiter := newRateLimiter(log, cfg.RateLimit)
	frontend := frontendHandler()

	mux := http.NewServeMux()
	mux.Handle("/", frontend)
	mux.Handle("GET /healthz", metrics.Instrument("healthz", http.HandlerFunc(s.handleHealthz)))
	mux.Handle("GET /readyz", metrics.Instrument("readyz", http.HandlerFunc(s.handleReadyz)))
	mux.Handle("GET /metrics", metrics.Handler())

	mux.Handle(
		"POST /v1/uploads/presign",
		metrics.Instrument(
			"uploads_presign",
			rateLimiter.wrap(
				"uploads_presign",
				cfg.RateLimit.PresignRequestsPerMinute,
				cfg.RateLimit.PresignBurst,
				http.HandlerFunc(s.handlePresignUpload),
			),
		),
	)
	mux.Handle(
		"POST /v1/jobs",
		metrics.Instrument(
			"jobs_create",
			rateLimiter.wrap(
				"jobs_create",
				cfg.RateLimit.CreateRequestsPerMinute,
				cfg.RateLimit.CreateBurst,
				http.HandlerFunc(s.handleCreateJob),
			),
		),
	)
	mux.Handle(
		"POST /v1/jobs/{id}/retry",
		metrics.Instrument(
			"jobs_retry",
			rateLimiter.wrap(
				"jobs_retry",
				cfg.RateLimit.RetryRequestsPerMinute,
				cfg.RateLimit.RetryBurst,
				http.HandlerFunc(s.handleRetryJob),
			),
		),
	)
	mux.Handle("GET /v1/jobs/{id}", metrics.Instrument("jobs_get", http.HandlerFunc(s.handleGetJob)))
	mux.Handle("GET /v1/jobs/{id}/results", metrics.Instrument("jobs_results", http.HandlerFunc(s.handleGetResults)))

	s.server = &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return s
}

func (s *Server) ListenAndServe() error {
	s.log.Info("api server listening", "addr", s.server.Addr)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"reason": "postgres_unavailable",
		})
		return
	}

	if err := s.queue.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"reason": "redis_unavailable",
		})
		return
	}

	if err := s.storage.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_ready",
			"reason": "storage_unavailable",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handlePresignUpload(w http.ResponseWriter, r *http.Request) {
	var request presignUploadRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := request.validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	request.normalize()

	objectKey := buildUploadObjectKey(request.FileName, request.ContentType)
	uploadURL, err := s.storage.PresignUploadURL(r.Context(), objectKey, s.uploadPresignTTL)
	if err != nil {
		s.log.Error("presign upload url", "object_key", objectKey, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create upload url"})
		return
	}

	s.log.Info(
		"presigned upload created",
		"object_key", objectKey,
		"content_type", request.ContentType,
		"expires_in_seconds", int(s.uploadPresignTTL.Seconds()),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"content_type":       request.ContentType,
		"expires_in_seconds": int(s.uploadPresignTTL.Seconds()),
		"method":             "PUT",
		"object_key":         objectKey,
		"upload_url":         uploadURL,
	})
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var request createJobRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := request.validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	request.normalize()

	transforms, err := json.Marshal(request.RequestedTransforms)
	if err != nil {
		s.log.Error("marshal requested transforms", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode transforms"})
		return
	}

	job, err := s.jobs.CreateJob(r.Context(), dbrepo.CreateJobInput{
		SourceObjectKey:     request.SourceObjectKey,
		RequestedTransforms: transforms,
		OutputFormat:        request.OutputFormat,
	})
	if err != nil {
		s.log.Error("create job", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create job"})
		return
	}

	enqueueCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.queue.Enqueue(enqueueCtx, job.ID); err != nil {
		s.log.Error("enqueue job", "job_id", job.ID, "err", err)
		s.markJobFailed(job.ID, "enqueue_failed")
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":  "failed to enqueue job",
			"job_id": job.ID,
		})
		return
	}

	s.log.Info(
		"job created",
		"job_id", job.ID,
		"source_object_key", job.SourceObjectKey,
		"output_format", job.OutputFormat,
		"transform_count", len(request.RequestedTransforms),
	)

	writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobs.GetJob(r.Context(), r.PathValue("id"))
	if errors.Is(err, dbrepo.ErrJobNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	if err != nil {
		s.log.Error("get job", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch job"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleGetResults(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if _, err := s.jobs.GetJob(r.Context(), jobID); errors.Is(err, dbrepo.ErrJobNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	} else if err != nil {
		s.log.Error("get job before listing results", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch job"})
		return
	}

	outputs, err := s.jobs.ListOutputs(r.Context(), jobID)
	if err != nil {
		s.log.Error("list job outputs", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch job outputs"})
		return
	}

	resultOutputs := make([]jobResultOutput, 0, len(outputs))
	for _, output := range outputs {
		downloadURL, err := s.storage.PresignDownloadURL(r.Context(), output.ObjectKey, s.downloadPresignTTL)
		if err != nil {
			s.log.Error("presign output download url", "object_key", output.ObjectKey, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create output download url"})
			return
		}

		resultOutputs = append(resultOutputs, jobResultOutput{
			ID:               output.ID,
			JobID:            output.JobID,
			VariantName:      output.VariantName,
			ObjectKey:        output.ObjectKey,
			ContentType:      output.ContentType,
			SizeBytes:        output.SizeBytes,
			CreatedAt:        output.CreatedAt,
			DownloadURL:      downloadURL,
			ExpiresInSeconds: int(s.downloadPresignTTL.Seconds()),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"outputs": resultOutputs})
}

func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")

	job, err := s.jobs.ResetJobToQueued(r.Context(), jobID)
	if errors.Is(err, dbrepo.ErrJobNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	if errors.Is(err, dbrepo.ErrRetryNotAllowed) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	if err != nil {
		s.log.Error("reset job to queued", "job_id", jobID, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retry job"})
		return
	}

	enqueueCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.queue.Enqueue(enqueueCtx, job.ID); err != nil {
		s.log.Error("enqueue retried job", "job_id", job.ID, "err", err)
		s.markJobFailed(job.ID, "retry_enqueue_failed")
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":  "failed to enqueue retried job",
			"job_id": job.ID,
		})
		return
	}

	s.log.Info("job requeued manually", "job_id", job.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
}

func notImplemented(message string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": message,
		})
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) markJobFailed(jobID, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.jobs.MarkJobFailed(ctx, jobID, reason); err != nil {
		s.log.Error("mark job failed", "job_id", jobID, "err", err)
	}
}

type createJobRequest struct {
	SourceObjectKey     string                `json:"source_object_key"`
	RequestedTransforms []imageproc.Transform `json:"requested_transforms"`
	OutputFormat        string                `json:"output_format"`
}

type presignUploadRequest struct {
	ContentType string `json:"content_type"`
	FileName    string `json:"file_name,omitempty"`
}

type jobResultOutput struct {
	ID               int64     `json:"id"`
	JobID            string    `json:"job_id"`
	VariantName      string    `json:"variant_name"`
	ObjectKey        string    `json:"object_key"`
	ContentType      string    `json:"content_type"`
	SizeBytes        int64     `json:"size_bytes"`
	CreatedAt        time.Time `json:"created_at"`
	DownloadURL      string    `json:"download_url"`
	ExpiresInSeconds int       `json:"expires_in_seconds"`
}

func (r createJobRequest) validate() error {
	if strings.TrimSpace(r.SourceObjectKey) == "" {
		return errors.New("source_object_key is required")
	}

	if _, err := imageproc.ContentTypeForFormat(r.OutputFormat); err != nil {
		return errors.New("output_format must be one of jpg, png, webp, avif")
	}

	if err := imageproc.ValidateTransforms(r.RequestedTransforms); err != nil {
		return err
	}

	return nil
}

func (r *createJobRequest) normalize() {
	r.SourceObjectKey = strings.TrimSpace(r.SourceObjectKey)
	r.OutputFormat = imageproc.NormalizeFormat(r.OutputFormat)
	imageproc.NormalizeTransforms(r.RequestedTransforms)
}

func (r presignUploadRequest) validate() error {
	switch normalizeContentType(r.ContentType) {
	case "image/jpeg", "image/png", "image/webp", "image/avif":
		return nil
	default:
		return errors.New("content_type must be one of image/jpeg, image/png, image/webp, image/avif")
	}
}

func (r *presignUploadRequest) normalize() {
	r.ContentType = normalizeContentType(r.ContentType)
	r.FileName = strings.TrimSpace(filepath.Base(r.FileName))
}

func buildUploadObjectKey(fileName, contentType string) string {
	extension := extensionForContentType(contentType)
	baseName := "upload"
	if trimmed := strings.TrimSuffix(fileName, filepath.Ext(fileName)); strings.TrimSpace(trimmed) != "" {
		baseName = sanitizeObjectKeyComponent(trimmed)
	}

	return "uploads/" + uuid.NewString() + "/" + baseName + extension
}

func extensionForContentType(contentType string) string {
	switch normalizeContentType(contentType) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	default:
		return ".img"
	}
}

func normalizeContentType(contentType string) string {
	return strings.ToLower(strings.TrimSpace(contentType))
}

func sanitizeObjectKeyComponent(input string) string {
	var builder strings.Builder
	lastWasDash := false

	for _, r := range strings.ToLower(strings.TrimSpace(input)) {
		isAlphaNumeric := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNumeric {
			builder.WriteRune(r)
			lastWasDash = false
			continue
		}

		if !lastWasDash {
			builder.WriteByte('-')
			lastWasDash = true
		}
	}

	sanitized := strings.Trim(builder.String(), "-")
	if sanitized == "" {
		return "upload"
	}

	if len(sanitized) > 80 {
		return sanitized[:80]
	}

	return sanitized
}
