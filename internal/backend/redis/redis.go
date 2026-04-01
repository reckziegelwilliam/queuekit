// Package redis implements the Backend interface using Redis.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/reckziegelwilliam/queuekit/internal/backend"
	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

var _ backend.Backend = (*RedisBackend)(nil)

// RedisBackend implements the Backend interface using Redis
type RedisBackend struct {
	client *redis.Client
}

// New creates a new RedisBackend with the given client
func New(client *redis.Client) *RedisBackend {
	return &RedisBackend{
		client: client,
	}
}

// NewFromOptions creates a new RedisBackend from Redis options
func NewFromOptions(opts *redis.Options) (*RedisBackend, error) {
	client := redis.NewClient(opts)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisBackend{client: client}, nil
}

// jobKey returns the Redis key for a job
func jobKey(jobID string) string {
	return fmt.Sprintf("job:%s", jobID)
}

// queueKey returns the Redis key for a queue sorted set
func queueKey(queueName string) string {
	return fmt.Sprintf("queue:%s", queueName)
}

// statusKey returns the Redis key for status tracking
func statusKey(queueName, status string) string {
	return fmt.Sprintf("status:queue:%s:%s", queueName, status)
}

// Enqueue adds a new job to the queue
func (r *RedisBackend) Enqueue(ctx context.Context, job *queue.Job) error {
	if err := job.Validate(); err != nil {
		return fmt.Errorf("invalid job: %w", err)
	}

	pipe := r.client.Pipeline()

	// Store job as hash
	jobData := map[string]interface{}{
		"id":           job.ID,
		"type":         job.Type,
		"queue":        job.Queue,
		"payload":      string(job.Payload),
		"status":       job.Status,
		"priority":     job.Priority,
		"attempts":     job.Attempts,
		"max_attempts": job.MaxAttempts,
		"scheduled_at": job.ScheduledAt.Unix(),
		"created_at":   job.CreatedAt.Unix(),
		"updated_at":   job.UpdatedAt.Unix(),
		"last_error":   job.LastError,
	}

	pipe.HSet(ctx, jobKey(job.ID), jobData)

	// Add to queue sorted set (score = scheduled_at for time-based ordering)
	score := float64(job.ScheduledAt.Unix())
	pipe.ZAdd(ctx, queueKey(job.Queue), redis.Z{
		Score:  score,
		Member: job.ID,
	})

	// Add to status set
	pipe.SAdd(ctx, statusKey(job.Queue, job.Status), job.ID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	return nil
}

// Reserve atomically claims the next available job from the specified queue
func (r *RedisBackend) Reserve(ctx context.Context, queueName string) (*queue.Job, error) {
	now := time.Now().UTC()

	result, err := reserveScript.Run(ctx, r.client,
		[]string{queueKey(queueName), "job:"},
		now.Unix(),
		queue.StatusRunning,
		now.Unix(),
	).Result()

	if err != nil {
		if err == redis.Nil {
			return nil, nil // No jobs available
		}
		return nil, fmt.Errorf("failed to reserve job: %w", err)
	}

	if result == nil {
		return nil, nil // No jobs available
	}

	// Parse result as hash fields
	fields, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type from reserve script")
	}

	job, err := parseJobFromHash(fields)
	if err != nil {
		return nil, fmt.Errorf("failed to parse job: %w", err)
	}

	return job, nil
}

// Ack marks a job as successfully completed
func (r *RedisBackend) Ack(ctx context.Context, jobID string) error {
	pipe := r.client.Pipeline()

	now := time.Now().UTC()

	// Get job queue before updating
	queueCmd := pipe.HGet(ctx, jobKey(jobID), "queue")

	// Update job status
	pipe.HSet(ctx, jobKey(jobID),
		"status", queue.StatusCompleted,
		"completed_at", now.Unix(),
		"updated_at", now.Unix(),
	)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to ack job: %w", err)
	}

	queueName, err := queueCmd.Result()
	if err != nil {
		return fmt.Errorf("failed to get job queue: %w", err)
	}

	// Move from running to completed status set
	pipe = r.client.Pipeline()
	pipe.SRem(ctx, statusKey(queueName, queue.StatusRunning), jobID)
	pipe.SAdd(ctx, statusKey(queueName, queue.StatusCompleted), jobID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update status sets: %w", err)
	}

	return nil
}

