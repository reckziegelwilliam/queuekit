package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

// ---------------------------------------------------------------------------
// Mock backend
// ---------------------------------------------------------------------------

type mockBackend struct {
	mu     sync.Mutex
	jobs   map[string]*queue.Job
	queued []*queue.Job
	acked  []string
	nacked []string
}

func newMockBackend() *mockBackend {
	return &mockBackend{jobs: make(map[string]*queue.Job)}
}

func (m *mockBackend) Enqueue(_ context.Context, job *queue.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.ID] = job
	m.queued = append(m.queued, job)
	return nil
}

func (m *mockBackend) Reserve(_ context.Context, queueName string) (*queue.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, j := range m.queued {
		if j.Queue == queueName && j.Status == queue.StatusPending {
			m.queued = append(m.queued[:i], m.queued[i+1:]...)
			j.Status = queue.StatusRunning
			return j, nil
		}
	}
	return nil, nil
}

func (m *mockBackend) Ack(_ context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = append(m.acked, jobID)
	if j, ok := m.jobs[jobID]; ok {
		j.Status = queue.StatusCompleted
	}
	return nil
}

func (m *mockBackend) Nack(_ context.Context, jobID string, err error, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nacked = append(m.nacked, jobID)
	if j, ok := m.jobs[jobID]; ok {
		j.Attempts++
		if j.Attempts >= j.MaxAttempts {
			j.Status = queue.StatusDead
		} else {
			// Re-schedule for immediate retry in the mock
			j.Status = queue.StatusPending
			m.queued = append(m.queued, j)
		}
		if err != nil {
			j.LastError = err.Error()
		}
	}
	return nil
}

func (m *mockBackend) MoveToDLQ(_ context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[jobID]; ok {
		j.Status = queue.StatusDead
	}
	return nil
}

func (m *mockBackend) ListQueues(_ context.Context) ([]queue.Queue, error) { return nil, nil }
func (m *mockBackend) ListJobs(_ context.Context, _, _ string, _, _ int) ([]*queue.Job, error) {
	return nil, nil
}
func (m *mockBackend) GetJob(_ context.Context, jobID string) (*queue.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}
	return j, nil
}
func (m *mockBackend) DeleteJob(_ context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, jobID)
	return nil
}
func (m *mockBackend) Close() error { return nil }

// ---------------------------------------------------------------------------
// Backoff tests
// ---------------------------------------------------------------------------

func TestFixedBackoff(t *testing.T) {
	b := &FixedBackoff{Delay: 5 * time.Second}
	for _, attempt := range []int{0, 1, 5, 100} {
		assert.Equal(t, 5*time.Second, b.NextDelay(attempt),
			"fixed backoff should always return the same delay")
	}
}

func TestExponentialBackoff(t *testing.T) {
	b := &ExponentialBackoff{
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Factor:       2.0,
	}

	assert.Equal(t, 1*time.Second, b.NextDelay(0), "attempt 0 should equal InitialDelay")
	assert.Equal(t, 1*time.Second, b.NextDelay(1), "attempt 1 should equal InitialDelay")
	assert.Equal(t, 2*time.Second, b.NextDelay(2))
	assert.Equal(t, 4*time.Second, b.NextDelay(3))
	assert.Equal(t, 8*time.Second, b.NextDelay(4))
	// Cap at MaxDelay
	assert.Equal(t, 30*time.Second, b.NextDelay(10), "should be capped at MaxDelay")
}

func TestDefaultExponentialBackoff(t *testing.T) {
	b := DefaultExponentialBackoff()
	assert.NotNil(t, b)
	assert.Greater(t, b.NextDelay(5), b.NextDelay(1), "delay should increase with attempts")
	assert.Equal(t, b.MaxDelay, b.NextDelay(100), "very high attempt should return MaxDelay")
}

func TestNoBackoff(t *testing.T) {
	b := NoBackoff()
	assert.Equal(t, time.Duration(0), b.NextDelay(5))
}

