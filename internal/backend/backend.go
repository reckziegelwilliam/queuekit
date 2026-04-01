// Package backend defines the storage interface for job queue backends.
package backend

import (
	"context"
	"time"

	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

// Backend defines the interface for job queue storage backends
type Backend interface {
	// Enqueue adds a new job to the queue
	Enqueue(ctx context.Context, job *queue.Job) error

	// Reserve atomically claims the next available job from the specified queue
	// Returns nil if no jobs are available
	Reserve(ctx context.Context, queueName string) (*queue.Job, error)

	// Ack marks a job as successfully completed
	Ack(ctx context.Context, jobID string) error

	// Nack marks a job as failed, increments its attempt count, and schedules a retry.
	// retryDelay controls how long before the job becomes eligible for re-processing.
	// If the job has exceeded max attempts it is moved to the dead-letter queue instead.
	Nack(ctx context.Context, jobID string, err error, retryDelay time.Duration) error

	// MoveToDLQ moves a job to the dead-letter queue
	MoveToDLQ(ctx context.Context, jobID string) error

	// ListQueues returns all queues with their statistics
	ListQueues(ctx context.Context) ([]queue.Queue, error)

	// ListJobs returns jobs from a queue with optional filtering
	// If queueName is empty, returns jobs from all queues
	// If status is empty, returns jobs with any status
	ListJobs(ctx context.Context, queueName, status string, limit, offset int) ([]*queue.Job, error)

	// GetJob retrieves a single job by its ID
	GetJob(ctx context.Context, jobID string) (*queue.Job, error)

	// DeleteJob permanently deletes a job
	DeleteJob(ctx context.Context, jobID string) error

	// Close cleans up backend resources
	Close() error
}
