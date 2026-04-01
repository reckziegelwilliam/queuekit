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

## Phase 3 – HTTP API ✅

- [x] Choose router (`chi/v5`)
- [x] Implement endpoints:
  - [x] `POST /v1/jobs` – enqueue
  - [x] `GET /v1/queues` – list queues
  - [x] `GET /v1/queues/{name}/jobs` – list jobs
  - [x] `GET /v1/jobs/{id}` – get job
  - [x] `POST /v1/jobs/{id}/retry`
  - [x] `POST /v1/jobs/{id}/cancel`
  - [x] `DELETE /v1/jobs/{id}` – delete job
- [x] API key authentication (Bearer token middleware)
- [x] JSON validation (leverages `job.Validate()`)
- [x] Integration tests (19 tests with mock backend)

## Phase 4 – Dashboard ✅

- [x] Setup `html/template` with embedded layout
- [x] Views:
  - [x] Queues overview (counts, health scores)
  - [x] Queue details (jobs, status filter tabs, pagination)
  - [x] Job details panel (payload, error, retry button)
- [x] Minimal CSS (no framework, embedded in layout)
- [x] Server-side sorting/filtering (no SPA)

## Phase 5 – CLI (`queuekit`) ✅

- [x] Set up CLI using `cobra`
- [x] Commands:
  - [x] `queuekit enqueue <type> --queue <name> --payload <json>`
  - [x] `queuekit inspect queues`
  - [x] `queuekit inspect jobs --queue <name>`
  - [x] `queuekit retry <job-id>`
  - [x] `queuekit cancel <job-id>`
- [x] Global config via YAML (`~/.config/queuekit/config.yaml`) with viper

## Phase 6 – Packaging & Examples ✅

- [x] Multi-stage Dockerfile for `queuekitd` (distroless final image)
- [x] `docker-compose.yml` with Postgres + Redis
- [x] Example integrations:
  - [x] Simple Go service (`examples/simple/`)
  - [x] Cron-style recurring jobs (`examples/cron/`)
- [x] README polish with API reference, CLI usage, architecture diagram

