package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/reckziegelwilliam/queuekit/internal/backend"
	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

// Status represents the current operational state of a Worker.
type Status string

const (
	// StatusIdle means the worker is polling for jobs but none are available.
	StatusIdle Status = "idle"
	// StatusRunning means the worker is currently executing a job.
	StatusRunning Status = "running"
	// StatusStopped means the worker has exited its run loop.
	StatusStopped Status = "stopped"
)

// WorkerState is a point-in-time snapshot of a worker's status.
type WorkerState struct {
	ID        string
	Queue     string
	Status    Status
	LastJobID string
	UpdatedAt time.Time
}

// Worker is a single job consumer. It polls one queue, executes handlers, and
// calls Ack/Nack on the backend based on the outcome.
type Worker struct {
	id           string
	queueName    string
	backend      backend.Backend
	registry     *Registry
	backoff      BackoffStrategy
	logger       *slog.Logger
	pollInterval time.Duration

	mu        sync.Mutex
	status    Status
	lastJobID string
	updatedAt time.Time
}

// Option is a functional option for configuring a Worker.
type Option func(*Worker)

// WithBackoff sets a custom BackoffStrategy (default: exponential 5 s → 1 h).
func WithBackoff(b BackoffStrategy) Option {
	return func(w *Worker) { w.backoff = b }
}

// WithPollInterval sets how often the worker checks for new jobs (default: 1 s).
func WithPollInterval(d time.Duration) Option {
	return func(w *Worker) { w.pollInterval = d }
}

// WithWorkerLogger sets a custom structured logger.
func WithWorkerLogger(l *slog.Logger) Option {
	return func(w *Worker) { w.logger = l }
}

// NewWorker creates a Worker that processes jobs from queueName.
func NewWorker(id, queueName string, b backend.Backend, r *Registry, opts ...Option) *Worker {
	w := &Worker{
		id:           id,
		queueName:    queueName,
		backend:      b,
		registry:     r,
		backoff:      DefaultExponentialBackoff(),
		logger:       slog.Default(),
		pollInterval: 1 * time.Second,
		status:       StatusIdle,
		updatedAt:    time.Now(),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// State returns a thread-safe snapshot of the worker's current state.
func (w *Worker) State() WorkerState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return WorkerState{
		ID:        w.id,
		Queue:     w.queueName,
		Status:    w.status,
		LastJobID: w.lastJobID,
		UpdatedAt: w.updatedAt,
	}
}

// Run starts the worker's polling loop. It blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	w.setState(StatusIdle, "")
	w.logger.Info("worker started", "worker_id", w.id, "queue", w.queueName)

	defer func() {
		w.setState(StatusStopped, "")
		w.logger.Info("worker stopped", "worker_id", w.id, "queue", w.queueName)
	}()

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.processNext(ctx); err != nil {
				if !errors.Is(err, context.Canceled) {
					w.logger.Error("worker error", "worker_id", w.id, "error", err)
				}
			}
		}
	}
}

// processNext attempts to claim and execute the next available job.
func (w *Worker) processNext(ctx context.Context) error {
	job, err := w.backend.Reserve(ctx, w.queueName)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("reserve: %w", err)
	}
	if job == nil {
		return nil // queue is empty
	}
	return w.execute(ctx, job)
}

// execute runs the handler for a job and issues Ack or Nack based on the result.
func (w *Worker) execute(ctx context.Context, job *queue.Job) error {
	w.setState(StatusRunning, job.ID)
	defer w.setState(StatusIdle, "")

	w.logger.Info("executing job",
		"worker_id", w.id,
		"job_id", job.ID,
		"job_type", job.Type,
		"queue", job.Queue,
		"attempt", job.Attempts+1,
		"max_attempts", job.MaxAttempts,
	)

	handler, ok := w.registry.Get(job.Type)
	if !ok {
		// No handler — nack immediately so attempts are counted and DLQ applies.
		noHandlerErr := &ErrNoHandler{JobType: job.Type}
		w.logger.Error("no handler for job type",
			"worker_id", w.id, "job_id", job.ID, "job_type", job.Type)
		if err := w.backend.Nack(ctx, job.ID, noHandlerErr, 0); err != nil {
			return fmt.Errorf("nack (no handler): %w", err)
		}
		return nil
	}

	execErr := handler(ctx, job)
	if execErr == nil {
		w.logger.Info("job completed",
			"worker_id", w.id, "job_id", job.ID, "job_type", job.Type)
		if err := w.backend.Ack(ctx, job.ID); err != nil {
			return fmt.Errorf("ack: %w", err)
		}
		return nil
	}

	// Handler returned an error — calculate backoff and nack.
	delay := w.backoff.NextDelay(job.Attempts + 1)
	w.logger.Warn("job failed",
		"worker_id", w.id,
		"job_id", job.ID,
		"job_type", job.Type,
		"attempt", job.Attempts+1,
		"retry_delay", delay,
		"error", execErr,
	)

	if err := w.backend.Nack(ctx, job.ID, execErr, delay); err != nil {
		return fmt.Errorf("nack: %w", err)
	}
	return nil
}

// setState updates the worker's internal status (thread-safe).
func (w *Worker) setState(s Status, jobID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = s
	w.lastJobID = jobID
	w.updatedAt = time.Now()
}
