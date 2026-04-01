package worker

import (
	"context"
	"fmt"

	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

// Handler is a function that processes a single job.
// It receives a context (which may be canceled on shutdown) and the job to process.
// Return nil to indicate success, or a non-nil error to trigger a retry/DLQ.
type Handler func(ctx context.Context, job *queue.Job) error

// Registry maps job types to their handlers.
type Registry struct {
	handlers map[string]Handler
}

// NewRegistry creates an empty handler registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register associates a Handler with the given job type.
// Registering the same type twice overwrites the previous handler.
func (r *Registry) Register(jobType string, h Handler) {
	r.handlers[jobType] = h
}

// Get returns the Handler registered for the given job type, or false if none.
func (r *Registry) Get(jobType string) (Handler, bool) {
	h, ok := r.handlers[jobType]
	return h, ok
}

// ErrNoHandler is returned when a job type has no registered handler.
type ErrNoHandler struct {
	JobType string
}

func (e *ErrNoHandler) Error() string {
	return fmt.Sprintf("no handler registered for job type %q", e.JobType)
}
