// Package httpapi provides the REST API server and handlers.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/reckziegelwilliam/queuekit/internal/backend"
)

type Server struct {
	router  chi.Router
	backend backend.Backend
	logger  *slog.Logger
	apiKey  string
}

func NewServer(b backend.Backend, apiKey string, logger *slog.Logger) *Server {
	s := &Server{
		router:  chi.NewRouter(),
		backend: b,
		logger:  logger,
		apiKey:  apiKey,
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)

	s.router.Route("/v1", func(r chi.Router) {
		r.Use(s.apiKeyAuth)

		r.Post("/jobs", s.handleEnqueue)
		r.Get("/jobs/{id}", s.handleGetJob)
		r.Post("/jobs/{id}/retry", s.handleRetryJob)
		r.Post("/jobs/{id}/cancel", s.handleCancelJob)
		r.Delete("/jobs/{id}", s.handleDeleteJob)

		r.Get("/queues", s.handleListQueues)
		r.Get("/queues/{name}/jobs", s.handleListJobs)
	})
}

// MountDashboard attaches dashboard routes under /dashboard.
func (s *Server) MountDashboard(h http.Handler) {
	s.router.Mount("/dashboard", h)
}
