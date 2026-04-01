// Package queue defines the core domain models for jobs and queues.
package queue

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Job status constants
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusDead      = "dead"
)

// Priority constants
const (
	PriorityLow      = 0
	PriorityNormal   = 10
	PriorityHigh     = 20
	PriorityCritical = 30
)

// Job represents a background job to be executed
type Job struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Queue       string          `json:"queue"`
	Payload     json.RawMessage `json:"payload"`
	Status      string          `json:"status"`
	Priority    int             `json:"priority"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	ScheduledAt time.Time       `json:"scheduled_at"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	FailedAt    *time.Time      `json:"failed_at,omitempty"`
	LastError   string          `json:"last_error,omitempty"`
}

// NewJob creates a new job with default values
func NewJob(jobType, queue string, payload json.RawMessage) *Job {
	now := time.Now().UTC()
	return &Job{
		ID:          uuid.New().String(),
		Type:        jobType,
		Queue:       queue,
		Payload:     payload,
		Status:      StatusPending,
		Priority:    PriorityNormal,
		Attempts:    0,
		MaxAttempts: 3,
		ScheduledAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// Validate checks if the job has all required fields
func (j *Job) Validate() error {
	if j.Type == "" {
		return errors.New("job type is required")
	}
	if j.Queue == "" {
		return errors.New("job queue is required")
	}
	if len(j.Payload) == 0 {
		return errors.New("job payload is required")
	}
	if j.MaxAttempts < 1 {
		return errors.New("job max_attempts must be at least 1")
	}
	return nil
}

// IsRetryable checks if the job can be retried
func (j *Job) IsRetryable() bool {
	return j.Attempts < j.MaxAttempts
}

// MarkRunning updates the job status to running
func (j *Job) MarkRunning() {
	j.Status = StatusRunning
	j.UpdatedAt = time.Now().UTC()
}

// MarkCompleted updates the job status to completed
func (j *Job) MarkCompleted() {
	j.Status = StatusCompleted
	now := time.Now().UTC()
	j.UpdatedAt = now
	j.CompletedAt = &now
}

// MarkFailed updates the job status to failed and increments attempts
func (j *Job) MarkFailed(err error) {
	j.Status = StatusFailed
	j.Attempts++
	now := time.Now().UTC()
	j.UpdatedAt = now
	j.FailedAt = &now
	if err != nil {
		j.LastError = err.Error()
	}
}

// MarkDead moves the job to the dead-letter queue
func (j *Job) MarkDead() {
	j.Status = StatusDead
	j.UpdatedAt = time.Now().UTC()
}
