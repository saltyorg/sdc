package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/saltyorg/sdc/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopForShutdownSendsAdditiveIntent(t *testing.T) {
	var request JobRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSON(t, r.Body, &request)
		writeJSON(t, w, JobResponse{ID: "job-1", Status: "pending"})
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, clientTestLogger(t))
	_, err := client.StopContainersForShutdown(t.Context(), 600, nil)

	require.NoError(t, err)
	assert.True(t, request.Shutdown)
}

func TestHTTPErrorExposesStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, clientTestLogger(t))
	_, err := client.StartContainers(t.Context(), 600, nil)

	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusServiceUnavailable, httpErr.StatusCode)
}

func TestReadyFallsBackToPingForV1Server(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/ready" {
			http.NotFound(w, r)
			return
		}
		writeJSON(t, w, HealthResponse{Status: "healthy"})
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, clientTestLogger(t))
	require.NoError(t, client.Ready(t.Context()))
	assert.Equal(t, []string{"/ready", "/ping"}, paths)
}

func TestWaitForServerReadyAttemptsImmediatelyAndHonorsDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, clientTestLogger(t))
	started := time.Now()
	err := client.WaitForServerReady(t.Context(), 25*time.Millisecond)

	assert.Error(t, err)
	assert.Less(t, time.Since(started), 250*time.Millisecond)
}

func decodeJSON(t *testing.T, body io.Reader, target any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(body).Decode(target))
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(writer).Encode(value))
}

func clientTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(false)
	require.NoError(t, err)
	return log
}
