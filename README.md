1️⃣ QueueKit (Go Job Queue & Scheduler)
README.md
# QueueKit

> A minimal, self-hosted job queue and scheduler written in Go.  
> Think Sidekiq/BullMQ, but "boring Go" with HTML templates and Postgres/Redis under the hood.

QueueKit is a background job system for services that need retries, backoff, idempotency, and scheduled work
without dragging in a huge stack or JS-heavy dashboards.

- 🧵 **Concurrency-first**: built with Go's goroutines and channels.
- 📬 **Job queues + scheduled jobs**: fire-and-forget work and cron-style recurring jobs.
- 🔁 **Retries & backoff**: configurable strategies, dead-letter queue.
- 🗃️ **Postgres / Redis**: pluggable backend for durability and speed.
- 📊 **Tiny dashboard**: Go `html/template` + minimal CSS, no SPA.
- 🛠️ **CLI tooling**: `queuekit` for enqueueing, inspecting, and managing jobs.

---

## Architecture

High-level components:

- **API server (`queuekitd`)**
  - HTTP API to enqueue jobs, inspect queues, and manage schedules.
  - Exposes Prometheus metrics (optional).
- **Worker pool**
  - Runs inside the same process or separate worker processes.
  - Pulls jobs from backend, executes registered handlers.
- **Backend**
  - **Postgres**: durable job storage, job history, DLQ.
  - **Redis**: fast queue/lock operations.
- **Dashboard**
  - Simple HTML views for queues, workers, failures, schedules.

```text
Client → HTTP API → Backend (Postgres/Redis) → Workers → Dashboard

Tech Stack

Language: Go (1.22+)

Backend: Postgres + Redis

HTTP: net/http + chi/echo/gorilla (TBD)

Templates: html/template

CLI: cobra or stdlib flag

Quickstart (planned UX)
# run server + workers
queuekitd serve --config ./queuekit.yaml

# enqueue a job from CLI
queuekit enqueue email.send \
  --payload '{"to":"user@example.com","subject":"Hi"}' \
  --queue critical

# inspect queues
queuekit inspect queues
queuekit inspect jobs --queue critical


Then open the dashboard:

http://localhost:8080

Job Definition (example)

In your Go service:

import "github.com/yourname/queuekit/client"

func main() {
    c := client.New("http://localhost:8080", client.WithAPIKey("secret"))

    _ = c.Enqueue(context.Background(), client.Job{
        Queue: "emails",
        Type:  "email.send",
        Payload: map[string]any{
            "to":      "user@example.com",
            "subject": "Welcome",
        },
    })
}


Worker registration:

import "github.com/yourname/queuekit/worker"

func main() {
    w := worker.New(worker.Config{ /* ... */ })

    w.Handle("email.send", func(ctx context.Context, job worker.Job) error {
        // send email here
        return nil
    })

    w.Run()
}

Status

This is a portfolio / learning project intended to showcase:

Production-ready Go code organization.

Concurrency, reliability, and observability patterns.

Minimalist but usable dashboards without any frontend frameworks.

Not yet production hardened. Use at your own risk.

Roadmap (high level)

 Core job model and storage

 Redis + Postgres backends

 Worker pool with backoff & DLQ

 HTTP API + auth

 HTML dashboard

 CLI (queuekit / queuekitd)

 Docker & example deployment


### `PLAN.md`

```md
# QueueKit – Implementation Plan

## Phase 0 – Repo & Skeleton

- [ ] Initialize Go module: `github.com/<you>/queuekit`
- [ ] Create basic structure:
  - [ ] `cmd/queuekitd` – server/worker binary
  - [ ] `cmd/queuekit` – CLI tool
  - [ ] `internal/queue` – core domain types
  - [ ] `internal/backend` – Postgres/Redis adapters
  - [ ] `internal/worker` – worker pool
  - [ ] `internal/httpapi` – HTTP handlers
  - [ ] `internal/dashboard` – templates and handlers
- [ ] Add `Makefile` / `taskfile` for common commands
- [ ] Set up Go linters and CI (GitHub Actions)

## Phase 1 – Core Domain & Storage

- [ ] Define job model:
  - [ ] `Job` (id, type, queue, payload, status, attempts, scheduled_at, etc.)
  - [ ] `Queue` model and statuses
- [ ] Implement backend interfaces:
  - [ ] `Backend` interface (enqueue, reserve, ack, nack, moveToDLQ, listQueues, listJobs)
  - [ ] Postgres implementation (including migrations)
  - [ ] Redis implementation (fast queue operations, locks)
- [ ] Unit tests for backend behavior

## Phase 2 – Worker Pool & Execution

- [ ] Implement worker pool:
  - [ ] Multiple workers per queue
  - [ ] Graceful shutdown
  - [ ] Heartbeats / worker status
- [ ] Implement retry & backoff strategies:
  - [ ] Fixed backoff
  - [ ] Exponential backoff
  - [ ] Max attempts → DLQ
- [ ] Worker handler registration:
  - [ ] Map job type → handler func
  - [ ] Context with job metadata
- [ ] Basic logging and instrumentation hooks

## Phase 3 – HTTP API

- [ ] Choose router (chi/echo)
- [ ] Implement endpoints:
  - [ ] `POST /v1/jobs` – enqueue
  - [ ] `GET /v1/queues` – list queues
  - [ ] `GET /v1/queues/{name}/jobs` – list jobs
  - [ ] `POST /v1/jobs/{id}/retry`
  - [ ] `POST /v1/jobs/{id}/cancel`
- [ ] API key authentication
- [ ] JSON schema & validation
- [ ] Integration tests against Postgres/Redis backends

## Phase 4 – Dashboard

- [ ] Setup `html/template` with basic layout
- [ ] Views:
  - [ ] Queues overview (throughput, latency, failures)
  - [ ] Queue details (jobs, statuses, pagination)
  - [ ] Job details panel (payload, error, retry)
  - [ ] Schedules view
- [ ] Minimal CSS (no framework)
- [ ] Server-side sorting/filtering (no SPA)

## Phase 5 – CLI (`queuekit`)

- [ ] Set up CLI using `cobra` or stdlib `flag`
- [ ] Commands:
  - [ ] `queuekit enqueue <type> --queue <name> --payload <json>`
  - [ ] `queuekit inspect queues`
  - [ ] `queuekit inspect jobs --queue <name>`
  - [ ] `queuekit retry <job-id>`
- [ ] Global config via YAML (`~/.config/queuekit/config.yaml`)

## Phase 6 – Packaging & Examples

- [ ] Dockerfile for `queuekitd`
- [ ] `docker-compose.yml` with Postgres + Redis
- [ ] Example integrations:
  - [ ] Simple Go service using the client
  - [ ] Cron-style recurring jobs
- [ ] Final README polish and screenshots
