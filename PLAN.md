# QueueKit – Implementation Plan

## Phase 0 – Repo & Skeleton ✅

- [x] Initialize Go module: `github.com/reckziegelwilliam/queuekit`
- [x] Create basic structure:
  - [x] `cmd/queuekitd` – server/worker binary
  - [x] `cmd/queuekit` – CLI tool
  - [x] `internal/queue` – core domain types
  - [x] `internal/backend` – Postgres/Redis adapters
  - [x] `internal/worker` – worker pool
  - [x] `internal/httpapi` – HTTP handlers
  - [x] `internal/dashboard` – templates and handlers
- [x] Add `Makefile` / `taskfile` for common commands
- [x] Set up Go linters and CI (GitHub Actions)

## Phase 1 – Core Domain & Storage ✅

- [x] Define job model:
  - [x] `Job` (id, type, queue, payload, status, attempts, scheduled_at, etc.)
  - [x] `Queue` model and statuses
- [x] Implement backend interfaces:
  - [x] `Backend` interface (enqueue, reserve, ack, nack, moveToDLQ, listQueues, listJobs)
  - [x] Postgres implementation (including migrations)
  - [x] Redis implementation (fast queue operations, locks)
- [x] Unit tests for backend behavior

## Phase 2 – Worker Pool & Execution ✅

- [x] Implement worker pool:
  - [x] Multiple workers per queue (configurable concurrency per queue)
  - [x] Graceful shutdown (context cancellation, waits for in-flight jobs)
  - [x] Worker status snapshots (idle / running / stopped + last job ID)
- [x] Implement retry & backoff strategies:
  - [x] Fixed backoff (`FixedBackoff`)
  - [x] Exponential backoff (`ExponentialBackoff`, capped at MaxDelay)
  - [x] Max attempts → DLQ (handled in backend `Nack`)
- [x] Worker handler registration:
  - [x] Map job type → handler func (`Registry`)
  - [x] Context with job metadata passed to every handler
- [x] Basic logging and instrumentation hooks (`log/slog`, per-event structured logs)
- [x] Fix retry bug: backends now re-schedule non-final failures as `pending` with backoff delay

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

