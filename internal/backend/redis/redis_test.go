package redis

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

// getTestRedis creates a test Redis connection
// Set TEST_REDIS_ADDR environment variable to run these tests
// Example: TEST_REDIS_ADDR=localhost:6379
func getTestRedis(t *testing.T) *redis.Client {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set, skipping integration tests")
	}

	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx := context.Background()
	err := client.Ping(ctx).Err()
	require.NoError(t, err)

	return client
}

// cleanupRedis removes all keys from the test Redis
func cleanupRedis(t *testing.T, client *redis.Client) {
	ctx := context.Background()
	err := client.FlushDB(ctx).Err()
	require.NoError(t, err)
}

func TestRedisBackend_Enqueue(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	payload := json.RawMessage(`{"email": "test@example.com"}`)
	job := queue.NewJob("email.send", "emails", payload)

	err := backend.Enqueue(ctx, job)
	require.NoError(t, err)

	// Verify job was stored
	retrieved, err := backend.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, retrieved.ID)
	assert.Equal(t, job.Type, retrieved.Type)
	assert.Equal(t, job.Queue, retrieved.Queue)
	assert.Equal(t, job.Status, retrieved.Status)
}

func TestRedisBackend_EnqueueInvalid(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()

	backend := New(client)
	ctx := context.Background()

	// Missing type
	job := &queue.Job{
		Queue:       "test",
		Payload:     json.RawMessage(`{}`),
		MaxAttempts: 3,
	}

	err := backend.Enqueue(ctx, job)
	assert.Error(t, err)
}

func TestRedisBackend_Reserve(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	// Enqueue jobs
	payload := json.RawMessage(`{"test": "data"}`)
	job1 := queue.NewJob("test.job", "default", payload)
	job1.Priority = queue.PriorityNormal
	job1.ScheduledAt = time.Now().UTC().Add(-1 * time.Hour) // In the past

	job2 := queue.NewJob("test.job", "default", payload)
	job2.Priority = queue.PriorityHigh
	job2.ScheduledAt = time.Now().UTC().Add(-30 * time.Minute) // Also in the past

	require.NoError(t, backend.Enqueue(ctx, job1))
	require.NoError(t, backend.Enqueue(ctx, job2))

	// Reserve should return earliest scheduled job
	reserved, err := backend.Reserve(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, reserved)
	assert.Equal(t, queue.StatusRunning, reserved.Status)

	// Reserve again should return the other job
	reserved2, err := backend.Reserve(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, reserved2)

	// Reserve again should return nil (no jobs available)
	reserved3, err := backend.Reserve(ctx, "default")
	require.NoError(t, err)
	assert.Nil(t, reserved3)
}

func TestRedisBackend_ReserveScheduled(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	// Enqueue job scheduled in the future
	payload := json.RawMessage(`{"test": "data"}`)
	job := queue.NewJob("test.job", "default", payload)
	job.ScheduledAt = time.Now().UTC().Add(1 * time.Hour)

	require.NoError(t, backend.Enqueue(ctx, job))

	// Should not be able to reserve yet
	reserved, err := backend.Reserve(ctx, "default")
	require.NoError(t, err)
	assert.Nil(t, reserved)
}

func TestRedisBackend_Ack(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	// Enqueue and reserve a job
	payload := json.RawMessage(`{"test": "data"}`)
	job := queue.NewJob("test.job", "default", payload)
	require.NoError(t, backend.Enqueue(ctx, job))

	reserved, err := backend.Reserve(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, reserved)

	// Ack the job
	err = backend.Ack(ctx, reserved.ID)
	require.NoError(t, err)

	// Verify job status
	completed, err := backend.GetJob(ctx, reserved.ID)
	require.NoError(t, err)
	assert.Equal(t, queue.StatusCompleted, completed.Status)
	assert.NotNil(t, completed.CompletedAt)
}

func TestRedisBackend_Nack(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	// Enqueue a job with 2 max attempts
	payload := json.RawMessage(`{"test": "data"}`)
	job := queue.NewJob("test.job", "default", payload)
	job.MaxAttempts = 2
	require.NoError(t, backend.Enqueue(ctx, job))

	// Reserve and nack
	reserved, err := backend.Reserve(ctx, "default")
	require.NoError(t, err)

	testErr := &testError{msg: "processing failed"}
	err = backend.Nack(ctx, reserved.ID, testErr)
	require.NoError(t, err)

	// Check job was marked as failed
	failed, err := backend.GetJob(ctx, reserved.ID)
	require.NoError(t, err)
	assert.Equal(t, queue.StatusFailed, failed.Status)
	assert.Equal(t, 1, failed.Attempts)
	assert.Equal(t, "processing failed", failed.LastError)

	// Nack again - should move to dead letter queue
	err = backend.Nack(ctx, failed.ID, testErr)
	require.NoError(t, err)

	dead, err := backend.GetJob(ctx, failed.ID)
	require.NoError(t, err)
	assert.Equal(t, queue.StatusDead, dead.Status)
	assert.Equal(t, 2, dead.Attempts)
}

