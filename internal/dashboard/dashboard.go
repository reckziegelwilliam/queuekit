// Package dashboard provides server-rendered HTML views for queue management.

package dashboard

import (
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/reckziegelwilliam/queuekit/internal/backend"
	"github.com/reckziegelwilliam/queuekit/internal/queue"
)

//go:embed templates/*.html
var templateFS embed.FS

type Dashboard struct {
	backend backend.Backend
	logger  *slog.Logger
	tmpl    *template.Template
	router  chi.Router
}

func New(b backend.Backend, logger *slog.Logger) *Dashboard {
	d := &Dashboard{
		backend: b,
		logger:  logger,
		tmpl:    template.Must(template.ParseFS(templateFS, "templates/*.html")),
		router:  chi.NewRouter(),
	}
	d.routes()
	return d
}

func (d *Dashboard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.router.ServeHTTP(w, r)
}

func (d *Dashboard) routes() {
	d.router.Get("/", d.handleQueues)
	d.router.Get("/queues/{name}", d.handleQueueDetail)
	d.router.Get("/jobs/{id}", d.handleJobDetail)
	d.router.Post("/jobs/{id}/retry", d.handleRetryJob)
}

// --- page data types ---------------------------------------------------------

type queuesPage struct {
	Title  string
	Queues []queue.Queue
}

type queueDetailPage struct {
	Title      string
	QueueName  string
	Status     string
	Jobs       []*queue.Job
	Limit      int
	Offset     int
	PrevOffset int
	NextOffset int
	HasNext    bool
}

type jobDetailPage struct {
	Title         string
	Job           *queue.Job
	PayloadPretty string
}

// --- handlers ----------------------------------------------------------------

func (d *Dashboard) handleQueues(w http.ResponseWriter, r *http.Request) {
	queues, err := d.backend.ListQueues(r.Context())
	if err != nil {
		d.renderError(w, "Failed to list queues", err)
		return
	}

	d.render(w, "queues.html", queuesPage{
		Title:  "Queues",
		Queues: queues,
	})
}

func (d *Dashboard) handleQueueDetail(w http.ResponseWriter, r *http.Request) {
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

	jobs, err := d.backend.ListJobs(r.Context(), name, status, limit+1, offset)
	if err != nil {
		d.renderError(w, "Failed to list jobs", err)
		return
	}

	hasNext := len(jobs) > limit
	if hasNext {
		jobs = jobs[:limit]
	}

	prevOffset := offset - limit
	if prevOffset < 0 {
		prevOffset = 0
	}

	d.render(w, "queue_detail.html", queueDetailPage{
		Title:      "Queue: " + name,
		QueueName:  name,
		Status:     status,
		Jobs:       jobs,
		Limit:      limit,
		Offset:     offset,
		PrevOffset: prevOffset,
		NextOffset: offset + limit,
		HasNext:    hasNext,
	})
}

func (d *Dashboard) handleJobDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	job, err := d.backend.GetJob(r.Context(), id)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, job.Payload, "", "  "); err != nil {
		pretty.Write(job.Payload)
	}

	d.render(w, "job_detail.html", jobDetailPage{
		Title:         "Job: " + id[:8],
		Job:           job,
		PayloadPretty: pretty.String(),
	})
}

func (d *Dashboard) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	job, err := d.backend.GetJob(r.Context(), id)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	if err := d.backend.DeleteJob(r.Context(), id); err != nil {
		d.renderError(w, "Failed to delete old job for retry", err)
		return
	}

	retried := queue.NewJob(job.Type, job.Queue, job.Payload)
	retried.Priority = job.Priority
	retried.MaxAttempts = job.MaxAttempts

	if err := d.backend.Enqueue(r.Context(), retried); err != nil {
		d.renderError(w, "Failed to enqueue retried job", err)
		return
	}

	http.Redirect(w, r, "/dashboard/jobs/"+retried.ID, http.StatusSeeOther)
}

// --- render helpers ----------------------------------------------------------

func (d *Dashboard) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := d.tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		d.logger.Error("template render failed", "template", name, "error", err)
	}
}

func (d *Dashboard) renderError(w http.ResponseWriter, msg string, err error) {
	d.logger.Error(msg, "error", err)
	http.Error(w, msg, http.StatusInternalServerError)
}
