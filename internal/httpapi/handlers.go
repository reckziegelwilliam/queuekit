package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

// --- request / response types ------------------------------------------------

type enqueueRequest struct {
	Type        string          `json:"type"`
	Queue       string          `json:"queue"`
	Payload     json.RawMessage `json:"payload"`
	Priority    *int            `json:"priority,omitempty"`
	MaxAttempts *int            `json:"max_attempts,omitempty"`
	ScheduledAt *time.Time      `json:"scheduled_at,omitempty"`
}

// --- helpers -----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- handlers ----------------------------------------------------------------

func (s *Server) handleEnqueue(w http.ResponseWriter, r *http.Request) {
	var req enqueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	job := queue.NewJob(req.Type, req.Queue, req.Payload)

	if req.Priority != nil {
		job.Priority = *req.Priority
	}
	if req.MaxAttempts != nil {
		job.MaxAttempts = *req.MaxAttempts
	}
	if req.ScheduledAt != nil {
		job.ScheduledAt = *req.ScheduledAt
	}

	if err := job.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if err := s.backend.Enqueue(r.Context(), job); err != nil {
		s.logger.Error("enqueue failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to enqueue job")
		return
	}

	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	job, err := s.backend.GetJob(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		s.logger.Error("get job failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get job")
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	job, err := s.backend.GetJob(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		s.logger.Error("get job for retry failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get job")
		return
	}

	if err := s.backend.DeleteJob(r.Context(), job.ID); err != nil {
		s.logger.Error("delete old job for retry failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to retry job")
		return
	}

	retried := queue.NewJob(job.Type, job.Queue, job.Payload)
	retried.Priority = job.Priority
	retried.MaxAttempts = job.MaxAttempts

	if err := s.backend.Enqueue(r.Context(), retried); err != nil {
		s.logger.Error("enqueue retry failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to retry job")
		return
	}

	writeJSON(w, http.StatusOK, retried)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	job, err := s.backend.GetJob(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		s.logger.Error("get job for cancel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get job")
		return
	}

	if job.Status != queue.StatusPending {
		writeError(w, http.StatusConflict, "only pending jobs can be cancelled")
		return
	}

	if err := s.backend.DeleteJob(r.Context(), id); err != nil {
		s.logger.Error("cancel job failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to cancel job")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := s.backend.DeleteJob(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		s.logger.Error("delete job failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete job")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListQueues(w http.ResponseWriter, r *http.Request) {
	queues, err := s.backend.ListQueues(r.Context())
	if err != nil {
		s.logger.Error("list queues failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list queues")
		return
	}

	writeJSON(w, http.StatusOK, queues)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	status := r.URL.Query().Get("status")

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	jobs, err := s.backend.ListJobs(r.Context(), name, status, limit, offset)
	if err != nil {
		s.logger.Error("list jobs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	if jobs == nil {
		jobs = []*queue.Job{}
	}

	writeJSON(w, http.StatusOK, jobs)
}