// Nack marks a job as failed, increments its attempt count, and schedules a retry.
// If attempts >= max_attempts the job is moved to the dead-letter queue instead.
// retryDelay controls when the job becomes eligible for re-processing.
func (r *RedisBackend) Nack(ctx context.Context, jobID string, jobErr error, retryDelay time.Duration) error {
	// Get current job data
	jobData, err := r.client.HGetAll(ctx, jobKey(jobID)).Result()
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	if len(jobData) == 0 {
		return fmt.Errorf("job not found: %s", jobID)
	}

	attempts, err := strconv.Atoi(jobData["attempts"])
	if err != nil {
		return fmt.Errorf("invalid attempts value: %w", err)
	}
	maxAttempts, err := strconv.Atoi(jobData["max_attempts"])
	if err != nil {
		return fmt.Errorf("invalid max_attempts value: %w", err)
	}
	queueName := jobData["queue"]

	lastError := ""
	if jobErr != nil {
		lastError = jobErr.Error()
	}

	now := time.Now().UTC()
	retryAt := now.Add(retryDelay)

	_, err = nackScript.Run(ctx, r.client,
		[]string{jobKey(jobID), queueName},
		attempts,
		maxAttempts,
		lastError,
		now.Unix(),
		retryAt.Unix(),
	).Result()

	if err != nil {
		return fmt.Errorf("failed to nack job: %w", err)
	}

	return nil
}

// MoveToDLQ moves a job to the dead-letter queue
func (r *RedisBackend) MoveToDLQ(ctx context.Context, jobID string) error {
	// Get job queue
	queueName, err := r.client.HGet(ctx, jobKey(jobID), "queue").Result()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("job not found: %s", jobID)
		}
		return fmt.Errorf("failed to get job queue: %w", err)
	}

	now := time.Now().UTC()
	pipe := r.client.Pipeline()

	// Update job status
	pipe.HSet(ctx, jobKey(jobID),
		"status", queue.StatusDead,
		"updated_at", now.Unix(),
	)

	// Remove from queue sorted set
	pipe.ZRem(ctx, queueKey(queueName), jobID)

	// Update status sets
	pipe.SRem(ctx, statusKey(queueName, queue.StatusRunning), jobID)
	pipe.SRem(ctx, statusKey(queueName, queue.StatusFailed), jobID)
	pipe.SAdd(ctx, statusKey(queueName, queue.StatusDead), jobID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to move job to DLQ: %w", err)
	}

	return nil
}

// ListQueues returns all queues with their statistics
func (r *RedisBackend) ListQueues(ctx context.Context) ([]queue.Queue, error) {
	// Find all queue keys
	keys, err := r.client.Keys(ctx, "queue:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list queue keys: %w", err)
	}

	queues := make([]queue.Queue, 0, len(keys))

	for _, key := range keys {
		// Extract queue name from key
		queueName := key[6:] // Remove "queue:" prefix

		// Get queue statistics
		result, err := queueStatsScript.Run(ctx, r.client, []string{queueName}).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get queue stats: %w", err)
		}

		fields, ok := result.([]interface{})
		if !ok {
			continue
		}

		q := parseQueueStats(fields)
		queues = append(queues, q)
	}

	return queues, nil
}

