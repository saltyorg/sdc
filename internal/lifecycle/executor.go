package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/saltyorg/sdc/pkg/logger"
)

const defaultStopTimeout = 10 * time.Second

type executor struct {
	runtime      Runtime
	logger       *logger.Logger
	maxParallel  int
	pollInterval time.Duration
}

type nodeResult struct {
	result    ContainerResult
	available bool
	active    bool
}

func newExecutor(runtime Runtime, logger *logger.Logger, maxParallel int) *executor {
	if maxParallel < 1 {
		maxParallel = 1
	}
	return &executor{runtime: runtime, logger: logger, maxParallel: maxParallel, pollInterval: 500 * time.Millisecond}
}

func (e *executor) execute(parent context.Context, request Request) ExecutionResult {
	ctx, cancel := context.WithTimeout(parent, request.Timeout)
	defer cancel()

	containers, err := e.runtime.ListContainers(ctx)
	if err != nil {
		return ExecutionResult{Err: fmt.Errorf("listing containers: %w", err)}
	}
	plan, err := buildPlan(containers)
	if err != nil {
		var cycle *cycleError
		if errors.As(err, &cycle) && plan != nil {
			outcomes := make([]ContainerResult, 0, len(plan.nodes))
			for _, current := range plan.nodes {
				outcomes = append(outcomes, ContainerResult{
					Container: current.container.Name,
					Outcome:   OutcomeBlocked,
					Reason:    ReasonDependencyCycle,
					Error:     err.Error(),
				})
			}
			return ExecutionResult{Outcomes: outcomes, Err: err}
		}
		return ExecutionResult{Err: err}
	}

	ignored := make(map[string]bool, len(request.Ignore))
	for _, name := range request.Ignore {
		ignored[name] = true
	}

	known := make(map[string]nodeResult, len(plan.nodes))
	var outcomes []ContainerResult
	batches := plan.startup
	run := e.startNode
	if request.Operation == OperationStop {
		batches = plan.shutdown
		run = e.stopNode
	}
	for _, batch := range batches {
		results := e.runBatch(ctx, batch, func(ctx context.Context, current *node) nodeResult {
			return run(ctx, current, known, ignored)
		})
		for _, result := range results {
			known[result.result.Container] = result
			outcomes = append(outcomes, result.result)
		}
	}
	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].Container < outcomes[j].Container })

	for _, outcome := range outcomes {
		if outcome.Outcome == OutcomeFailed || outcome.Outcome == OutcomeBlocked || outcome.Outcome == OutcomeCanceled {
			return ExecutionResult{Outcomes: outcomes, Err: errors.New("one or more containers did not reach the requested state")}
		}
	}
	return ExecutionResult{Outcomes: outcomes}
}

func (e *executor) runBatch(ctx context.Context, batch []*node, run func(context.Context, *node) nodeResult) []nodeResult {
	jobs := make(chan *node)
	results := make(chan nodeResult, len(batch))
	workers := min(len(batch), e.maxParallel)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for current := range jobs {
				results <- run(ctx, current)
			}
		})
	}
	for _, current := range batch {
		jobs <- current
	}
	close(jobs)
	wg.Wait()
	close(results)

	collected := make([]nodeResult, 0, len(batch))
	for result := range results {
		collected = append(collected, result)
	}
	sort.Slice(collected, func(i, j int) bool { return collected[i].result.Container < collected[j].result.Container })
	return collected
}

func (e *executor) startNode(ctx context.Context, current *node, known map[string]nodeResult, ignored map[string]bool) nodeResult {
	state, err := e.runtime.InspectContainer(ctx, current.container.ID)
	if err != nil {
		return failedResult(current, err)
	}
	if ignored[current.container.Name] {
		return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeIgnored, Reason: ReasonRequestedIgnore}, available: state.Running, active: state.Running}
	}
	if state.Running {
		return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeUnchanged, Reason: ReasonAlreadyRunning}, available: true, active: true}
	}
	if len(current.missing) > 0 {
		return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeBlocked, Reason: ReasonMissingDependency, Error: current.missing[0]}}
	}
	for _, parent := range current.parents {
		parentResult := known[parent.container.Name]
		switch parentResult.result.Outcome {
		case OutcomeFailed, OutcomeBlocked, OutcomeCanceled:
			return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeBlocked, Reason: ReasonDependencyFailed, Error: parent.container.Name}}
		case OutcomeIgnored, OutcomeProtected:
			if !parentResult.available {
				return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeProtected, Reason: ReasonDependencyIgnored, Error: parent.container.Name}}
			}
		}
		if current.waitHealth {
			if err := e.waitForHealthy(ctx, parent.container.ID); err != nil {
				return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeBlocked, Reason: ReasonDependencyUnhealthy, Error: err.Error()}}
			}
		}
	}
	if current.delay > 0 {
		timer := time.NewTimer(current.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return canceledResult(current, ctx.Err())
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return canceledResult(current, err)
	}
	if err := e.runtime.StartContainer(ctx, current.container.ID); err != nil {
		if ctx.Err() != nil {
			return canceledResult(current, ctx.Err())
		}
		return failedResult(current, err)
	}
	return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeChanged}, available: true, active: true}
}

func (e *executor) stopNode(ctx context.Context, current *node, known map[string]nodeResult, ignored map[string]bool) nodeResult {
	state, err := e.runtime.InspectContainer(ctx, current.container.ID)
	if err != nil {
		return failedResult(current, err)
	}
	if ignored[current.container.Name] {
		return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeIgnored, Reason: ReasonRequestedIgnore}, available: state.Running, active: state.Running}
	}
	if !state.Running {
		return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeUnchanged, Reason: ReasonAlreadyStopped}}
	}
	for _, child := range current.children {
		childResult := known[child.container.Name]
		switch childResult.result.Outcome {
		case OutcomeFailed, OutcomeBlocked, OutcomeCanceled:
			return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeBlocked, Reason: ReasonDependencyFailed, Error: child.container.Name}, active: true}
		case OutcomeIgnored, OutcomeProtected:
			if childResult.active {
				return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeProtected, Reason: ReasonRequiredByIgnored, Error: child.container.Name}, active: true}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return canceledResult(current, err)
	}
	timeout := defaultStopTimeout
	if current.container.StopTimeout != nil {
		timeout = *current.container.StopTimeout
	}
	if err := e.runtime.StopContainer(ctx, current.container.ID, timeout); err != nil {
		if ctx.Err() != nil {
			return canceledResult(current, ctx.Err())
		}
		return failedResult(current, err)
	}
	return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeChanged}}
}

func (e *executor) waitForHealthy(ctx context.Context, id string) error {
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()
	for {
		state, err := e.runtime.InspectContainer(ctx, id)
		if err == nil {
			switch state.Healthcheck {
			case HealthcheckNone, HealthcheckDisabled:
				return nil
			case HealthcheckConfigured:
				if state.Health == HealthHealthy {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return fmt.Errorf("inspecting dependency health: %w", err)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func failedResult(current *node, err error) nodeResult {
	return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeFailed, Reason: ReasonOperationFailed, Error: err.Error()}}
}

func canceledResult(current *node, err error) nodeResult {
	reason := ReasonCanceledByShutdown
	if errors.Is(err, context.DeadlineExceeded) {
		reason = ReasonDeadlineExceeded
	}
	return nodeResult{result: ContainerResult{Container: current.container.Name, Outcome: OutcomeCanceled, Reason: reason, Error: err.Error()}}
}
