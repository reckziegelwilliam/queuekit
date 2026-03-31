package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

// getTestDB creates a test database connection
// Set TEST_DATABASE_URL environment variable to run these tests
// Example: TEST_DATABASE_URL=postgres://user:pass@localhost/queuekit_test
func getTestDB(t *testing.T) *pgxpool.Pool {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)

	// Run migrations
	err = RunMigrations(ctx, pool)
	require.NoError(t, err)

	return pool
}

// cleanupDB removes all jobs from the test database
func cleanupDB(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	_, err := pool.Exec(ctx, "TRUNCATE TABLE jobs")
	require.NoError(t, err)
}

func TestPostgresBackend_Enqueue(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()
	defer cleanupDB(t, pool)

	backend := New(pool)
	ctx := context.Background()

	payload := json.RawMessage(`{"email": "test@example.com"}`)
	job := queue.NewJob("email.send", "emails", payload)

	err := backend.Enqueue(ctx, job)
	require.NoError(t, err)

	// Verify job was inserted
	retrieved, err := backend.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, retrieved.ID)
	assert.Equal(t, job.Type, retrieved.Type)
	assert.Equal(t, job.Queue, retrieved.Queue)
	assert.Equal(t, job.Status, retrieved.Status)
}

func TestPostgresBackend_EnqueueInvalid(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	backend := New(pool)
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

func TestPostgresBackend_Reserve(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()
	defer cleanupDB(t, pool)

	backend := New(pool)
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

	// Reserve should return job2 first (higher priority)
	reserved, err := backend.Reserve(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, reserved)
	assert.Equal(t, job2.ID, reserved.ID)
	assert.Equal(t, queue.StatusRunning, reserved.Status)

	// Reserve again should return job1
	reserved, err = backend.Reserve(ctx, "default")
	require.NoError(t, err)
	require.NotNil(t, reserved)
	assert.Equal(t, job1.ID, reserved.ID)

	// Reserve again should return nil (no jobs available)
	reserved, err = backend.Reserve(ctx, "default")
	require.NoError(t, err)
	assert.Nil(t, reserved)
}

func TestPostgresBackend_ReserveScheduled(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()
	defer cleanupDB(t, pool)

	backend := New(pool)
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

func TestPostgresBackend_Ack(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()
	defer cleanupDB(t, pool)

	backend := New(pool)
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

func TestPostgresBackend_Nack(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()
	defer cleanupDB(t, pool)

	backend := New(pool)
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
	err = backend.Nack(ctx, reserved.ID, testErr, 0)
	require.NoError(t, err)

	// Job should be rescheduled as pending (retryable, attempts=1, max=2)
	retrying, err := backend.GetJob(ctx, reserved.ID)
	require.NoError(t, err)
	assert.Equal(t, queue.StatusPending, retrying.Status)
	assert.Equal(t, 1, retrying.Attempts)
	assert.Equal(t, "processing failed", retrying.LastError)

	// Nack again - should move to dead letter queue (attempts=2 >= max=2)
	err = backend.Nack(ctx, retrying.ID, testErr, 0)
	require.NoError(t, err)

	dead, err := backend.GetJob(ctx, failed.ID)
	require.NoError(t, err)
	assert.Equal(t, queue.StatusDead, dead.Status)
	assert.Equal(t, 2, dead.Attempts)
}

func TestPostgresBackend_MoveToDLQ(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()
	defer cleanupDB(t, pool)

	backend := New(pool)
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

func TestPostgresBackend_ListQueues(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()
	defer cleanupDB(t, pool)

	backend := New(pool)
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

	// Check emails queue
	var emailsQueue *queue.Queue
	for i := range queues {
		if queues[i].Name == "emails" {
			emailsQueue = &queues[i]
			break
		}
	}
	require.NotNil(t, emailsQueue)
	assert.Equal(t, int64(1), emailsQueue.Size)           // 1 pending
	assert.Equal(t, int64(1), emailsQueue.CompletedCount) // 1 completed
}

func TestPostgresBackend_ListJobs(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()
	defer cleanupDB(t, pool)

	backend := New(pool)
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
	assert.Len(t, jobs, 5)

	// List by status
	jobs, err = backend.ListJobs(ctx, "", queue.StatusPending, 0, 0)
	require.NoError(t, err)
	assert.Len(t, jobs, 5)
}

func TestPostgresBackend_DeleteJob(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()
	defer cleanupDB(t, pool)

	backend := New(pool)
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

func TestPostgresBackend_ConcurrentReserve(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()
	defer cleanupDB(t, pool)

	backend := New(pool)
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

// Helper test error type
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
