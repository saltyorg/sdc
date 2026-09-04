package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCompleteJobMapsOutcomesToLegacyArrays(t *testing.T) {
	job := newJob(Request{Operation: OperationStart, Timeout: time.Minute})
	completeJob(job, ExecutionResult{
		Outcomes: []ContainerResult{
			{Container: "changed", Outcome: OutcomeChanged},
			{Container: "ignored", Outcome: OutcomeIgnored},
			{Container: "protected", Outcome: OutcomeProtected},
			{Container: "blocked", Outcome: OutcomeBlocked},
		},
		Err: errors.New("dependency failure"),
	})

	assert.Equal(t, StatusFailed, job.snapshot.Status)
	assert.Equal(t, []string{"changed"}, job.snapshot.Started)
	assert.Equal(t, []string{"ignored", "protected"}, job.snapshot.Skipped)
	assert.Equal(t, []string{"blocked"}, job.snapshot.Failed)
	assert.Equal(t, "dependency failure", job.snapshot.Error)
}

func TestCleanupUsesCompletionTimeAndNeverEvictsActiveJobs(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	oldCompleted := newJob(Request{Operation: OperationStart, Timeout: time.Minute})
	oldCompleted.snapshot.Status = StatusCompleted
	oldCompleted.snapshot.EndedAt = now.Add(-2 * time.Hour)
	recentLongRunning := newJob(Request{Operation: OperationStart, Timeout: time.Minute})
	recentLongRunning.snapshot.CreatedAt = now.Add(-2 * time.Hour)
	recentLongRunning.snapshot.Status = StatusCompleted
	recentLongRunning.snapshot.EndedAt = now.Add(-time.Minute)
	active := newJob(Request{Operation: OperationStop, Timeout: time.Minute})
	active.snapshot.CreatedAt = now.Add(-24 * time.Hour)
	active.snapshot.Status = StatusRunning
	jobs := map[string]*jobRecord{
		oldCompleted.snapshot.ID:      oldCompleted,
		recentLongRunning.snapshot.ID: recentLongRunning,
		active.snapshot.ID:            active,
	}

	cleanupJobs(jobs, now)

	assert.NotContains(t, jobs, oldCompleted.snapshot.ID)
	assert.Contains(t, jobs, recentLongRunning.snapshot.ID)
	assert.Contains(t, jobs, active.snapshot.ID)
}

func TestSubmitRejectsInvalidLifecycleRequest(t *testing.T) {
	controller := NewController(newFakeRuntime(), testLogger(t))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		assert.NoError(t, controller.Shutdown(ctx))
	})

	_, err := controller.Submit(t.Context(), Request{Operation: OperationStart})
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = controller.Submit(t.Context(), Request{Operation: "invalid", Timeout: time.Minute})
	assert.ErrorIs(t, err, ErrInvalidRequest)
}
