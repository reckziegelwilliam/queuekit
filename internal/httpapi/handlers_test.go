// Package httpapi_test contains integration tests for the HTTP API handlers.
package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/reckziegelwilliam/queuekit/internal/backend"
	"github.com/reckziegelwilliam/queuekit/internal/httpapi"
	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

// --- mock backend ------------------------------------------------------------

var _ backend.Backend = (*mockBackend)(nil)

type mockBackend struct {
	mu   sync.Mutex
	jobs map[string]*queue.Job
}

func newMockBackend() *mockBackend {
	return &mockBackend{jobs: make(map[string]*queue.Job)}
}

func (m *mockBackend) Enqueue(_ context.Context, job *queue.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.ID] = job
	return nil
}

func (m *mockBackend) Reserve(_ context.Context, queueName string) (*queue.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.Queue == queueName && j.Status == queue.StatusPending {
			j.Status = queue.StatusRunning
			return j, nil
		}
	}
	return nil, nil
}

func (m *mockBackend) Ack(_ context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}
	j.Status = queue.StatusCompleted
	return nil
}

func (m *mockBackend) Nack(_ context.Context, jobID string, jobErr error, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}
	j.Attempts++
	if jobErr != nil {
		j.LastError = jobErr.Error()
	}
	if j.Attempts >= j.MaxAttempts {
		j.Status = queue.StatusDead
	} else {
		j.Status = queue.StatusPending
	}
	return nil
}

func (m *mockBackend) MoveToDLQ(_ context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}
	j.Status = queue.StatusDead
	return nil
}

func (m *mockBackend) ListQueues(_ context.Context) ([]queue.Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stats := make(map[string]*queue.Queue)
	for _, j := range m.jobs {
		q, ok := stats[j.Queue]
		if !ok {
			q = &queue.Queue{Name: j.Queue}
			stats[j.Queue] = q
		}
		switch j.Status {
		case queue.StatusPending:
			q.Size++
		case queue.StatusRunning:
			q.ProcessingCount++
		case queue.StatusCompleted:
			q.CompletedCount++
		case queue.StatusFailed:
			q.FailedCount++
		case queue.StatusDead:
			q.DeadCount++
		}
	}
	result := make([]queue.Queue, 0, len(stats))
	for _, q := range stats {
		result = append(result, *q)
	}
	return result, nil
}

func (m *mockBackend) ListJobs(_ context.Context, queueName, status string, limit, offset int) ([]*queue.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*queue.Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		if queueName != "" && j.Queue != queueName {
			continue
		}
		if status != "" && j.Status != status {
			continue
		}
		result = append(result, j)
	}
	if offset > len(result) {
		return nil, nil
	}
	result = result[offset:]
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result, nil
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
	if _, ok := m.jobs[jobID]; !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}
	delete(m.jobs, jobID)
	return nil
}

func (m *mockBackend) Close() error { return nil }

// --- test helpers ------------------------------------------------------------

const testAPIKey = "test-secret-key"

func newTestServer(b backend.Backend) *httpapi.Server {
	logger := slog.Default()
	return httpapi.NewServer(b, testAPIKey, logger)
}

func authHeader() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+testAPIKey)
	return h
}

