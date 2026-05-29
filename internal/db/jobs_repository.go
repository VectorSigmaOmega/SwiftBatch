package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type JobsRepository struct {
	db *sql.DB
}

type CreateJobInput struct {
	SourceObjectKey     string
	RequestedTransforms json.RawMessage
	OutputFormat        string
}

type ReplaceJobOutputsInput struct {
	VariantName string
	ObjectKey   string
	ContentType string
	SizeBytes   int64
}

func NewJobsRepository(db *sql.DB) *JobsRepository {
	return &JobsRepository{db: db}
}

func (r *JobsRepository) CreateJob(ctx context.Context, input CreateJobInput) (Job, error) {
	const query = `
INSERT INTO jobs (status, source_object_key, requested_transforms, output_format)
VALUES ($1, $2, $3, $4)
RETURNING id, status, source_object_key, requested_transforms, output_format, COALESCE(failure_reason, ''), created_at, updated_at
`

	var job Job
	err := r.db.QueryRowContext(
		ctx,
		query,
		JobStatusQueued,
		input.SourceObjectKey,
		input.RequestedTransforms,
		input.OutputFormat,
	).Scan(
		&job.ID,
		&job.Status,
		&job.SourceObjectKey,
		&job.RequestedTransforms,
		&job.OutputFormat,
		&job.FailureReason,
		&job.CreatedAt,
		&job.UpdatedAt,
	)

	return job, err
}

func (r *JobsRepository) GetJob(ctx context.Context, id string) (Job, error) {
	const query = `
SELECT id, status, source_object_key, requested_transforms, output_format, COALESCE(failure_reason, ''), created_at, updated_at
FROM jobs
WHERE id = $1
`

	var job Job
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&job.ID,
		&job.Status,
		&job.SourceObjectKey,
		&job.RequestedTransforms,
		&job.OutputFormat,
		&job.FailureReason,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}

	return job, err
}

func (r *JobsRepository) ListOutputs(ctx context.Context, jobID string) ([]JobOutput, error) {
	const query = `
SELECT id, job_id, variant_name, object_key, content_type, size_bytes, created_at
FROM job_outputs
WHERE job_id = $1
ORDER BY id ASC
`

	rows, err := r.db.QueryContext(ctx, query, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	outputs := make([]JobOutput, 0)
	for rows.Next() {
		var output JobOutput
		if err := rows.Scan(
			&output.ID,
			&output.JobID,
			&output.VariantName,
			&output.ObjectKey,
			&output.ContentType,
			&output.SizeBytes,
			&output.CreatedAt,
		); err != nil {
			return nil, err
		}

		outputs = append(outputs, output)
	}

	return outputs, rows.Err()
}

func (r *JobsRepository) ListCleanupCandidates(ctx context.Context, cutoff time.Time, limit int) ([]CleanupCandidate, error) {
	const jobsQuery = `
SELECT id, status, source_object_key, updated_at
FROM jobs
WHERE status IN ('completed', 'failed', 'dead_lettered')
  AND updated_at < $1
ORDER BY updated_at ASC
LIMIT $2
`

	rows, err := r.db.QueryContext(ctx, jobsQuery, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]CleanupCandidate, 0, limit)
	for rows.Next() {
		var candidate CleanupCandidate
		if err := rows.Scan(
			&candidate.ID,
			&candidate.Status,
			&candidate.SourceObjectKey,
			&candidate.UpdatedAt,
		); err != nil {
			return nil, err
		}

		candidates = append(candidates, candidate)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	for index := range candidates {
		outputs, err := r.ListOutputs(ctx, candidates[index].ID)
		if err != nil {
			return nil, err
		}

		candidates[index].OutputObjectKeys = make([]string, 0, len(outputs))
		for _, output := range outputs {
			candidates[index].OutputObjectKeys = append(candidates[index].OutputObjectKeys, output.ObjectKey)
		}
	}

	return candidates, nil
}

func (r *JobsRepository) DeleteJob(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = $1`, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrJobNotFound
	}

	return nil
}

func (r *JobsRepository) ReplaceJobOutputs(ctx context.Context, jobID string, outputs []ReplaceJobOutputsInput) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := replaceJobOutputsTx(ctx, tx, jobID, outputs); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *JobsRepository) CompleteJobAttemptWithOutputs(
	ctx context.Context,
	id string,
	attemptNumber int,
	outputs []ReplaceJobOutputsInput,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	job, err := getJobForUpdate(ctx, tx, id)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	if job.Status != JobStatusProcessing {
		_ = tx.Rollback()
		return ErrInvalidJobStatus
	}

	if err := replaceJobOutputsTx(ctx, tx, id, outputs); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := finishAttemptRecord(ctx, tx, id, attemptNumber, JobStatusCompleted, ""); err != nil {
		_ = tx.Rollback()
		return err
	}

	if _, err := updateJobState(ctx, tx, id, JobStatusCompleted, ""); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func replaceJobOutputsTx(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	outputs []ReplaceJobOutputsInput,
) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM job_outputs WHERE job_id = $1`, jobID); err != nil {
		return err
	}

	const query = `
INSERT INTO job_outputs (job_id, variant_name, object_key, content_type, size_bytes)
VALUES ($1, $2, $3, $4, $5)
`

	for _, output := range outputs {
		if _, err := tx.ExecContext(
			ctx,
			query,
			jobID,
			output.VariantName,
			output.ObjectKey,
			output.ContentType,
			output.SizeBytes,
		); err != nil {
			return err
		}
	}

	return nil
}

