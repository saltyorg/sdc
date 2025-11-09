package jobs

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// JobType represents the type of operation being performed
type JobType string

const (
	JobTypeStart JobType = "start"
	JobTypeStop  JobType = "stop"
)

// JobStatus represents the current state of a job
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

// Job represents a container orchestration operation
type Job struct {
	ID        string    `json:"id"`
	Type      JobType   `json:"type"`
	Status    JobStatus `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	StartedAt time.Time `json:"started_at,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitempty"`

	// Operation parameters
	Timeout int      `json:"timeout"`
	Ignore  []string `json:"ignore"`

	// Results
	Started []string `json:"started,omitempty"` // For start operations
	Stopped []string `json:"stopped,omitempty"` // For stop operations
	Skipped []string `json:"skipped,omitempty"`
	Failed  []string `json:"failed,omitempty"`

	// Error information
	Error string `json:"error,omitempty"`

	mu sync.RWMutex
}

// NewJob creates a new job with a generated UUID
func NewJob(jobType JobType, timeout int, ignore []string) *Job {
	return &Job{
		ID:        uuid.New().String(),
		Type:      jobType,
		Status:    JobStatusPending,
		CreatedAt: time.Now(),
		Timeout:   timeout,
		Ignore:    ignore,
		Started:   []string{},
		Stopped:   []string{},
		Skipped:   []string{},
		Failed:    []string{},
	}
}

// GetStatus returns the current job status (thread-safe)
func (j *Job) GetStatus() JobStatus {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status
}

// SetStatus updates the job status (thread-safe)
func (j *Job) SetStatus(status JobStatus) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.Status = status
	now := time.Now()

	switch status {
	case JobStatusRunning:
		if j.StartedAt.IsZero() {
			j.StartedAt = now
		}
	case JobStatusCompleted, JobStatusFailed:
		if j.EndedAt.IsZero() {
			j.EndedAt = now
		}
	}
}

// SetError sets the error message and marks the job as failed
func (j *Job) SetError(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.Error = err.Error()
	j.Status = JobStatusFailed
	if j.EndedAt.IsZero() {
		j.EndedAt = time.Now()
	}
}

// SetResults updates the job results (thread-safe)
func (j *Job) SetResults(started, stopped, skipped, failed []string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if started != nil {
		j.Started = started
	}
	if stopped != nil {
		j.Stopped = stopped
	}
	if skipped != nil {
		j.Skipped = skipped
	}
	if failed != nil {
		j.Failed = failed
	}
}

// Clone creates a deep copy of the job (thread-safe)
func (j *Job) Clone() *Job {
	j.mu.RLock()
	defer j.mu.RUnlock()

	return &Job{
		ID:        j.ID,
		Type:      j.Type,
		Status:    j.Status,
		CreatedAt: j.CreatedAt,
		StartedAt: j.StartedAt,
		EndedAt:   j.EndedAt,
		Timeout:   j.Timeout,
		Ignore:    append([]string{}, j.Ignore...),
		Started:   append([]string{}, j.Started...),
		Stopped:   append([]string{}, j.Stopped...),
		Skipped:   append([]string{}, j.Skipped...),
		Failed:    append([]string{}, j.Failed...),
		Error:     j.Error,
	}
}

// Duration returns how long the job took to complete
func (j *Job) Duration() time.Duration {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if j.StartedAt.IsZero() {
		return 0
	}
	if j.EndedAt.IsZero() {
		return time.Since(j.StartedAt)
	}
	return j.EndedAt.Sub(j.StartedAt)
}

// Age returns how long ago the job was created
func (j *Job) Age() time.Duration {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return time.Since(j.CreatedAt)
}