func doRequest(srv http.Handler, method, path string, body any, headers http.Header) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, vals := range headers {
		for _, v := range vals {
			req.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// --- auth tests --------------------------------------------------------------

func TestAuthRequired(t *testing.T) {
	srv := newTestServer(newMockBackend())

	w := doRequest(srv, http.MethodGet, "/v1/queues", nil, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthInvalidKey(t *testing.T) {
	srv := newTestServer(newMockBackend())

	h := http.Header{}
	h.Set("Authorization", "Bearer wrong-key")
	w := doRequest(srv, http.MethodGet, "/v1/queues", nil, h)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthValidKey(t *testing.T) {
	srv := newTestServer(newMockBackend())

	w := doRequest(srv, http.MethodGet, "/v1/queues", nil, authHeader())
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- enqueue tests -----------------------------------------------------------

func TestEnqueue(t *testing.T) {
	mb := newMockBackend()
	srv := newTestServer(mb)

	body := map[string]any{
		"type":    "send_email",
		"queue":   "emails",
		"payload": map[string]string{"to": "test@example.com"},
	}

	w := doRequest(srv, http.MethodPost, "/v1/jobs", body, authHeader())
	require.Equal(t, http.StatusCreated, w.Code)

	var job queue.Job
	require.NoError(t, json.NewDecoder(w.Body).Decode(&job))
	assert.Equal(t, "send_email", job.Type)
	assert.Equal(t, "emails", job.Queue)
	assert.Equal(t, queue.StatusPending, job.Status)
	assert.Equal(t, queue.PriorityNormal, job.Priority)
	assert.Equal(t, 3, job.MaxAttempts)
}

func TestEnqueueWithOptionalFields(t *testing.T) {
	mb := newMockBackend()
	srv := newTestServer(mb)

	body := map[string]any{
		"type":         "send_email",
		"queue":        "emails",
		"payload":      map[string]string{"to": "test@example.com"},
		"priority":     20,
		"max_attempts": 5,
	}

	w := doRequest(srv, http.MethodPost, "/v1/jobs", body, authHeader())
	require.Equal(t, http.StatusCreated, w.Code)

	var job queue.Job
	require.NoError(t, json.NewDecoder(w.Body).Decode(&job))
	assert.Equal(t, 20, job.Priority)
	assert.Equal(t, 5, job.MaxAttempts)
}

func TestEnqueueValidationError(t *testing.T) {
	srv := newTestServer(newMockBackend())

	body := map[string]any{
		"type":  "",
		"queue": "emails",
	}

	w := doRequest(srv, http.MethodPost, "/v1/jobs", body, authHeader())
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestEnqueueBadJSON(t *testing.T) {
	srv := newTestServer(newMockBackend())

	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewBufferString("{bad"))
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- get job tests -----------------------------------------------------------

func TestGetJob(t *testing.T) {
	mb := newMockBackend()
	srv := newTestServer(mb)

	job := queue.NewJob("test", "default", json.RawMessage(`{"key":"val"}`))
	mb.Enqueue(context.Background(), job)

	w := doRequest(srv, http.MethodGet, "/v1/jobs/"+job.ID, nil, authHeader())
	require.Equal(t, http.StatusOK, w.Code)

	var got queue.Job
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, job.ID, got.ID)
}

func TestGetJobNotFound(t *testing.T) {
	srv := newTestServer(newMockBackend())

	w := doRequest(srv, http.MethodGet, "/v1/jobs/nonexistent", nil, authHeader())
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- retry tests -------------------------------------------------------------

func TestRetryJob(t *testing.T) {
	mb := newMockBackend()
	srv := newTestServer(mb)

	job := queue.NewJob("test", "default", json.RawMessage(`{"key":"val"}`))
	job.Status = queue.StatusDead
	mb.Enqueue(context.Background(), job)

	w := doRequest(srv, http.MethodPost, "/v1/jobs/"+job.ID+"/retry", nil, authHeader())
	require.Equal(t, http.StatusOK, w.Code)

	var retried queue.Job
	require.NoError(t, json.NewDecoder(w.Body).Decode(&retried))
	assert.Equal(t, queue.StatusPending, retried.Status)
	assert.NotEqual(t, job.ID, retried.ID)
	assert.Equal(t, "test", retried.Type)
}

func TestRetryJobNotFound(t *testing.T) {
	srv := newTestServer(newMockBackend())

	w := doRequest(srv, http.MethodPost, "/v1/jobs/nonexistent/retry", nil, authHeader())
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- cancel tests ------------------------------------------------------------

func TestCancelPendingJob(t *testing.T) {
	mb := newMockBackend()
	srv := newTestServer(mb)

	job := queue.NewJob("test", "default", json.RawMessage(`{"key":"val"}`))
	mb.Enqueue(context.Background(), job)

	w := doRequest(srv, http.MethodPost, "/v1/jobs/"+job.ID+"/cancel", nil, authHeader())
	assert.Equal(t, http.StatusOK, w.Code)

	_, err := mb.GetJob(context.Background(), job.ID)
	assert.Error(t, err)
}

func TestCancelRunningJobConflict(t *testing.T) {
	mb := newMockBackend()
	srv := newTestServer(mb)

	job := queue.NewJob("test", "default", json.RawMessage(`{"key":"val"}`))
	job.Status = queue.StatusRunning
	mb.Enqueue(context.Background(), job)

	w := doRequest(srv, http.MethodPost, "/v1/jobs/"+job.ID+"/cancel", nil, authHeader())
	assert.Equal(t, http.StatusConflict, w.Code)
}

// --- delete tests ------------------------------------------------------------

func TestDeleteJob(t *testing.T) {
	mb := newMockBackend()
	srv := newTestServer(mb)

	job := queue.NewJob("test", "default", json.RawMessage(`{"key":"val"}`))
	mb.Enqueue(context.Background(), job)

	w := doRequest(srv, http.MethodDelete, "/v1/jobs/"+job.ID, nil, authHeader())
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteJobNotFound(t *testing.T) {
	srv := newTestServer(newMockBackend())

	w := doRequest(srv, http.MethodDelete, "/v1/jobs/nonexistent", nil, authHeader())
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- list queues tests -------------------------------------------------------

func TestListQueues(t *testing.T) {
	mb := newMockBackend()
	srv := newTestServer(mb)

	mb.Enqueue(context.Background(), queue.NewJob("a", "q1", json.RawMessage(`{}`)))
	mb.Enqueue(context.Background(), queue.NewJob("b", "q2", json.RawMessage(`{}`)))

	w := doRequest(srv, http.MethodGet, "/v1/queues", nil, authHeader())
	require.Equal(t, http.StatusOK, w.Code)

	var queues []queue.Queue
	require.NoError(t, json.NewDecoder(w.Body).Decode(&queues))
	assert.Len(t, queues, 2)
}

// --- list jobs tests ---------------------------------------------------------

func TestListJobs(t *testing.T) {
	mb := newMockBackend()
	srv := newTestServer(mb)

	mb.Enqueue(context.Background(), queue.NewJob("a", "emails", json.RawMessage(`{}`)))
	mb.Enqueue(context.Background(), queue.NewJob("b", "emails", json.RawMessage(`{}`)))
	mb.Enqueue(context.Background(), queue.NewJob("c", "other", json.RawMessage(`{}`)))

	w := doRequest(srv, http.MethodGet, "/v1/queues/emails/jobs", nil, authHeader())
	require.Equal(t, http.StatusOK, w.Code)

	var jobs []*queue.Job
	require.NoError(t, json.NewDecoder(w.Body).Decode(&jobs))
	assert.Len(t, jobs, 2)
}

func TestListJobsWithStatusFilter(t *testing.T) {
	mb := newMockBackend()
	srv := newTestServer(mb)

	j1 := queue.NewJob("a", "emails", json.RawMessage(`{}`))
	j2 := queue.NewJob("b", "emails", json.RawMessage(`{}`))
	j2.Status = queue.StatusCompleted
	mb.Enqueue(context.Background(), j1)
	mb.Enqueue(context.Background(), j2)

	w := doRequest(srv, http.MethodGet, "/v1/queues/emails/jobs?status=pending", nil, authHeader())
	require.Equal(t, http.StatusOK, w.Code)

	var jobs []*queue.Job
	require.NoError(t, json.NewDecoder(w.Body).Decode(&jobs))
	assert.Len(t, jobs, 1)
	assert.Equal(t, queue.StatusPending, jobs[0].Status)
}

func TestListJobsEmptyReturnsEmptyArray(t *testing.T) {
	srv := newTestServer(newMockBackend())

	w := doRequest(srv, http.MethodGet, "/v1/queues/nothing/jobs", nil, authHeader())
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "[]\n", w.Body.String())
}
