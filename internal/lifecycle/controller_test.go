package lifecycle

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func awaitTerminalJob(t *testing.T, controller *Controller, id string) JobSnapshot {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline.C:
			t.Fatalf("job %s did not become terminal", id)
		case <-ticker.C:
			job, err := controller.Job(t.Context(), id)
			require.NoError(t, err)
			if job.Status == StatusCompleted || job.Status == StatusFailed {
				return job
			}
		}
	}
}

func TestControllerRunsNormalJobsFIFO(t *testing.T) {
	startGate := make(chan struct{})
	runtime := newFakeRuntime(testContainer("application", false))
	runtime.startGate = startGate
	runtime.startSeen = make(chan struct{}, 1)
	runtime.stopSeen = make(chan struct{}, 1)
	controller := NewController(runtime, testLogger(t))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, controller.Shutdown(ctx))
	})

	start, err := controller.Submit(t.Context(), Request{Operation: OperationStart, Timeout: time.Minute})
	require.NoError(t, err)
	<-runtime.startSeen
	stop, err := controller.Submit(t.Context(), Request{Operation: OperationStop, Timeout: time.Minute})
	require.NoError(t, err)

	select {
	case <-runtime.stopSeen:
		t.Fatal("stop operation ran before the active start completed")
	default:
	}
	close(startGate)

	assert.Equal(t, StatusCompleted, awaitTerminalJob(t, controller, start.ID).Status)
	assert.Equal(t, StatusCompleted, awaitTerminalJob(t, controller, stop.ID).Status)
	assert.Equal(t, []string{"start:application", "stop:application"}, runtime.eventLog())
}

func TestControllerBlockRejectsNewAdmissionWithoutCancelingAcceptedJob(t *testing.T) {
	startGate := make(chan struct{})
	runtime := newFakeRuntime(testContainer("application", false))
	runtime.startGate = startGate
	runtime.startSeen = make(chan struct{}, 1)
	controller := NewController(runtime, testLogger(t))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, controller.Shutdown(ctx))
	})

	accepted, err := controller.Submit(t.Context(), Request{Operation: OperationStart, Timeout: time.Minute})
	require.NoError(t, err)
	<-runtime.startSeen
	require.NoError(t, controller.Block(t.Context(), time.Minute))

	_, err = controller.Submit(t.Context(), Request{Operation: OperationStop, Timeout: time.Minute})
	assert.ErrorIs(t, err, ErrBlocked)
	close(startGate)
	assert.Equal(t, StatusCompleted, awaitTerminalJob(t, controller, accepted.ID).Status)
}

func TestControllerShutdownStopCancelsStartAndBecomesGate(t *testing.T) {
	startGate := make(chan struct{})
	stopGate := make(chan struct{})
	var releaseStopOnce sync.Once
	releaseStop := func() { releaseStopOnce.Do(func() { close(stopGate) }) }
	runtime := newFakeRuntime(testContainer("application", false))
	runtime.startGate = startGate
	runtime.stopGate = stopGate
	runtime.startSeen = make(chan struct{}, 1)
	runtime.stopSeen = make(chan struct{}, 1)
	controller := NewController(runtime, testLogger(t))
	t.Cleanup(func() {
		releaseStop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, controller.Shutdown(ctx))
	})

	start, err := controller.Submit(t.Context(), Request{Operation: OperationStart, Timeout: time.Minute})
	require.NoError(t, err)
	<-runtime.startSeen
	stop, err := controller.Submit(t.Context(), Request{
		Operation: OperationStop,
		Intent:    IntentShutdown,
		Timeout:   time.Minute,
	})
	require.NoError(t, err)
	<-runtime.stopSeen

	_, err = controller.Submit(t.Context(), Request{Operation: OperationStart, Timeout: time.Minute})
	assert.ErrorIs(t, err, ErrShutdownInProgress)
	assert.Equal(t, StatusFailed, awaitTerminalJob(t, controller, start.ID).Status)
	releaseStop()
	assert.Equal(t, StatusCompleted, awaitTerminalJob(t, controller, stop.ID).Status)
}

func TestControllerReadyReportsRuntimeFailure(t *testing.T) {
	runtime := newFakeRuntime()
	runtime.pingErr = errors.New("docker unavailable")
	controller := NewController(runtime, testLogger(t))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, controller.Shutdown(ctx))
	})

	assert.ErrorContains(t, controller.Ready(t.Context()), "docker unavailable")
}
