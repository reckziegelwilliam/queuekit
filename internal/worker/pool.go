package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/reckziegelwilliam/queuekit/internal/backend"
)

// QueueConfig holds the processing configuration for a single queue.
type QueueConfig struct {
	// Name is the queue to consume from.
	Name string
	// Concurrency is the number of workers to run in parallel for this queue.
	// Defaults to 1 if zero or negative.
	Concurrency int
}

// Pool manages a group of workers across one or more queues.
// Start it once; Stop waits for all in-flight jobs to complete before returning.
type Pool struct {
	backend    backend.Backend
	registry   *Registry
	queues     []QueueConfig
	logger     *slog.Logger
	workerOpts []Option

	mu      sync.Mutex
	workers []*Worker
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
}

// PoolOption is a functional option for Pool.
type PoolOption func(*Pool)

// WithPoolLogger sets a custom logger for the pool and all workers it spawns.
func WithPoolLogger(l *slog.Logger) PoolOption {
	return func(p *Pool) {
		p.logger = l
		p.workerOpts = append(p.workerOpts, WithWorkerLogger(l))
	}
}

// WithPoolWorkerOptions appends worker-level options applied to every worker the pool creates.
func WithPoolWorkerOptions(opts ...Option) PoolOption {
	return func(p *Pool) { p.workerOpts = append(p.workerOpts, opts...) }
}

// NewPool creates a Pool that will dispatch jobs from the listed queues.
func NewPool(b backend.Backend, r *Registry, queues []QueueConfig, opts ...PoolOption) *Pool {
	p := &Pool{
		backend:  b,
		registry: r,
		queues:   queues,
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Start launches all workers as goroutines. It returns immediately; workers run
// in the background until Stop is called or the parent context is canceled.
//
// Start returns an error if the pool is already running.
func (p *Pool) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return fmt.Errorf("pool is already running")
	}

	poolCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.started = true

	workerIdx := 0
	for _, q := range p.queues {
		concurrency := q.Concurrency
		if concurrency <= 0 {
			concurrency = 1
		}
		for i := 0; i < concurrency; i++ {
			id := fmt.Sprintf("worker-%d", workerIdx)
			workerIdx++

			w := NewWorker(id, q.Name, p.backend, p.registry, p.workerOpts...)
			p.workers = append(p.workers, w)

			p.wg.Add(1)
			go func(w *Worker) {
				defer p.wg.Done()
				w.Run(poolCtx)
			}(w)
		}
	}

	p.logger.Info("worker pool started",
		"queues", len(p.queues),
		"total_workers", workerIdx,
	)
	return nil
}

// Stop cancels the pool's context and waits for all workers to finish their
// current job before returning. It is safe to call Stop multiple times.
func (p *Pool) Stop() {
	p.mu.Lock()
	cancel := p.cancel
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	p.wg.Wait()

	p.mu.Lock()
	p.started = false
	p.workers = nil
	p.cancel = nil
	p.mu.Unlock()

	p.logger.Info("worker pool stopped")
}

// States returns a snapshot of every worker's current state. Safe to call at
// any time, including while the pool is running.
func (p *Pool) States() []WorkerState {
	p.mu.Lock()
	workers := make([]*Worker, len(p.workers))
	copy(workers, p.workers)
	p.mu.Unlock()

	states := make([]WorkerState, len(workers))
	for i, w := range workers {
		states[i] = w.State()
	}
	return states
}

// Register is a convenience method that delegates to the pool's Registry.
func (p *Pool) Register(jobType string, h Handler) {
	p.registry.Register(jobType, h)
}
