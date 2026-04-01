// Command simple demonstrates basic QueueKit usage with a worker pool.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/reckziegelwilliam/queuekit/internal/backend/postgres"
	"github.com/reckziegelwilliam/queuekit/internal/queue"
	"github.com/reckziegelwilliam/queuekit/internal/worker"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://queuekit:queuekit@localhost:5432/queuekit?sslmode=disable"
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	be, err := postgres.NewFromDSN(ctx, dsn)
	if err != nil {
		logger.Error("failed to connect", "error", err)
		os.Exit(1)
	}
	defer func() { _ = be.Close() }()

	if err := postgres.RunMigrations(ctx, nil); err != nil {
		logger.Warn("migration skipped (run manually)", "error", err)
	}

	// Register a handler for "greet" jobs
	registry := worker.NewRegistry()
	registry.Register("greet", func(ctx context.Context, job *queue.Job) error {
		var payload struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("unmarshal payload: %w", err)
		}
		logger.Info("Hello!", "name", payload.Name, "job_id", job.ID)
		return nil
	})

	// Enqueue a sample job
	job := queue.NewJob("greet", "default", json.RawMessage(`{"name":"World"}`))
	if err := be.Enqueue(ctx, job); err != nil {
		logger.Error("enqueue failed", "error", err)
		os.Exit(1)
	}
	logger.Info("enqueued job", "id", job.ID)

	// Start a worker pool
	pool := worker.NewPool(be, registry, []worker.QueueConfig{
		{Name: "default", Concurrency: 2},
	}, worker.WithPoolLogger(logger))

	if err := pool.Start(ctx); err != nil {
		logger.Error("pool start failed", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()
	pool.Stop()
	logger.Info("done")
}