func TestRedisBackend_MoveToDLQ(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	// Enqueue and reserve a job
	payload := json.RawMessage(`{"test": "data"}`)
	job := queue.NewJob("test.job", "default", payload)
	require.NoError(t, backend.Enqueue(ctx, job))

	reserved, err := backend.Reserve(ctx, "default")
	require.NoError(t, err)

	// Move to DLQ
	err = backend.MoveToDLQ(ctx, reserved.ID)
	require.NoError(t, err)

	// Verify status
	dead, err := backend.GetJob(ctx, reserved.ID)
	require.NoError(t, err)
	assert.Equal(t, queue.StatusDead, dead.Status)
}

func TestRedisBackend_ListQueues(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	// Enqueue jobs in different queues
	payload := json.RawMessage(`{"test": "data"}`)

	job1 := queue.NewJob("test.job", "emails", payload)
	job2 := queue.NewJob("test.job", "emails", payload)
	job3 := queue.NewJob("test.job", "notifications", payload)

	require.NoError(t, backend.Enqueue(ctx, job1))
	require.NoError(t, backend.Enqueue(ctx, job2))
	require.NoError(t, backend.Enqueue(ctx, job3))

	// Reserve one from emails
	reserved, err := backend.Reserve(ctx, "emails")
	require.NoError(t, err)

	// Complete it
	err = backend.Ack(ctx, reserved.ID)
	require.NoError(t, err)

	// List queues
	queues, err := backend.ListQueues(ctx)
	require.NoError(t, err)
	assert.Len(t, queues, 2)

	// Check emails queue exists
	var found bool
	for _, q := range queues {
		if q.Name == "emails" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestRedisBackend_ListJobs(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	// Enqueue multiple jobs
	payload := json.RawMessage(`{"test": "data"}`)
	for i := 0; i < 5; i++ {
		job := queue.NewJob("test.job", "default", payload)
		require.NoError(t, backend.Enqueue(ctx, job))
	}

	// List all jobs
	jobs, err := backend.ListJobs(ctx, "", "", 0, 0)
	require.NoError(t, err)
	assert.Len(t, jobs, 5)

	// List with limit
	jobs, err = backend.ListJobs(ctx, "", "", 2, 0)
	require.NoError(t, err)
	assert.Len(t, jobs, 2)

	// List with offset
	jobs, err = backend.ListJobs(ctx, "", "", 2, 2)
	require.NoError(t, err)
	assert.Len(t, jobs, 2)

	// List by queue
	jobs, err = backend.ListJobs(ctx, "default", "", 0, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(jobs), 1)

	// List by status
	jobs, err = backend.ListJobs(ctx, "", queue.StatusPending, 0, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(jobs), 1)
}

func TestRedisBackend_DeleteJob(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	// Enqueue a job
	payload := json.RawMessage(`{"test": "data"}`)
	job := queue.NewJob("test.job", "default", payload)
	require.NoError(t, backend.Enqueue(ctx, job))

	// Delete it
	err := backend.DeleteJob(ctx, job.ID)
	require.NoError(t, err)

	// Verify it's gone
	_, err = backend.GetJob(ctx, job.ID)
	assert.Error(t, err)
}

func TestRedisBackend_ConcurrentReserve(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	// Enqueue 10 jobs
	payload := json.RawMessage(`{"test": "data"}`)
	for i := 0; i < 10; i++ {
		job := queue.NewJob("test.job", "default", payload)
		require.NoError(t, backend.Enqueue(ctx, job))
	}

	// Reserve concurrently from multiple goroutines
	results := make(chan string, 10)
	for i := 0; i < 10; i++ {
		go func() {
			reserved, err := backend.Reserve(ctx, "default")
			if err == nil && reserved != nil {
				results <- reserved.ID
			} else {
				results <- ""
			}
		}()
	}

	// Collect results
	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		jobID := <-results
		if jobID != "" {
			if seen[jobID] {
				t.Errorf("Job %s was reserved twice!", jobID)
			}
			seen[jobID] = true
		}
	}

	// Should have reserved all 10 unique jobs
	assert.Equal(t, 10, len(seen))
}

func TestRedisBackend_LuaScripts(t *testing.T) {
	client := getTestRedis(t)
	defer client.Close()
	defer cleanupRedis(t, client)

	backend := New(client)
	ctx := context.Background()

	// Test that reserve script works atomically
	payload := json.RawMessage(`{"test": "data"}`)
	job := queue.NewJob("test.job", "atomic_test", payload)
	require.NoError(t, backend.Enqueue(ctx, job))

	// Reserve the job
	reserved, err := backend.Reserve(ctx, "atomic_test")
	require.NoError(t, err)
	require.NotNil(t, reserved)

	// Attempting to reserve again should return nil (job already running)
	reserved2, err := backend.Reserve(ctx, "atomic_test")
	require.NoError(t, err)
	assert.Nil(t, reserved2)
}

// Helper test error type
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

