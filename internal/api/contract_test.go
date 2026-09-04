package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/saltyorg/sdc/internal/lifecycle"
	"github.com/saltyorg/sdc/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLifecycleController struct {
	submitted []lifecycle.Request
	job       lifecycle.JobSnapshot
	submitErr error
	readyErr  error
	blocked   []time.Duration
	unblocked int
}

func (f *fakeLifecycleController) Submit(_ context.Context, request lifecycle.Request) (lifecycle.JobSnapshot, error) {
	if f.submitErr != nil {
		return lifecycle.JobSnapshot{}, f.submitErr
	}
	f.submitted = append(f.submitted, request)
	return lifecycle.JobSnapshot{ID: "job-1", Status: lifecycle.StatusPending}, nil
}

func (f *fakeLifecycleController) Job(context.Context, string) (lifecycle.JobSnapshot, error) {
	if f.job.ID == "" {
		return lifecycle.JobSnapshot{}, lifecycle.ErrJobNotFound
	}
	return f.job, nil
}

func (f *fakeLifecycleController) Block(_ context.Context, duration time.Duration) error {
	f.blocked = append(f.blocked, duration)
	return nil
}

func (f *fakeLifecycleController) Unblock(context.Context) error {
	f.unblocked++
	return nil
}

func (f *fakeLifecycleController) Ready(context.Context) error { return f.readyErr }

func apiTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(false)
	require.NoError(t, err)
	return log
}

func decodeResponse[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var response T
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	return response
}

func TestStartPreservesV1ResponseAndAddsPendingStatus(t *testing.T) {
	controller := &fakeLifecycleController{}
	router := NewServer(controller, apiTestLogger(t)).Router()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/start?timeout=42", bytes.NewBufferString(`{"ignore":["traefik"]}`))

	router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, JobResponse{JobID: "job-1", Status: "pending"}, decodeResponse[JobResponse](t, recorder))
	require.Len(t, controller.submitted, 1)
	assert.Equal(t, lifecycle.OperationStart, controller.submitted[0].Operation)
	assert.Equal(t, 42*time.Second, controller.submitted[0].Timeout)
	assert.Equal(t, []string{"traefik"}, controller.submitted[0].Ignore)
}

func TestStopAcceptsExplicitShutdownIntent(t *testing.T) {
	controller := &fakeLifecycleController{}
	router := NewServer(controller, apiTestLogger(t)).Router()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/stop", bytes.NewBufferString(`{"shutdown":true}`))

	router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, controller.submitted, 1)
	assert.Equal(t, lifecycle.IntentShutdown, controller.submitted[0].Intent)
	assert.Equal(t, 300*time.Second, controller.submitted[0].Timeout)
}

func TestJobStatusPreservesLegacyFieldsAndAddsOutcomes(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	controller := &fakeLifecycleController{job: lifecycle.JobSnapshot{
		ID: "job-1", Operation: lifecycle.OperationStart, Status: lifecycle.StatusCompleted,
		CreatedAt: now, StartedAt: now.Add(time.Second), EndedAt: now.Add(2 * time.Second),
		Timeout: time.Minute, Started: []string{"application"}, Skipped: []string{"database"},
		Outcomes: []lifecycle.ContainerResult{{Container: "application", Outcome: lifecycle.OutcomeChanged}},
	}}
	router := NewServer(controller, apiTestLogger(t)).Router()
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/job_status/job-1", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	response := decodeResponse[Job](t, recorder)
	assert.Equal(t, "job-1", response.ID)
	assert.Equal(t, "start", response.Type)
	assert.Equal(t, "completed", response.Status)
	assert.Equal(t, int64(60), response.Timeout)
	assert.Equal(t, []string{"application"}, response.Started)
	require.Len(t, response.Outcomes, 1)
	assert.Equal(t, lifecycle.OutcomeChanged, response.Outcomes[0].Outcome)
}

func TestRequestsRejectMalformedOrUnsafeInput(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "unknown field", path: "/start", body: `{"unknown":true}`},
		{name: "trailing document", path: "/start", body: `{} {}`},
		{name: "zero timeout", path: "/start", body: `{"timeout":0}`},
		{name: "negative query timeout", path: "/stop?timeout=-1", body: ``},
		{name: "shutdown start", path: "/start", body: `{"shutdown":true}`},
		{name: "invalid block duration", path: "/block/invalid", body: ``},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := &fakeLifecycleController{}
			router := NewServer(controller, apiTestLogger(t)).Router()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))

			router.ServeHTTP(recorder, request)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Empty(t, controller.submitted)
		})
	}
}

func TestReadySeparatesOperationalReadinessFromPing(t *testing.T) {
	controller := &fakeLifecycleController{readyErr: errors.New("docker unavailable")}
	router := NewServer(controller, apiTestLogger(t)).Router()

	ready := httptest.NewRecorder()
	router.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/ready", nil))
	assert.Equal(t, http.StatusServiceUnavailable, ready.Code)

	ping := httptest.NewRecorder()
	router.ServeHTTP(ping, httptest.NewRequest(http.MethodGet, "/ping", nil))
	assert.Equal(t, http.StatusOK, ping.Code)
	assert.Equal(t, map[string]string{"status": "healthy"}, decodeResponse[map[string]string](t, ping))
}

func TestBlockedSubmissionKeepsV1ServiceUnavailableContract(t *testing.T) {
	controller := &fakeLifecycleController{submitErr: lifecycle.ErrBlocked}
	router := NewServer(controller, apiTestLogger(t)).Router()
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/start", nil))

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Equal(t, ErrorResponse{Error: "Operation blocked"}, decodeResponse[ErrorResponse](t, recorder))
}
