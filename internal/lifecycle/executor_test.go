package lifecycle

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/saltyorg/sdc/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	managedLabel    = "com.github.saltbox.saltbox_managed"
	dependencyLabel = "com.github.saltbox.depends_on"
	healthWaitLabel = "com.github.saltbox.depends_on.healthchecks"
)

type fakeRuntime struct {
	mu sync.Mutex

	containers []Container
	listErrors []error
	states     map[string]ContainerState
	inspectErr map[string]error
	startErr   map[string]error
	stopErr    map[string]error
	events     []string
	startGate  <-chan struct{}
	stopGate   <-chan struct{}
	startSeen  chan struct{}
	stopSeen   chan struct{}
	pingErr    error
}

func newFakeRuntime(containers ...Container) *fakeRuntime {
	states := make(map[string]ContainerState, len(containers))
	for _, container := range containers {
		states[container.ID] = ContainerState{Running: container.Running}
	}
	return &fakeRuntime{
		containers: containers,
		states:     states,
		inspectErr: make(map[string]error),
		startErr:   make(map[string]error),
		stopErr:    make(map[string]error),
	}
}

func (f *fakeRuntime) ListContainers(context.Context) ([]Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.listErrors) > 0 {
		err := f.listErrors[0]
		f.listErrors = f.listErrors[1:]
		return nil, err
	}
	return append([]Container(nil), f.containers...), nil
}

func (f *fakeRuntime) InspectContainer(_ context.Context, id string) (ContainerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.states[id], f.inspectErr[id]
}

func (f *fakeRuntime) StartContainer(ctx context.Context, id string) error {
	f.mu.Lock()
	f.events = append(f.events, "start:"+id)
	seen, gate, err := f.startSeen, f.startGate, f.startErr[id]
	f.mu.Unlock()

	if seen != nil {
		select {
		case seen <- struct{}{}:
		default:
		}
	}
	if err == nil {
		f.mu.Lock()
		state := f.states[id]
		state.Running = true
		f.states[id] = state
		f.mu.Unlock()
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}
	return nil
}

func (f *fakeRuntime) StopContainer(ctx context.Context, id string, _ time.Duration) error {
	f.mu.Lock()
	f.events = append(f.events, "stop:"+id)
	seen, gate, err := f.stopSeen, f.stopGate, f.stopErr[id]
	f.mu.Unlock()

	if seen != nil {
		select {
		case seen <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}

	f.mu.Lock()
	state := f.states[id]
	state.Running = false
	f.states[id] = state
	f.mu.Unlock()
	return nil
}

func (f *fakeRuntime) Ping(context.Context) error { return f.pingErr }

func (f *fakeRuntime) eventLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func testContainer(name string, running bool, dependencies ...string) Container {
	labels := map[string]string{managedLabel: "true"}
	if len(dependencies) > 0 {
		labels[dependencyLabel] = dependencies[0]
	}
	return Container{ID: name, Name: name, Labels: labels, Running: running}
}

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(false)
	require.NoError(t, err)
	return log
}

func resultsByContainer(results []ContainerResult) map[string]ContainerResult {
	indexed := make(map[string]ContainerResult, len(results))
	for _, result := range results {
		indexed[result.Container] = result
	}
	return indexed
}

func TestExecutorBlocksDescendantWhenParentStartFails(t *testing.T) {
	runtime := newFakeRuntime(
		testContainer("database", false),
		testContainer("application", false, "database"),
	)
	runtime.startErr["database"] = errors.New("start failed")

	result := newExecutor(runtime, testLogger(t), 2).execute(t.Context(), Request{
		Operation: OperationStart,
		Timeout:   time.Minute,
	})

	indexed := resultsByContainer(result.Outcomes)
	assert.Equal(t, OutcomeFailed, indexed["database"].Outcome)
	assert.Equal(t, ReasonOperationFailed, indexed["database"].Reason)
	assert.Equal(t, OutcomeBlocked, indexed["application"].Outcome)
	assert.Equal(t, ReasonDependencyFailed, indexed["application"].Reason)
	assert.Equal(t, []string{"start:database"}, runtime.eventLog())
	assert.Error(t, result.Err)
}

func TestExecutorProtectsDependencyOfIgnoredRunningContainerOnStop(t *testing.T) {
	runtime := newFakeRuntime(
		testContainer("database", true),
		testContainer("application", true, "database"),
	)

	result := newExecutor(runtime, testLogger(t), 2).execute(t.Context(), Request{
		Operation: OperationStop,
		Timeout:   time.Minute,
		Ignore:    []string{"application"},
	})

	indexed := resultsByContainer(result.Outcomes)
	assert.Equal(t, OutcomeIgnored, indexed["application"].Outcome)
	assert.Equal(t, ReasonRequestedIgnore, indexed["application"].Reason)
	assert.Equal(t, OutcomeProtected, indexed["database"].Outcome)
	assert.Equal(t, ReasonRequiredByIgnored, indexed["database"].Reason)
	assert.Empty(t, runtime.eventLog())
	assert.NoError(t, result.Err)
}

func TestExecutorProtectsDescendantOfIgnoredStoppedDependencyOnStart(t *testing.T) {
	runtime := newFakeRuntime(
		testContainer("database", false),
		testContainer("application", false, "database"),
	)

	result := newExecutor(runtime, testLogger(t), 2).execute(t.Context(), Request{
		Operation: OperationStart,
		Timeout:   time.Minute,
		Ignore:    []string{"database"},
	})

	indexed := resultsByContainer(result.Outcomes)
	assert.Equal(t, OutcomeIgnored, indexed["database"].Outcome)
	assert.Equal(t, OutcomeProtected, indexed["application"].Outcome)
	assert.Equal(t, ReasonDependencyIgnored, indexed["application"].Reason)
	assert.Empty(t, runtime.eventLog())
	assert.NoError(t, result.Err)
}

