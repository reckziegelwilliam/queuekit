package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reckziegelwilliam/queuekit/internal/backend"
	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

var _ backend.Backend = (*PostgresBackend)(nil)

// PostgresBackend implements the Backend interface using PostgreSQL
type PostgresBackend struct {
	pool *pgxpool.Pool
}

// New creates a new PostgresBackend with the given connection pool
func New(pool *pgxpool.Pool) *PostgresBackend {
	return &PostgresBackend{
		pool: pool,
	}
}

// NewFromDSN creates a new PostgresBackend from a connection string
func NewFromDSN(ctx context.Context, dsn string) (*PostgresBackend, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &PostgresBackend{pool: pool}, nil
}

// Enqueue adds a new job to the queue
func (p *PostgresBackend) Enqueue(ctx context.Context, job *queue.Job) error {
	if err := job.Validate(); err != nil {
		return fmt.Errorf("invalid job: %w", err)
	}

	query := `
		INSERT INTO jobs (
			id, type, queue, payload, status, priority, attempts, max_attempts,
			scheduled_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
	`

	_, err := p.pool.Exec(ctx, query,
		job.ID, job.Type, job.Queue, job.Payload, job.Status, job.Priority,
		job.Attempts, job.MaxAttempts, job.ScheduledAt, job.CreatedAt, job.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	return nil
}

// Reserve atomically claims the next available job from the specified queue
func (p *PostgresBackend) Reserve(ctx context.Context, queueName string) (*queue.Job, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Find and lock the next available job
	query := `
		SELECT id, type, queue, payload, status, priority, attempts, max_attempts,
		       scheduled_at, created_at, updated_at, completed_at, failed_at, last_error
		FROM jobs
		WHERE queue = $1 
		  AND status = 'pending'
		  AND scheduled_at <= $2
		ORDER BY priority DESC, scheduled_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`

	row := tx.QueryRow(ctx, query, queueName, time.Now().UTC())

	job := &queue.Job{}
	err = row.Scan(
		&job.ID, &job.Type, &job.Queue, &job.Payload, &job.Status, &job.Priority,
		&job.Attempts, &job.MaxAttempts, &job.ScheduledAt, &job.CreatedAt,
		&job.UpdatedAt, &job.CompletedAt, &job.FailedAt, &job.LastError,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // No jobs available
		}
		return nil, fmt.Errorf("failed to query job: %w", err)
	}

	// Update job status to running
	updateQuery := `
		UPDATE jobs
		SET status = 'running', updated_at = $1
		WHERE id = $2
	`

	now := time.Now().UTC()
	_, err = tx.Exec(ctx, updateQuery, now, job.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update job status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	job.Status = queue.StatusRunning
	job.UpdatedAt = now

	return job, nil
}

// Ack marks a job as successfully completed
func (p *PostgresBackend) Ack(ctx context.Context, jobID string) error {
	query := `
		UPDATE jobs
		SET status = 'completed', completed_at = $1, updated_at = $1
		WHERE id = $2
	`

	now := time.Now().UTC()
	result, err := p.pool.Exec(ctx, query, now, jobID)
	if err != nil {
		return fmt.Errorf("failed to ack job: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found: %s", jobID)
	}

	return nil
}

// Nack marks a job as failed, increments its attempt count, and schedules a retry.
// If attempts >= max_attempts the job is moved to the dead-letter queue instead.
// retryDelay controls when the job becomes eligible for re-processing.
func (p *PostgresBackend) Nack(ctx context.Context, jobID string, jobErr error, retryDelay time.Duration) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get current job state
	var attempts, maxAttempts int
	queryJob := `SELECT attempts, max_attempts FROM jobs WHERE id = $1 FOR UPDATE`
	err = tx.QueryRow(ctx, queryJob, jobID).Scan(&attempts, &maxAttempts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("job not found: %s", jobID)
		}
		return fmt.Errorf("failed to query job: %w", err)
	}

	// Increment attempts
	attempts++
	now := time.Now().UTC()
	var lastError string
	if jobErr != nil {
		lastError = jobErr.Error()
	}

	// If exceeded max attempts, move to dead letter queue
	if attempts >= maxAttempts {
		query := `
			UPDATE jobs
			SET status = 'dead', attempts = $1, last_error = $2,
			    failed_at = $3, updated_at = $3
			WHERE id = $4
		`
		_, err = tx.Exec(ctx, query, attempts, lastError, now, jobID)
	} else {
		// Schedule retry: put back to pending with a future scheduled_at
		retryAt := now.Add(retryDelay)
		query := `
			UPDATE jobs
			SET status = 'pending', attempts = $1, last_error = $2,
			    failed_at = $3, updated_at = $3, scheduled_at = $4
			WHERE id = $5
		`
		_, err = tx.Exec(ctx, query, attempts, lastError, now, retryAt, jobID)
	}

	if err != nil {
		return fmt.Errorf("failed to nack job: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// MoveToDLQ moves a job to the dead-letter queue
func (p *PostgresBackend) MoveToDLQ(ctx context.Context, jobID string) error {
	query := `
		UPDATE jobs
		SET status = 'dead', updated_at = $1
		WHERE id = $2
	`

	now := time.Now().UTC()
	result, err := p.pool.Exec(ctx, query, now, jobID)
	if err != nil {
		return fmt.Errorf("failed to move job to DLQ: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found: %s", jobID)
	}

	return nil
}

// ListQueues returns all queues with their statistics
func (p *PostgresBackend) ListQueues(ctx context.Context) ([]queue.Queue, error) {
	query := `
		SELECT 
			queue,
			COUNT(*) FILTER (WHERE status = 'pending') as pending_count,
			COUNT(*) FILTER (WHERE status = 'running') as running_count,
			COUNT(*) FILTER (WHERE status = 'completed') as completed_count,
			COUNT(*) FILTER (WHERE status = 'failed') as failed_count,
			COUNT(*) FILTER (WHERE status = 'dead') as dead_count
		FROM jobs
		GROUP BY queue
		ORDER BY queue
	`

	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list queues: %w", err)
	}
	defer rows.Close()

	var queues []queue.Queue
	for rows.Next() {
		var q queue.Queue
		err := rows.Scan(&q.Name, &q.Size, &q.ProcessingCount, &q.CompletedCount, &q.FailedCount, &q.DeadCount)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queue: %w", err)
		}
		queues = append(queues, q)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating queues: %w", err)
	}

	return queues, nil
}

