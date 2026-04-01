// Command cron demonstrates recurring job scheduling with QueueKit.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	registry := worker.NewRegistry()
	registry.Register("cleanup", func(ctx context.Context, job *queue.Job) error {
		logger.Info("running periodic cleanup", "job_id", job.ID, "time", time.Now().Format(time.RFC3339))
		return nil
	})

	// Enqueue a cleanup job every 30 seconds
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		enqueue := func() {
			payload := json.RawMessage(fmt.Sprintf(`{"scheduled_at":"%s"}`, time.Now().Format(time.RFC3339)))
			job := queue.NewJob("cleanup", "cron", payload)
			if err := be.Enqueue(ctx, job); err != nil {
				logger.Error("enqueue cron job failed", "error", err)
				return
			}
			logger.Info("enqueued cron job", "id", job.ID)
		}

		enqueue()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				enqueue()
			}
		}
	}()

	pool := worker.NewPool(be, registry, []worker.QueueConfig{
		{Name: "cron", Concurrency: 1},
	}, worker.WithPoolLogger(logger))

	if err := pool.Start(ctx); err != nil {
		logger.Error("pool start failed", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()
	pool.Stop()
	logger.Info("done")
}
