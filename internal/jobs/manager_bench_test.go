package jobs

import (
	"testing"
)

// BenchmarkJobCreation benchmarks job creation
func BenchmarkJobCreation(b *testing.B) {

	for b.Loop() {
		_ = NewJob(JobTypeStart, 600, nil)
	}
}

// BenchmarkJobCreationWithIgnore benchmarks job creation with ignore list
func BenchmarkJobCreationWithIgnore(b *testing.B) {
	ignore := []string{"container1", "container2", "container3"}

	for b.Loop() {
		_ = NewJob(JobTypeStart, 600, ignore)
	}
}

// BenchmarkJobStatusUpdate benchmarks status updates
func BenchmarkJobStatusUpdate(b *testing.B) {
	job := NewJob(JobTypeStart, 600, nil)

	for b.Loop() {
		job.SetStatus(JobStatusRunning)
		job.SetStatus(JobStatusCompleted)
	}
}

// BenchmarkJobGetStatus benchmarks getting job status
func BenchmarkJobGetStatus(b *testing.B) {
	job := NewJob(JobTypeStart, 600, nil)
	job.SetStatus(JobStatusRunning)

	for b.Loop() {
		_ = job.GetStatus()
	}
}

// BenchmarkJobResultUpdate benchmarks updating job results
func BenchmarkJobResultUpdate(b *testing.B) {
	job := NewJob(JobTypeStart, 600, nil)
	started := []string{"container1", "container2", "container3"}

	for b.Loop() {
		job.Started = started
		job.Stopped = started
		job.Failed = started
		job.Skipped = started
	}
}
