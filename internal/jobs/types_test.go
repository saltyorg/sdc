package jobs

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewJob(t *testing.T) {
	job := NewJob(JobTypeStart, 600, []string{"traefik"})

	assert.NotEmpty(t, job.ID)
	assert.Equal(t, JobTypeStart, job.Type)
	assert.Equal(t, JobStatusPending, job.Status)
	assert.Equal(t, 600, job.Timeout)
	assert.Equal(t, []string{"traefik"}, job.Ignore)
	assert.NotZero(t, job.CreatedAt)
	assert.True(t, job.StartedAt.IsZero())
	assert.True(t, job.EndedAt.IsZero())
}

func TestJob_SetStatus(t *testing.T) {
	job := NewJob(JobTypeStart, 600, nil)

	// Set to running should set StartedAt
	job.SetStatus(JobStatusRunning)
	assert.Equal(t, JobStatusRunning, job.Status)
	assert.False(t, job.StartedAt.IsZero())
	assert.True(t, job.EndedAt.IsZero())

	startedAt := job.StartedAt

	// Set to running again should not update StartedAt
	time.Sleep(10 * time.Millisecond)
	job.SetStatus(JobStatusRunning)
	assert.Equal(t, startedAt, job.StartedAt)

	// Set to completed should set EndedAt
	job.SetStatus(JobStatusCompleted)
	assert.Equal(t, JobStatusCompleted, job.Status)
	assert.False(t, job.EndedAt.IsZero())

	endedAt := job.EndedAt

	// Set to completed again should not update EndedAt
	time.Sleep(10 * time.Millisecond)
	job.SetStatus(JobStatusCompleted)
	assert.Equal(t, endedAt, job.EndedAt)
}

func TestJob_SetError(t *testing.T) {
	job := NewJob(JobTypeStart, 600, nil)

	err := errors.New("test error")
	job.SetError(err)

	assert.Equal(t, JobStatusFailed, job.Status)
	assert.Equal(t, "test error", job.Error)
	assert.False(t, job.EndedAt.IsZero())
}

func TestJob_SetResults(t *testing.T) {
	job := NewJob(JobTypeStart, 600, nil)

	started := []string{"nginx", "redis"}
	skipped := []string{"traefik"}
	failed := []string{"broken"}

	job.SetResults(started, nil, skipped, failed)

	assert.Equal(t, started, job.Started)
	assert.Empty(t, job.Stopped)
	assert.Equal(t, skipped, job.Skipped)
	assert.Equal(t, failed, job.Failed)
}

func TestJob_Clone(t *testing.T) {
	original := NewJob(JobTypeStart, 600, []string{"traefik"})
	original.SetStatus(JobStatusRunning)
	original.SetResults([]string{"nginx"}, nil, []string{"redis"}, nil)

	clone := original.Clone()

	// Verify values match
	assert.Equal(t, original.ID, clone.ID)
	assert.Equal(t, original.Type, clone.Type)
	assert.Equal(t, original.Status, clone.Status)
	assert.Equal(t, original.Timeout, clone.Timeout)
	assert.Equal(t, original.Started, clone.Started)

	// Verify it's a deep copy (modifying clone doesn't affect original)
	clone.Started = append(clone.Started, "postgres")
	assert.NotEqual(t, original.Started, clone.Started)
}

func TestJob_Duration(t *testing.T) {
	job := NewJob(JobTypeStart, 600, nil)

	// Not started yet
	assert.Equal(t, time.Duration(0), job.Duration())

	// Start the job
	job.SetStatus(JobStatusRunning)
	time.Sleep(50 * time.Millisecond)

	duration1 := job.Duration()
	assert.Greater(t, duration1, 40*time.Millisecond)

	// Complete the job
	time.Sleep(50 * time.Millisecond)
	job.SetStatus(JobStatusCompleted)

	duration2 := job.Duration()
	assert.Greater(t, duration2, duration1)

	// Duration should be stable after completion
	time.Sleep(50 * time.Millisecond)
	duration3 := job.Duration()
	assert.Equal(t, duration2, duration3)
}

func TestJob_Age(t *testing.T) {
	job := NewJob(JobTypeStart, 600, nil)

	age1 := job.Age()
	assert.Greater(t, age1, time.Duration(0))

	time.Sleep(50 * time.Millisecond)
	age2 := job.Age()
	assert.Greater(t, age2, age1)
}

func TestJob_GetStatus_ThreadSafe(t *testing.T) {
	job := NewJob(JobTypeStart, 600, nil)

	// Test concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			status := job.GetStatus()
			assert.Equal(t, JobStatusPending, status)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestJob_SetStatus_ThreadSafe(t *testing.T) {
	job := NewJob(JobTypeStart, 600, nil)

	// Test concurrent writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			job.SetStatus(JobStatusRunning)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, JobStatusRunning, job.Status)
}
