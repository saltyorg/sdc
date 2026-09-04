package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/saltyorg/sdc/internal/lifecycle"
	"github.com/saltyorg/sdc/pkg/logger"
)

const (
	maxRequestBodyBytes = 64 << 10
	maxOperationSeconds = int64(1<<31 - 1)
	maxBlockMinutes     = int64(153722867)
)

type lifecycleController interface {
	Submit(context.Context, lifecycle.Request) (lifecycle.JobSnapshot, error)
	Job(context.Context, string) (lifecycle.JobSnapshot, error)
	Block(context.Context, time.Duration) error
	Unblock(context.Context) error
	Ready(context.Context) error
}

type Server struct {
	controller lifecycleController
	logger     *logger.Logger
}

func NewServer(controller lifecycleController, logger *logger.Logger) *Server {
	return &Server{controller: controller, logger: logger}
}

func (s *Server) Router() http.Handler {
	router := chi.NewRouter()
	router.Use(s.RecoveryMiddleware)
	router.Use(s.LoggingMiddleware)
	router.Post("/start", s.HandleStartContainers)
	router.Post("/stop", s.HandleStopContainers)
	router.Get("/ping", s.HandleHealth)
	router.Get("/ready", s.HandleReady)
	router.Post("/block", s.HandleBlock)
	router.Post("/block/{duration}", s.HandleBlock)
	router.Post("/unblock", s.HandleUnblock)
	router.Get("/job_status/{job_id}", s.HandleGetJobStatus)
	return router
}

type JobResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type Job struct {
	ID        string                      `json:"id"`
	Type      string                      `json:"type"`
	Status    string                      `json:"status"`
	CreatedAt time.Time                   `json:"created_at"`
	StartedAt time.Time                   `json:"started_at"`
	EndedAt   time.Time                   `json:"ended_at"`
	Timeout   int64                       `json:"timeout"`
	Ignore    []string                    `json:"ignore"`
	Started   []string                    `json:"started,omitempty"`
	Stopped   []string                    `json:"stopped,omitempty"`
	Skipped   []string                    `json:"skipped,omitempty"`
	Failed    []string                    `json:"failed,omitempty"`
	Outcomes  []lifecycle.ContainerResult `json:"outcomes,omitempty"`
	Error     string                      `json:"error,omitempty"`
}

type jobRequest struct {
	Timeout  *int64   `json:"timeout"`
	Ignore   []string `json:"ignore"`
	Shutdown bool     `json:"shutdown,omitempty"`
}

func (s *Server) HandleStartContainers(w http.ResponseWriter, r *http.Request) {
	s.handleOperation(w, r, lifecycle.OperationStart, 600)
}

func (s *Server) HandleStopContainers(w http.ResponseWriter, r *http.Request) {
	s.handleOperation(w, r, lifecycle.OperationStop, 300)
}

func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request, operation lifecycle.Operation, defaultSeconds int64) {
	request, err := decodeJobRequest(w, r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if request.Shutdown && operation != lifecycle.OperationStop {
		s.writeError(w, http.StatusBadRequest, "Shutdown intent is only valid for stop operations")
		return
	}

	timeoutSeconds := defaultSeconds
	if request.Timeout != nil {
		timeoutSeconds = *request.Timeout
	}
	if query := r.URL.Query().Get("timeout"); query != "" {
		timeoutSeconds, err = parsePositiveSeconds(query)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "Invalid timeout")
			return
		}
	}
	if timeoutSeconds <= 0 || timeoutSeconds > maxOperationSeconds {
		s.writeError(w, http.StatusBadRequest, "Invalid timeout")
		return
	}

	ignore := append([]string(nil), request.Ignore...)
	for _, value := range r.URL.Query()["ignore"] {
		for item := range strings.SplitSeq(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				ignore = append(ignore, item)
			}
		}
	}
	intent := lifecycle.IntentNormal
	if request.Shutdown {
		intent = lifecycle.IntentShutdown
	}
	job, err := s.controller.Submit(r.Context(), lifecycle.Request{
		Operation: operation,
		Intent:    intent,
		Timeout:   time.Duration(timeoutSeconds) * time.Second,
		Ignore:    ignore,
	})
	if err != nil {
		s.writeSubmitError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, JobResponse{JobID: job.ID, Status: string(job.Status)})
}