func TestExecutorTreatsDisabledHealthcheckAsNoHealthcheck(t *testing.T) {
	database := testContainer("database", true)
	application := testContainer("application", false, "database")
	application.Labels[healthWaitLabel] = "true"
	runtime := newFakeRuntime(database, application)
	runtime.states["database"] = ContainerState{
		Running:     true,
		Healthcheck: HealthcheckDisabled,
		Health:      HealthNone,
	}

	result := newExecutor(runtime, testLogger(t), 2).execute(t.Context(), Request{
		Operation: OperationStart,
		Timeout:   time.Minute,
	})

	indexed := resultsByContainer(result.Outcomes)
	assert.Equal(t, OutcomeUnchanged, indexed["database"].Outcome)
	assert.Equal(t, OutcomeChanged, indexed["application"].Outcome)
	assert.Equal(t, []string{"start:application"}, runtime.eventLog())
	assert.NoError(t, result.Err)
}

func TestExecutorFailsJobWhenDependencyIsMissing(t *testing.T) {
	runtime := newFakeRuntime(testContainer("application", false, "database"))

	result := newExecutor(runtime, testLogger(t), 1).execute(t.Context(), Request{
		Operation: OperationStart,
		Timeout:   time.Minute,
	})

	require.Error(t, result.Err)
	indexed := resultsByContainer(result.Outcomes)
	assert.Equal(t, OutcomeBlocked, indexed["application"].Outcome)
	assert.Equal(t, ReasonMissingDependency, indexed["application"].Reason)
	assert.Equal(t, "database", indexed["application"].Error)
}

func TestExecutorProtectsParentWhenChildStopFails(t *testing.T) {
	runtime := newFakeRuntime(
		testContainer("database", true),
		testContainer("application", true, "database"),
	)
	runtime.stopErr["application"] = errors.New("stop failed")

	result := newExecutor(runtime, testLogger(t), 2).execute(t.Context(), Request{
		Operation: OperationStop,
		Timeout:   time.Minute,
	})

	require.Error(t, result.Err)
	indexed := resultsByContainer(result.Outcomes)
	assert.Equal(t, OutcomeFailed, indexed["application"].Outcome)
	assert.Equal(t, OutcomeBlocked, indexed["database"].Outcome)
	assert.Equal(t, []string{"stop:application"}, runtime.eventLog())
}

func TestExecutorFailsDependentWhenConfiguredHealthNeverBecomesHealthy(t *testing.T) {
	database := testContainer("database", true)
	application := testContainer("application", false, "database")
	application.Labels[healthWaitLabel] = "true"
	runtime := newFakeRuntime(database, application)
	runtime.states["database"] = ContainerState{
		Running:     true,
		Healthcheck: HealthcheckConfigured,
		Health:      HealthUnhealthy,
	}
	executor := newExecutor(runtime, testLogger(t), 1)
	executor.pollInterval = time.Millisecond

	result := executor.execute(t.Context(), Request{Operation: OperationStart, Timeout: 5 * time.Millisecond})

	require.Error(t, result.Err)
	indexed := resultsByContainer(result.Outcomes)
	assert.Equal(t, OutcomeBlocked, indexed["application"].Outcome)
	assert.Equal(t, ReasonDependencyUnhealthy, indexed["application"].Reason)
}

func TestExecutorReportsDependencyCycleForEveryContainer(t *testing.T) {
	runtime := newFakeRuntime(
		testContainer("application", false, "database"),
		testContainer("database", false, "application"),
	)

	result := newExecutor(runtime, testLogger(t), 1).execute(t.Context(), Request{
		Operation: OperationStart,
		Timeout:   time.Minute,
	})

	require.Error(t, result.Err)
	require.Len(t, result.Outcomes, 2)
	for _, outcome := range result.Outcomes {
		assert.Equal(t, OutcomeBlocked, outcome.Outcome)
		assert.Equal(t, ReasonDependencyCycle, outcome.Reason)
	}
}

func TestExecutorReportsCanceledInspectionAsCanceled(t *testing.T) {
	runtime := newFakeRuntime(testContainer("application", false))
	runtime.inspectErr["application"] = context.Canceled

	result := newExecutor(runtime, testLogger(t), 1).execute(t.Context(), Request{
		Operation: OperationStart,
		Timeout:   time.Minute,
	})

	require.Error(t, result.Err)
	require.Len(t, result.Outcomes, 1)
	assert.Equal(t, OutcomeCanceled, result.Outcomes[0].Outcome)
	assert.Equal(t, ReasonCanceledByShutdown, result.Outcomes[0].Reason)
}

func TestExecutorReportsCanceledSnapshotForEveryManagedContainer(t *testing.T) {
	runtime := newFakeRuntime(
		testContainer("application", false),
		testContainer("database", true),
	)
	runtime.listErrors = []error{context.Canceled}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result := newExecutor(runtime, testLogger(t), 1).execute(ctx, Request{
		Operation: OperationStart,
		Timeout:   time.Minute,
	})

	require.ErrorIs(t, result.Err, context.Canceled)
	require.Len(t, result.Outcomes, 2)
	for _, outcome := range result.Outcomes {
		assert.Equal(t, OutcomeCanceled, outcome.Outcome)
		assert.Equal(t, ReasonCanceledByShutdown, outcome.Reason)
	}
}
