# QueueKit

> A minimal, self-hosted job queue and scheduler written in Go.
> Think Sidekiq/BullMQ, but "boring Go" with HTML templates and Postgres/Redis under the hood.

QueueKit is a background job system for services that need retries, backoff, and scheduled work without dragging in a huge stack or JS-heavy dashboards.

- **Concurrency-first**: built with Go's goroutines and channels.
- **Job queues + scheduled jobs**: fire-and-forget work and cron-style recurring jobs.
- **Retries & backoff**: configurable fixed/exponential strategies, dead-letter queue.
- **Postgres / Redis**: pluggable backend for durability and speed.
- **Dashboard**: Go `html/template` + minimal CSS, no SPA.
- **CLI tooling**: `queuekit` for enqueueing, inspecting, and managing jobs.

---

## Quick Start

### With Docker Compose

```bash
docker compose up --build
```

This starts Postgres, Redis, and the `queuekitd` server on port 8080.

- **API**: http://localhost:8080/v1/queues
- **Dashboard**: http://localhost:8080/dashboard

### Without Docker

```bash
# Prerequisites: Postgres and Redis running locally

export QUEUEKIT_BACKEND=postgres
export QUEUEKIT_POSTGRES_DSN="postgres://user:pass@localhost:5432/queuekit?sslmode=disable"
export QUEUEKIT_API_KEY="your-secret-key"

go run ./cmd/queuekitd
```

---

## API Reference

All `/v1/*` endpoints require an `Authorization: Bearer <API_KEY>` header.

| Method   | Path                          | Description              |
|----------|-------------------------------|--------------------------|
| `POST`   | `/v1/jobs`                    | Enqueue a new job        |
| `GET`    | `/v1/jobs/{id}`               | Get job details          |
| `DELETE` | `/v1/jobs/{id}`               | Delete a job             |
| `POST`   | `/v1/jobs/{id}/retry`         | Retry a failed/dead job  |
| `POST`   | `/v1/jobs/{id}/cancel`        | Cancel a pending job     |
| `GET`    | `/v1/queues`                  | List all queues          |
| `GET`    | `/v1/queues/{name}/jobs`      | List jobs in a queue     |

### Enqueue a Job

```bash
curl -X POST http://localhost:8080/v1/jobs \
  -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "send_email",
    "queue": "emails",
    "payload": {"to": "user@example.com", "subject": "Hello"},
    "priority": 20,
    "max_attempts": 5
  }'
```

### List Queues

```bash
curl http://localhost:8080/v1/queues \
  -H "Authorization: Bearer your-secret-key"
```

### List Jobs (with filtering)

```bash
curl "http://localhost:8080/v1/queues/emails/jobs?status=pending&limit=10" \
  -H "Authorization: Bearer your-secret-key"
```

---

## Dashboard

The dashboard is available at `/dashboard` and provides:

- **Queues overview**: pending, running, completed, failed, and dead counts with health scores
- **Queue detail**: filterable job list with status tabs and pagination
- **Job detail**: full payload view, error details, and retry button

---

## CLI

```bash
go install ./cmd/queuekit

# Or use the built binary
./queuekit --help
```

### Configuration

Create `~/.config/queuekit/config.yaml`:

```yaml
server: http://localhost:8080
api_key: your-secret-key
```

Or pass flags directly:

```bash
queuekit --server http://localhost:8080 --api-key your-secret-key inspect queues
```

### Commands

```bash
# Enqueue a job
queuekit enqueue send_email --queue emails --payload '{"to":"user@example.com"}'

# Inspect queues
queuekit inspect queues

# Inspect jobs in a queue
queuekit inspect jobs --queue emails --status pending --limit 20

# Retry a failed job
queuekit retry <job-id>

# Cancel a pending job
queuekit cancel <job-id>
```

---

## Using as a Library

### Register handlers and run workers

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"

    "github.com/reckziegelwilliam/queuekit/internal/backend/postgres"
    "github.com/reckziegelwilliam/queuekit/internal/queue"
    "github.com/reckziegelwilliam/queuekit/internal/worker"
)

func main() {
    ctx := context.Background()
    be, _ := postgres.NewFromDSN(ctx, "postgres://...")
    defer be.Close()

    registry := worker.NewRegistry()
    registry.Register("send_email", func(ctx context.Context, job *queue.Job) error {
        fmt.Println("Sending email for job", job.ID)
        return nil
    })

    // Enqueue a job
    job := queue.NewJob("send_email", "emails", json.RawMessage(`{"to":"user@example.com"}`))
    be.Enqueue(ctx, job)

    // Start workers
    pool := worker.NewPool(be, registry, []worker.QueueConfig{
        {Name: "emails", Concurrency: 4},
    }, worker.WithPoolLogger(slog.Default()))

    pool.Start(ctx)
    // ... wait for shutdown signal ...
    pool.Stop()
}
```

See [`examples/simple/`](examples/simple/) and [`examples/cron/`](examples/cron/) for complete working examples.

---

## Architecture

```
┌──────────┐     ┌────────────────────────────────────────┐
│ queuekit │────▶│              queuekitd                 │
│   (CLI)  │HTTP │  ┌──────────┐  ┌──────────────────┐   │
└──────────┘     │  │ HTTP API │  │    Dashboard      │   │
                 │  │  /v1/*   │  │   /dashboard/*    │   │
                 │  └────┬─────┘  └────────┬──────────┘   │
                 │       │                 │              │
                 │       ▼                 ▼              │
                 │  ┌──────────────────────────────┐      │
                 │  │       backend.Backend         │      │
                 │  ├──────────────┬───────────────┤      │
                 │  │   Postgres   │     Redis     │      │
                 │  └──────────────┴───────────────┘      │
                 │       │                                │
                 │       ▼                                │
                 │  ┌──────────────────┐                  │
                 │  │   Worker Pool    │                  │
                 │  │  (goroutines)    │                  │
                 │  └──────────────────┘                  │
                 └────────────────────────────────────────┘
```

---

## Configuration

`queuekitd` is configured via environment variables:

| Variable               | Default        | Description                      |
|------------------------|----------------|----------------------------------|
| `QUEUEKIT_LISTEN_ADDR` | `:8080`        | HTTP listen address              |
| `QUEUEKIT_API_KEY`     |                | API key for `/v1/*` auth         |
| `QUEUEKIT_BACKEND`     | `postgres`     | Backend type: `postgres`/`redis` |
| `QUEUEKIT_POSTGRES_DSN`|                | Postgres connection string       |
| `QUEUEKIT_REDIS_ADDR`  | `localhost:6379`| Redis address                   |

---

## Development

```bash
# Run tests
make test

# Run linter
make lint

# Build binaries
make build

# Run server locally
make run
```

---

## Project Status

This is a portfolio / learning project showcasing production-ready Go patterns:
concurrency, reliability, and observability, with minimalist but usable dashboards.

See [PLAN.md](PLAN.md) for the full implementation roadmap.
