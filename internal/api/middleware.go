package api

import (
	"net/http"
	"time"
)

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		status:         http.StatusOK, // Default to 200 if WriteHeader is never called
	}
}

func (rw *responseWriter) Status() int {
	return rw.status
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}

	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
	rw.wroteHeader = true
}

// LoggingMiddleware logs HTTP requests with structured logging
func (s *Server) LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := wrapResponseWriter(w)

		// Call the next handler
		next.ServeHTTP(wrapped, r)

		// Log the request
		duration := time.Since(start)

		s.logger.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"status", wrapped.status,
			"duration", duration,
			"user_agent", r.UserAgent())
	})
}

// RecoveryMiddleware recovers from panics and logs them
func (s *Server) RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				s.logger.Error("Panic recovered",
					"error", err,
					"method", r.Method,
					"path", r.URL.Path)

				s.writeError(w, http.StatusInternalServerError, "Internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}