// ListJobs returns jobs from a queue with optional filtering
func (p *PostgresBackend) ListJobs(ctx context.Context, queueName, status string, limit, offset int) ([]*queue.Job, error) {
	query := `
		SELECT id, type, queue, payload, status, priority, attempts, max_attempts,
		       scheduled_at, created_at, updated_at, completed_at, failed_at, last_error
		FROM jobs
		WHERE 1=1
	`

	args := []interface{}{}
	argPos := 1

	if queueName != "" {
		query += fmt.Sprintf(" AND queue = $%d", argPos)
		args = append(args, queueName)
		argPos++
	}

	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", argPos)
		args = append(args, status)
		argPos++
	}

	query += " ORDER BY created_at DESC"

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, limit)
		argPos++
	}

	if offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argPos)
		args = append(args, offset)
	}

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*queue.Job
	for rows.Next() {
		job := &queue.Job{}
		err := rows.Scan(
			&job.ID, &job.Type, &job.Queue, &job.Payload, &job.Status, &job.Priority,
			&job.Attempts, &job.MaxAttempts, &job.ScheduledAt, &job.CreatedAt,
			&job.UpdatedAt, &job.CompletedAt, &job.FailedAt, &job.LastError,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}
		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating jobs: %w", err)
	}

	return jobs, nil
}

// GetJob retrieves a single job by its ID
func (p *PostgresBackend) GetJob(ctx context.Context, jobID string) (*queue.Job, error) {
	query := `
		SELECT id, type, queue, payload, status, priority, attempts, max_attempts,
		       scheduled_at, created_at, updated_at, completed_at, failed_at, last_error
		FROM jobs
		WHERE id = $1
	`

	job := &queue.Job{}
	err := p.pool.QueryRow(ctx, query, jobID).Scan(
		&job.ID, &job.Type, &job.Queue, &job.Payload, &job.Status, &job.Priority,
		&job.Attempts, &job.MaxAttempts, &job.ScheduledAt, &job.CreatedAt,
		&job.UpdatedAt, &job.CompletedAt, &job.FailedAt, &job.LastError,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("job not found: %s", jobID)
		}
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	return job, nil
}

// DeleteJob permanently deletes a job
func (p *PostgresBackend) DeleteJob(ctx context.Context, jobID string) error {
	query := `DELETE FROM jobs WHERE id = $1`

	result, err := p.pool.Exec(ctx, query, jobID)
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found: %s", jobID)
	}

	return nil
}

// Close cleans up backend resources
func (p *PostgresBackend) Close() error {
	p.pool.Close()
	return nil
}

// Helper function to scan job payload
func scanJob(rows pgx.Row) (*queue.Job, error) {
	job := &queue.Job{}
	var payloadBytes []byte

	err := rows.Scan(
		&job.ID, &job.Type, &job.Queue, &payloadBytes, &job.Status, &job.Priority,
		&job.Attempts, &job.MaxAttempts, &job.ScheduledAt, &job.CreatedAt,
		&job.UpdatedAt, &job.CompletedAt, &job.FailedAt, &job.LastError,
	)

	if err != nil {
		return nil, err
	}

	job.Payload = json.RawMessage(payloadBytes)
	return job, nil
}