// ListJobs returns jobs from a queue with optional filtering
func (r *RedisBackend) ListJobs(ctx context.Context, queueName, status string, limit, offset int) ([]*queue.Job, error) {
	var jobIDs []string

	switch {
	case status != "":
		// Get job IDs from status set
		members, err := r.client.SMembers(ctx, statusKey(queueName, status)).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get job IDs from status set: %w", err)
		}
		jobIDs = members
	case queueName != "":
		// Get job IDs from queue sorted set
		members, err := r.client.ZRange(ctx, queueKey(queueName), 0, -1).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get job IDs from queue: %w", err)
		}
		jobIDs = members
	default:
		// Get all job keys
		keys, err := r.client.Keys(ctx, "job:*").Result()
		if err != nil {
			return nil, fmt.Errorf("failed to list job keys: %w", err)
		}
		// Extract job IDs from keys
		jobIDs = make([]string, 0, len(keys))
		for _, key := range keys {
			jobIDs = append(jobIDs, key[4:]) // Remove "job:" prefix
		}
	}

	// Apply pagination
	if offset >= len(jobIDs) {
		return []*queue.Job{}, nil
	}

	end := offset + limit
	if limit <= 0 || end > len(jobIDs) {
		end = len(jobIDs)
	}

	jobIDs = jobIDs[offset:end]

	// Fetch jobs
	jobs := make([]*queue.Job, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		job, err := r.GetJob(ctx, jobID)
		if err != nil {
			continue // Skip jobs that no longer exist
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// GetJob retrieves a single job by its ID
func (r *RedisBackend) GetJob(ctx context.Context, jobID string) (*queue.Job, error) {
	jobData, err := r.client.HGetAll(ctx, jobKey(jobID)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	if len(jobData) == 0 {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	job, err := parseJobFromMap(jobData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse job: %w", err)
	}

	return job, nil
}

// DeleteJob permanently deletes a job
func (r *RedisBackend) DeleteJob(ctx context.Context, jobID string) error {
	// Get job queue and status before deleting
	jobData, err := r.client.HGetAll(ctx, jobKey(jobID)).Result()
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	if len(jobData) == 0 {
		return fmt.Errorf("job not found: %s", jobID)
	}

	queueName := jobData["queue"]
	status := jobData["status"]

	pipe := r.client.Pipeline()

	// Delete job hash
	pipe.Del(ctx, jobKey(jobID))

	// Remove from queue sorted set
	pipe.ZRem(ctx, queueKey(queueName), jobID)

	// Remove from status set
	pipe.SRem(ctx, statusKey(queueName, status), jobID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	return nil
}

// Close cleans up backend resources
func (r *RedisBackend) Close() error {
	return r.client.Close()
}

// Helper functions

func parseJobFromHash(fields []interface{}) (*queue.Job, error) {
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("invalid hash fields count")
	}

	jobData := make(map[string]string)
	for i := 0; i < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			continue
		}
		value, ok := fields[i+1].(string)
		if !ok {
			continue
		}
		jobData[key] = value
	}

	return parseJobFromMap(jobData)
}

func parseJobFromMap(data map[string]string) (*queue.Job, error) {
	job := &queue.Job{
		ID:        data["id"],
		Type:      data["type"],
		Queue:     data["queue"],
		Status:    data["status"],
		LastError: data["last_error"],
	}

	// Parse payload
	if payloadStr, ok := data["payload"]; ok {
		job.Payload = json.RawMessage(payloadStr)
	}

	// Parse integers
	if v, err := strconv.Atoi(data["priority"]); err == nil {
		job.Priority = v
	}
	if v, err := strconv.Atoi(data["attempts"]); err == nil {
		job.Attempts = v
	}
	if v, err := strconv.Atoi(data["max_attempts"]); err == nil {
		job.MaxAttempts = v
	}

	// Parse timestamps
	if v, err := strconv.ParseInt(data["scheduled_at"], 10, 64); err == nil {
		job.ScheduledAt = time.Unix(v, 0).UTC()
	}
	if v, err := strconv.ParseInt(data["created_at"], 10, 64); err == nil {
		job.CreatedAt = time.Unix(v, 0).UTC()
	}
	if v, err := strconv.ParseInt(data["updated_at"], 10, 64); err == nil {
		job.UpdatedAt = time.Unix(v, 0).UTC()
	}

	// Parse optional timestamps
	if v, err := strconv.ParseInt(data["completed_at"], 10, 64); err == nil && v > 0 {
		t := time.Unix(v, 0).UTC()
		job.CompletedAt = &t
	}
	if v, err := strconv.ParseInt(data["failed_at"], 10, 64); err == nil && v > 0 {
		t := time.Unix(v, 0).UTC()
		job.FailedAt = &t
	}

	return job, nil
}

func parseQueueStats(fields []interface{}) queue.Queue {
	q := queue.Queue{}

	for i := 0; i < len(fields)-1; i += 2 {
		key, _ := fields[i].(string)
		value := fields[i+1]

		switch key {
		case "name":
			q.Name, _ = value.(string)
		case "size":
			if v, ok := value.(int64); ok {
				q.Size = v
			}
		case "processing_count":
			if v, ok := value.(int64); ok {
				q.ProcessingCount = v
			}
		case "completed_count":
			if v, ok := value.(int64); ok {
				q.CompletedCount = v
			}
		case "failed_count":
			if v, ok := value.(int64); ok {
				q.FailedCount = v
			}
		case "dead_count":
			if v, ok := value.(int64); ok {
				q.DeadCount = v
			}
		}
	}

	return q
}