func decodeJobRequest(w http.ResponseWriter, r *http.Request) (jobRequest, error) {
	if r.Body == nil {
		return jobRequest{}, nil
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()
	var request jobRequest
	if err := decoder.Decode(&request); err != nil {
		if errors.Is(err, io.EOF) {
			return jobRequest{}, nil
		}
		return jobRequest{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return jobRequest{}, errors.New("request body must contain one JSON document")
	}
	return request, nil
}

func parsePositiveSeconds(value string) (int64, error) {
	seconds, err := strconv.ParseInt(value, 10, 32)
	if err != nil || seconds <= 0 {
		return 0, errors.New("timeout must be a positive 32-bit integer")
	}
	return seconds, nil
}

func (s *Server) writeSubmitError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, lifecycle.ErrBlocked):
		s.writeError(w, http.StatusServiceUnavailable, "Operation blocked")
	case errors.Is(err, lifecycle.ErrQueueFull):
		s.writeError(w, http.StatusServiceUnavailable, "Job queue is full")
	case errors.Is(err, lifecycle.ErrClosing), errors.Is(err, lifecycle.ErrShutdownInProgress):
		s.writeError(w, http.StatusServiceUnavailable, "Controller is shutting down")
	case errors.Is(err, lifecycle.ErrInvalidRequest):
		s.writeError(w, http.StatusBadRequest, "Invalid request")
	default:
		s.logger.Error("Failed to submit job", "error", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to submit job")
	}
}

func (s *Server) HandleGetJobStatus(w http.ResponseWriter, r *http.Request) {
	job, err := s.controller.Job(r.Context(), chi.URLParam(r, "job_id"))
	if err != nil {
		if !errors.Is(err, lifecycle.ErrJobNotFound) {
			s.logger.Error("Failed to get job", "error", err)
		}
		s.writeJSON(w, http.StatusNotFound, map[string]string{"status": "not_found"})
		return
	}
	s.writeJSON(w, http.StatusOK, jobResponse(job))
}

func jobResponse(job lifecycle.JobSnapshot) Job {
	return Job{
		ID: job.ID, Type: string(job.Operation), Status: string(job.Status),
		CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, EndedAt: job.EndedAt,
		Timeout: int64(job.Timeout / time.Second), Ignore: append([]string(nil), job.Ignore...),
		Started: append([]string(nil), job.Started...), Stopped: append([]string(nil), job.Stopped...),
		Skipped: append([]string(nil), job.Skipped...), Failed: append([]string(nil), job.Failed...),
		Outcomes: append([]lifecycle.ContainerResult(nil), job.Outcomes...), Error: job.Error,
	}
}

func (s *Server) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Server) HandleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.controller.Ready(ctx); err != nil {
		reason := "docker_unavailable"
		if errors.Is(err, lifecycle.ErrClosing) {
			reason = "shutting_down"
		}
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": reason})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) HandleBlock(w http.ResponseWriter, r *http.Request) {
	durationMinutes := int64(10)
	if value := chi.URLParam(r, "duration"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 || parsed > maxBlockMinutes {
			s.writeError(w, http.StatusBadRequest, "Invalid block duration")
			return
		}
		durationMinutes = parsed
	}
	if err := s.controller.Block(r.Context(), time.Duration(durationMinutes)*time.Minute); err != nil {
		s.writeError(w, http.StatusServiceUnavailable, "Controller is shutting down")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Operations are now blocked for %d minutes", durationMinutes)})
}

func (s *Server) HandleUnblock(w http.ResponseWriter, r *http.Request) {
	if err := s.controller.Unblock(r.Context()); err != nil {
		s.writeError(w, http.StatusServiceUnavailable, "Controller is shutting down")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Operations are now unblocked"})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode JSON response", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, ErrorResponse{Error: message})
}
