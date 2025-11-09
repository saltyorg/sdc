package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/saltyorg/sdc/internal/jobs"
	"github.com/saltyorg/sdc/pkg/logger"
)

// Server represents the API server
type Server struct {
	jobManager *jobs.Manager
	logger     *logger.Logger
}

// NewServer creates a new API server
func NewServer(jobManager *jobs.Manager, logger *logger.Logger) *Server {
	return &Server{
		jobManager: jobManager,
		logger:     logger,
	}
}

// Router creates and configures the HTTP router
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// Middleware stack
	r.Use(s.RecoveryMiddleware)
	r.Use(s.LoggingMiddleware)

	// Main API routes (spec-compliant)
	r.Post("/start", s.HandleStartContainers)
	r.Post("/stop", s.HandleStopContainers)
	r.Get("/health", s.HandleHealth)

	// Job management routes
	r.Get("/jobs", s.HandleListJobs)
	r.Get("/jobs/{id}", s.HandleGetJob)
	r.Delete("/jobs/{id}", s.HandleDeleteJob)

	return r
}

// JobResponse represents a job response
type JobResponse struct {
	JobID string `json:"job_id"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// HandleStartContainers handles POST /start
func (s *Server) HandleStartContainers(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	timeout := 600 // 10 minutes default
	if timeoutStr := r.URL.Query().Get("timeout"); timeoutStr != "" {
		if parsedTimeout, err := strconv.Atoi(timeoutStr); err == nil {
			timeout = parsedTimeout
		}
	}

	// Create and submit job
	job := jobs.NewJob(jobs.JobTypeStart, timeout, nil)
	if err := s.jobManager.Submit(job); err != nil {
		s.logger.Error("Failed to submit job", "error", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to submit job")
		return
	}

	s.logger.Info("Start job created",
		"job_id", job.ID,
		"timeout", timeout)

	s.writeJSON(w, http.StatusOK, JobResponse{
		JobID: job.ID,
	})
}

// HandleStopContainers handles POST /stop
func (s *Server) HandleStopContainers(w http.ResponseWriter, r *http.Request) {
	// Parse timeout query parameter
	timeout := 300 // 5 minutes default
	if timeoutStr := r.URL.Query().Get("timeout"); timeoutStr != "" {
		if parsedTimeout, err := strconv.Atoi(timeoutStr); err == nil {
			timeout = parsedTimeout
		}
	}

	// Parse ignore query parameter (supports both comma-separated and repeated params)
	var ignore []string
	query := r.URL.Query()

	// Handle repeated params: ?ignore=traefik&ignore=nginx
	if ignoreParams := query["ignore"]; len(ignoreParams) > 0 {
		for _, param := range ignoreParams {
			// Also support comma-separated within each param: ?ignore=traefik,nginx
			parts := strings.Split(param, ",")
			for _, part := range parts {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					ignore = append(ignore, trimmed)
				}
			}
		}
	}

	// Create and submit job
	job := jobs.NewJob(jobs.JobTypeStop, timeout, ignore)
	if err := s.jobManager.Submit(job); err != nil {
		s.logger.Error("Failed to submit job", "error", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to submit job")
		return
	}

	s.logger.Info("Stop job created",
		"job_id", job.ID,
		"timeout", timeout,
		"ignore", ignore)

	s.writeJSON(w, http.StatusOK, JobResponse{
		JobID: job.ID,
	})
}

// HandleGetJob handles GET /api/v1/jobs/{id}
func (s *Server) HandleGetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	job, err := s.jobManager.Get(id)
	if err != nil {
		s.logger.Debug("Job not found", "job_id", id)
		s.writeError(w, http.StatusNotFound, "Job not found")
		return
	}

	s.writeJSON(w, http.StatusOK, job)
}

// HandleListJobs handles GET /api/v1/jobs
func (s *Server) HandleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.jobManager.List()
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs":  jobs,
		"count": len(jobs),
	})
}

// HandleDeleteJob handles DELETE /api/v1/jobs/{id}
func (s *Server) HandleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := s.jobManager.Delete(id); err != nil {
		s.logger.Debug("Job not found for deletion", "job_id", id)
		s.writeError(w, http.StatusNotFound, "Job not found")
		return
	}

	s.logger.Info("Job deleted", "job_id", id)
	w.WriteHeader(http.StatusNoContent)
}

// HandleHealth handles GET /health
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

// writeJSON writes a JSON response
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode JSON response", "error", err)
	}
}

// writeError writes an error JSON response
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, ErrorResponse{Error: message})
}