func (r *JobsRepository) MarkJobFailed(ctx context.Context, id, reason string) error {
	const query = `
UPDATE jobs
SET status = $2, failure_reason = $3, updated_at = NOW()
WHERE id = $1
`

	result, err := r.db.ExecContext(ctx, query, id, JobStatusFailed, reason)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrJobNotFound
	}

	return nil
}

func (r *JobsRepository) MarkJobFailedIfQueued(ctx context.Context, id, reason string) error {
	return r.updateJobStateIfCurrentStatus(ctx, id, JobStatusQueued, JobStatusFailed, reason)
}

func (r *JobsRepository) ResetJobToQueued(ctx context.Context, id string) (Job, error) {
	job, err := r.GetJob(ctx, id)
	if err != nil {
		return Job{}, err
	}

	if job.Status != JobStatusFailed && job.Status != JobStatusDeadLettered {
		return Job{}, ErrRetryNotAllowed
	}

	const query = `
UPDATE jobs
SET status = $2, failure_reason = NULL, updated_at = NOW()
WHERE id = $1
RETURNING id, status, source_object_key, requested_transforms, output_format, COALESCE(failure_reason, ''), created_at, updated_at
`

	var updated Job
	if err := r.db.QueryRowContext(ctx, query, id, JobStatusQueued).Scan(
		&updated.ID,
		&updated.Status,
		&updated.SourceObjectKey,
		&updated.RequestedTransforms,
		&updated.OutputFormat,
		&updated.FailureReason,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, ErrInvalidJobStatus
		}

		return Job{}, err
	}

	return updated, nil
}

func (r *JobsRepository) StartJobAttempt(ctx context.Context, id string) (Job, JobAttempt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, JobAttempt{}, err
	}

	job, err := getJobForUpdate(ctx, tx, id)
	if err != nil {
		_ = tx.Rollback()
		return Job{}, JobAttempt{}, err
	}

	if job.Status != JobStatusQueued {
		_ = tx.Rollback()
		return Job{}, JobAttempt{}, ErrInvalidJobStatus
	}

	attemptNumber, err := nextAttemptNumber(ctx, tx, id)
	if err != nil {
		_ = tx.Rollback()
		return Job{}, JobAttempt{}, err
	}

	attempt, err := insertJobAttempt(ctx, tx, id, attemptNumber, JobStatusProcessing)
	if err != nil {
		_ = tx.Rollback()
		return Job{}, JobAttempt{}, err
	}

	job, err = updateJobState(ctx, tx, id, JobStatusProcessing, "")
	if err != nil {
		_ = tx.Rollback()
		return Job{}, JobAttempt{}, err
	}

	if err := tx.Commit(); err != nil {
		return Job{}, JobAttempt{}, err
	}

	return job, attempt, nil
}

func (r *JobsRepository) CompleteJobAttempt(ctx context.Context, id string, attemptNumber int) error {
	return r.finishJobAttempt(ctx, id, attemptNumber, JobStatusCompleted, "")
}

func (r *JobsRepository) FailJobAttempt(ctx context.Context, id string, attemptNumber int, reason string) error {
	return r.finishJobAttempt(ctx, id, attemptNumber, JobStatusFailed, reason)
}

func (r *JobsRepository) RequeueJobAfterFailure(ctx context.Context, id string, attemptNumber int, reason string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	job, err := getJobForUpdate(ctx, tx, id)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	if job.Status != JobStatusProcessing {
		_ = tx.Rollback()
		return ErrInvalidJobStatus
	}

	if err := finishAttemptRecord(ctx, tx, id, attemptNumber, JobStatusFailed, reason); err != nil {
		_ = tx.Rollback()
		return err
	}

	if _, err := updateJobState(ctx, tx, id, JobStatusQueued, ""); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *JobsRepository) DeadLetterJob(ctx context.Context, id string, attemptNumber int, reason string) error {
	return r.finishJobAttempt(ctx, id, attemptNumber, JobStatusDeadLettered, reason)
}

