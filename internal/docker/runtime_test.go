package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/saltyorg/sdc/internal/lifecycle"
	"github.com/saltyorg/sdc/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ lifecycle.Runtime = (*Client)(nil)

func testDockerLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(false)
	require.NoError(t, err)
	return log
}

func TestRuntimeMapsDockerContainerState(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, _ = w.Write([]byte(`[{"Id":"app-id","Names":["/application"],"State":"running","Labels":{"com.github.saltbox.saltbox_managed":"true"}}]`))
		case strings.HasSuffix(r.URL.Path, "/containers/app-id/json"):
			_, _ = w.Write([]byte(`{"Id":"app-id","Name":"/application","Config":{"Labels":{"com.github.saltbox.saltbox_managed":"true"},"StopTimeout":17,"Healthcheck":{"Test":["NONE"]}},"State":{"Running":true,"Health":{"Status":"unhealthy"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	mobyClient, err := client.New(client.WithHost(daemon.URL))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mobyClient.Close()) })
	adapter := &Client{cli: mobyClient, logger: testDockerLogger(t)}

	containers, err := adapter.ListContainers(t.Context())
	require.NoError(t, err)
	require.Len(t, containers, 1)
	assert.Equal(t, "application", containers[0].Name)
	require.NotNil(t, containers[0].StopTimeout)
	assert.Equal(t, 17*time.Second, *containers[0].StopTimeout)

	state, err := adapter.InspectContainer(t.Context(), "app-id")
	require.NoError(t, err)
	assert.True(t, state.Running)
	assert.Equal(t, lifecycle.HealthcheckDisabled, state.Healthcheck)
	assert.Equal(t, lifecycle.HealthUnhealthy, state.Health)
}

func TestRuntimeTreatsEmptyHealthcheckCommandAsAbsent(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"app-id","Config":{"Healthcheck":{"Test":[]}},"State":{"Running":true}}`))
	}))
	t.Cleanup(daemon.Close)
	mobyClient, err := client.New(client.WithHost(daemon.URL))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mobyClient.Close()) })
	adapter := &Client{cli: mobyClient, logger: testDockerLogger(t)}

	state, err := adapter.InspectContainer(t.Context(), "app-id")

	require.NoError(t, err)
	assert.Equal(t, lifecycle.HealthcheckNone, state.Healthcheck)
}

func TestRuntimeStartAndStopUseContainerID(t *testing.T) {
	requests := make(chan string, 10)
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(daemon.Close)

	mobyClient, err := client.New(client.WithHost(daemon.URL))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mobyClient.Close()) })
	adapter := &Client{cli: mobyClient, logger: testDockerLogger(t)}

	require.NoError(t, adapter.StartContainer(context.Background(), "app-id"))
	require.NoError(t, adapter.StopContainer(context.Background(), "app-id", 23*time.Second))
	close(requests)
	var captured []string
	for request := range requests {
		captured = append(captured, request)
	}
	requestLog := strings.Join(captured, "\n")
	assert.Contains(t, requestLog, "/containers/app-id/start")
	assert.Contains(t, requestLog, "/containers/app-id/stop")
}
