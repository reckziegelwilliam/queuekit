package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/reckziegelwilliam/queuekit/internal/backend"
	"github.com/reckziegelwilliam/queuekit/internal/backend/postgres"
	redisbe "github.com/reckziegelwilliam/queuekit/internal/backend/redis"
	"github.com/reckziegelwilliam/queuekit/internal/config"
	"github.com/reckziegelwilliam/queuekit/internal/dashboard"
	"github.com/reckziegelwilliam/queuekit/internal/httpapi"
	"github.com/reckziegelwilliam/queuekit/internal/worker"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	versionFlag := flag.Bool("version", false, "Print version information")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("queuekitd version %s (commit: %s, built: %s)\n", Version, Commit, BuildTime)
		os.Exit(0)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	be, err := createBackend(ctx, cfg)
	if err != nil {
		logger.Error("failed to create backend", "error", err)
		os.Exit(1)
	}
	defer be.Close()

	registry := worker.NewRegistry()
	pool := worker.NewPool(be, registry, nil, worker.WithPoolLogger(logger))

	srv := httpapi.NewServer(be, cfg.APIKey, logger)
	dash := dashboard.New(be, logger)
	srv.MountDashboard(dash)

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("starting HTTP server", "addr", cfg.ListenAddr, "backend", cfg.Backend)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	if err := pool.Start(ctx); err != nil {
		logger.Error("failed to start worker pool", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()
	logger.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool.Stop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}

	logger.Info("shutdown complete")

	// Keep the pool and registry accessible for future use (e.g. dashboard)
	_ = pool
	_ = registry
}

func createBackend(ctx context.Context, cfg *config.Config) (backend.Backend, error) {
	switch cfg.Backend {
	case "postgres":
		return postgres.NewFromDSN(ctx, cfg.PostgresDSN)
	case "redis":
		opts, err := goredis.ParseURL("redis://" + cfg.RedisAddr)
		if err != nil {
			opts = &goredis.Options{Addr: cfg.RedisAddr}
		}
		return redisbe.NewFromOptions(opts)
	default:
		return nil, fmt.Errorf("unknown backend: %s", cfg.Backend)
	}
}
