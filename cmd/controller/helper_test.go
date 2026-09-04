package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/saltyorg/sdc/internal/client"
	"github.com/saltyorg/sdc/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHelperClient struct {
	startDone chan struct{}
	stopDone  chan struct{}
	stopOnce  sync.Once
}

func (f *fakeHelperClient) WaitForServerReady(context.Context, time.Duration) error { return nil }

func (f *fakeHelperClient) StartContainers(context.Context, int, []string) (*client.JobResponse, error) {
	return &client.JobResponse{ID: "start-job", Status: "pending"}, nil
}

func (f *fakeHelperClient) StopContainersForShutdown(context.Context, int, []string) (*client.JobResponse, error) {
	f.stopOnce.Do(func() { close(f.stopDone) })
	return &client.JobResponse{ID: "stop-job", Status: "pending"}, nil
}

func (f *fakeHelperClient) WaitForJob(_ context.Context, id string, _ time.Duration) (*client.Job, error) {
	if id == "start-job" {
		close(f.startDone)
		return &client.Job{ID: id, Status: "completed", Started: []string{"application"}}, nil
	}
	return &client.Job{ID: id, Status: "completed", Stopped: []string{"application"}}, nil
}

func TestHelperSignalSubmitsShutdownStopFromFreshContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	fake := &fakeHelperClient{startDone: make(chan struct{}), stopDone: make(chan struct{})}
	finished := make(chan error, 1)
	go func() {
		finished <- executeHelper(ctx, config.HelperConfig{
			StartupDelay: 0,
			Timeout:      1,
			PollInterval: time.Millisecond,
		}, fake, testCommandLogger(t))
	}()

	<-fake.startDone
	cancel()

	select {
	case <-fake.stopDone:
	case <-time.After(time.Second):
		t.Fatal("helper did not submit the shutdown stop")
	}
	require.NoError(t, <-finished)
}

func TestOperationBlockedUsesTypedHTTPStatus(t *testing.T) {
	assert.True(t, isOperationBlocked(&client.HTTPError{StatusCode: 503}))
	assert.False(t, isOperationBlocked(&client.HTTPError{StatusCode: 500}))
}