// ---------------------------------------------------------------------------
// Handler registry tests
// ---------------------------------------------------------------------------

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	called := false
	h := func(_ context.Context, _ *queue.Job) error {
		called = true
		return nil
	}

	r.Register("email.send", h)

	got, ok := r.Get("email.send")
	require.True(t, ok)
	require.NotNil(t, got)
	_ = got(context.Background(), nil)
	assert.True(t, called)
}

func TestRegistry_GetMissing(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	assert.False(t, ok)
}

func TestRegistry_Overwrite(t *testing.T) {
	r := NewRegistry()
	r.Register("type.a", func(_ context.Context, _ *queue.Job) error { return nil })

	var called string
	r.Register("type.a", func(_ context.Context, _ *queue.Job) error {
		called = "second"
		return nil
	})

	got, ok := r.Get("type.a")
	require.True(t, ok)
	_ = got(context.Background(), nil)
	assert.Equal(t, "second", called, "second registration should overwrite the first")
}

// ---------------------------------------------------------------------------
// Worker tests
// ---------------------------------------------------------------------------

func newTestJob(jobType, queueName string) *queue.Job {
	return queue.NewJob(jobType, queueName, json.RawMessage(`{"test":true}`))
}

func TestWorker_SuccessfulJob(t *testing.T) {
	b := newMockBackend()
	r := NewRegistry()

	job := newTestJob("email.send", "default")
	require.NoError(t, b.Enqueue(context.Background(), job))

	executed := make(chan string, 1)
	r.Register("email.send", func(_ context.Context, j *queue.Job) error {
		executed <- j.ID
		return nil
	})

	w := NewWorker("w1", "default", b, r,
		WithBackoff(NoBackoff()),
		WithPollInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go w.Run(ctx)

	select {
	case gotID := <-executed:
		assert.Equal(t, job.ID, gotID)
	case <-ctx.Done():
		t.Fatal("handler was never called")
	}

	// Give worker time to call Ack
	time.Sleep(50 * time.Millisecond)

	b.mu.Lock()
	defer b.mu.Unlock()
	assert.Contains(t, b.acked, job.ID, "job should have been acked")
}

func TestWorker_FailedJobNacked(t *testing.T) {
	b := newMockBackend()
	r := NewRegistry()

	job := newTestJob("risky.task", "default")
	job.MaxAttempts = 3
	require.NoError(t, b.Enqueue(context.Background(), job))

	r.Register("risky.task", func(_ context.Context, _ *queue.Job) error {
		return errors.New("transient error")
	})

	w := NewWorker("w1", "default", b, r,
		WithBackoff(NoBackoff()),
		WithPollInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go w.Run(ctx)
	<-ctx.Done()

	b.mu.Lock()
	nacked := b.nacked
	b.mu.Unlock()

	assert.NotEmpty(t, nacked, "job should have been nacked at least once")
	assert.Contains(t, nacked, job.ID)
}

func TestWorker_NoHandlerNacks(t *testing.T) {
	b := newMockBackend()
	r := NewRegistry() // no handlers registered

	job := newTestJob("unknown.type", "default")
	require.NoError(t, b.Enqueue(context.Background(), job))

	w := NewWorker("w1", "default", b, r,
		WithBackoff(NoBackoff()),
		WithPollInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go w.Run(ctx)
	<-ctx.Done()

	b.mu.Lock()
	nacked := b.nacked
	b.mu.Unlock()

	assert.Contains(t, nacked, job.ID, "job with no handler should be nacked")
}

func TestWorker_StateTransitions(t *testing.T) {
	b := newMockBackend()
	r := NewRegistry()

	block := make(chan struct{})
	r.Register("blocking.job", func(_ context.Context, _ *queue.Job) error {
		<-block
		return nil
	})

	job := newTestJob("blocking.job", "default")
	require.NoError(t, b.Enqueue(context.Background(), job))

	w := NewWorker("w1", "default", b, r,
		WithPollInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)

	// Worker should eventually transition to running
	assert.Eventually(t, func() bool {
		return w.State().Status == StatusRunning
	}, 500*time.Millisecond, 10*time.Millisecond)

	// Unblock the job
	close(block)

	// Worker should return to idle
	assert.Eventually(t, func() bool {
		return w.State().Status == StatusIdle
	}, 500*time.Millisecond, 10*time.Millisecond)

	cancel()

	// Worker should stop
	assert.Eventually(t, func() bool {
		return w.State().Status == StatusStopped
	}, 500*time.Millisecond, 10*time.Millisecond)
}

// ---------------------------------------------------------------------------
// Pool tests
// ---------------------------------------------------------------------------

func TestPool_StartStop(t *testing.T) {
	b := newMockBackend()
	r := NewRegistry()
	r.Register("noop", func(_ context.Context, _ *queue.Job) error { return nil })

	p := NewPool(b, r, []QueueConfig{
		{Name: "default", Concurrency: 2},
		{Name: "priority", Concurrency: 1},
	})

	ctx := context.Background()
	require.NoError(t, p.Start(ctx))

	states := p.States()
	assert.Len(t, states, 3, "should have 2+1=3 workers")

	p.Stop()

	// After Stop, States returns empty slice
	assert.Empty(t, p.States())
}

func TestPool_DoubleStartReturnsError(t *testing.T) {
	b := newMockBackend()
	r := NewRegistry()
	p := NewPool(b, r, []QueueConfig{{Name: "default", Concurrency: 1}})

	ctx := context.Background()
	require.NoError(t, p.Start(ctx))
	defer p.Stop()

	err := p.Start(ctx)
	assert.Error(t, err, "starting an already-running pool should return an error")
}

func TestPool_ProcessesJobs(t *testing.T) {
	b := newMockBackend()
	r := NewRegistry()

	const numJobs = 5
	processed := make(chan string, numJobs)

	r.Register("test.job", func(_ context.Context, j *queue.Job) error {
		processed <- j.ID
		return nil
	})

	for i := 0; i < numJobs; i++ {
		job := newTestJob("test.job", "default")
		require.NoError(t, b.Enqueue(context.Background(), job))
	}

	p := NewPool(b, r, []QueueConfig{{Name: "default", Concurrency: 3}},
		WithPoolWorkerOptions(
			WithBackoff(NoBackoff()),
			WithPollInterval(10*time.Millisecond),
		),
	)

	ctx := context.Background()
	require.NoError(t, p.Start(ctx))

	// Collect all processed job IDs
	seen := make(map[string]bool)
	timeout := time.After(2 * time.Second)
	for len(seen) < numJobs {
		select {
		case id := <-processed:
			seen[id] = true
		case <-timeout:
			t.Fatalf("only %d/%d jobs processed before timeout", len(seen), numJobs)
		}
	}

	p.Stop()
	assert.Len(t, seen, numJobs, "all jobs should have been processed exactly once")
}

func TestPool_Register(t *testing.T) {
	b := newMockBackend()
	r := NewRegistry()
	p := NewPool(b, r, []QueueConfig{{Name: "q", Concurrency: 1}})

	called := false
	p.Register("my.type", func(_ context.Context, _ *queue.Job) error {
		called = true
		return nil
	})

	h, ok := r.Get("my.type")
	require.True(t, ok)
	_ = h(context.Background(), nil)
	assert.True(t, called)
}

func TestPool_DefaultConcurrency(t *testing.T) {
	b := newMockBackend()
	r := NewRegistry()

	// Concurrency 0 should default to 1
	p := NewPool(b, r, []QueueConfig{{Name: "default", Concurrency: 0}})

	ctx := context.Background()
	require.NoError(t, p.Start(ctx))
	defer p.Stop()

	assert.Len(t, p.States(), 1)
}