func (r *JobsRepository) RecoverStaleProcessingJobs(
	ctx context.Context,
	cutoff time.Time,
	limit int,
	reason string,
) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	const query = `
SELECT j.id, a.attempt_number
FROM jobs j
JOIN job_attempts a
  ON a.job_id = j.id
 AND a.status = $2
WHERE j.status = $2
  AND a.started_at < $1
ORDER BY a.started_at ASC
LIMIT $3
FOR UPDATE OF j SKIP LOCKED
`

	rows, err := tx.QueryContext(ctx, query, cutoff, JobStatusProcessing, limit)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	type staleAttempt struct {
		jobID         string
		attemptNumber int
	}

	stale := make([]staleAttempt, 0, limit)
	for rows.Next() {
		var attempt staleAttempt
		if err := rows.Scan(&attempt.jobID, &attempt.attemptNumber); err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		stale = append(stale, attempt)
	}

	if err := rows.Err(); err != nil {
		_ = rows.Close()
		_ = tx.Rollback()
		return nil, err
	}

	if err := rows.Close(); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	recovered := make([]string, 0, len(stale))
	for _, attempt := range stale {
		if err := finishAttemptRecord(ctx, tx, attempt.jobID, attempt.attemptNumber, JobStatusFailed, reason); err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		if _, err := updateJobState(ctx, tx, attempt.jobID, JobStatusQueued, ""); err != nil {
			_ = tx.Rollback()
			return nil, err
		}

		recovered = append(recovered, attempt.jobID)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return recovered, nil
}

func (r *JobsRepository) finishJobAttempt(
	ctx context.Context,
	id string,
	attemptNumber int,
	status JobStatus,
	reason string,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	job, err := getJobForUpdate(ctx, tx, id)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	if job.Status != JobStatusProcessing {
		_ = tx.Rollback()
		return ErrInvalidJobStatus
	}

	if err := finishAttemptRecord(ctx, tx, id, attemptNumber, status, reason); err != nil {
		_ = tx.Rollback()
		return err
	}

	failureReason := ""
	if status == JobStatusFailed {
		failureReason = reason
	}

	if _, err := updateJobState(ctx, tx, id, status, failureReason); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func getJobForUpdate(ctx context.Context, tx *sql.Tx, id string) (Job, error) {
	const query = `
SELECT id, status, source_object_key, requested_transforms, output_format, COALESCE(failure_reason, ''), created_at, updated_at
FROM jobs
WHERE id = $1
FOR UPDATE
`

	var job Job
	err := tx.QueryRowContext(ctx, query, id).Scan(
		&job.ID,
		&job.Status,
		&job.SourceObjectKey,
		&job.RequestedTransforms,
		&job.OutputFormat,
		&job.FailureReason,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}

	return job, err
}

func nextAttemptNumber(ctx context.Context, tx *sql.Tx, jobID string) (int, error) {
	var next int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(attempt_number), 0) + 1 FROM job_attempts WHERE job_id = $1`,
		jobID,
	).Scan(&next); err != nil {
		return 0, err
	}

	return next, nil
}

func insertJobAttempt(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	attemptNumber int,
	status JobStatus,
) (JobAttempt, error) {
	const query = `
INSERT INTO job_attempts (job_id, attempt_number, status, started_at)
VALUES ($1, $2, $3, NOW())
RETURNING id, job_id, attempt_number, status, started_at, finished_at, COALESCE(error_message, '')
`

	var attempt JobAttempt
	var finishedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, query, jobID, attemptNumber, status).Scan(
		&attempt.ID,
		&attempt.JobID,
		&attempt.AttemptNumber,
		&attempt.Status,
		&attempt.StartedAt,
		&finishedAt,
		&attempt.ErrorMessage,
	); err != nil {
		return JobAttempt{}, err
	}

	if finishedAt.Valid {
		finishedAtValue := finishedAt.Time
		attempt.FinishedAt = &finishedAtValue
	}

	return attempt, nil
}

func finishAttemptRecord(
	ctx context.Context,
	tx *sql.Tx,
	jobID string,
	attemptNumber int,
	status JobStatus,
	reason string,
) error {
	const query = `
UPDATE job_attempts
SET status = $3, finished_at = NOW(), error_message = NULLIF($4, '')
WHERE job_id = $1 AND attempt_number = $2 AND status = $5
`

	result, err := tx.ExecContext(ctx, query, jobID, attemptNumber, status, reason, JobStatusProcessing)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("job attempt %s/%d not in processing state", jobID, attemptNumber)
	}

	return nil
}

func updateJobState(ctx context.Context, tx *sql.Tx, id string, status JobStatus, failureReason string) (Job, error) {
	const query = `
UPDATE jobs
SET status = $2,
	failure_reason = NULLIF($3, ''),
	updated_at = NOW()
WHERE id = $1
RETURNING id, status, source_object_key, requested_transforms, output_format, COALESCE(failure_reason, ''), created_at, updated_at
`

	var job Job
	err := tx.QueryRowContext(ctx, query, id, status, failureReason).Scan(
		&job.ID,
		&job.Status,
		&job.SourceObjectKey,
		&job.RequestedTransforms,
		&job.OutputFormat,
		&job.FailureReason,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}

	return job, err
}

func (r *JobsRepository) updateJobStateIfCurrentStatus(
	ctx context.Context,
	id string,
	currentStatus JobStatus,
	nextStatus JobStatus,
	reason string,
) error {
	const query = `
UPDATE jobs
SET status = $3,
	failure_reason = NULLIF($4, ''),
	updated_at = NOW()
WHERE id = $1 AND status = $2
`

	result, err := r.db.ExecContext(ctx, query, id, currentStatus, nextStatus, reason)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected > 0 {
		return nil
	}

	if _, err := r.GetJob(ctx, id); err != nil {
		return err
	}

	return ErrInvalidJobStatus
}
