package lifecycle

import (
	"context"
	"errors"
	"time"
)

const (
	DefaultQueueCapacity = 100
	MinJobRetention      = time.Hour
	MaxTerminalJobs      = 1000
	CleanupInterval      = 5 * time.Minute
)

var (
	ErrBlocked            = errors.New("operations are blocked")
	ErrClosing            = errors.New("lifecycle controller is shutting down")
	ErrInvalidRequest     = errors.New("invalid lifecycle request")
	ErrJobNotFound        = errors.New("job not found")
	ErrQueueFull          = errors.New("job queue is full")
	ErrShutdownInProgress = errors.New("shutdown operation is in progress")
)

type Operation string

const (
	OperationStart Operation = "start"
	OperationStop  Operation = "stop"
)

type Intent string

const (
	IntentNormal   Intent = ""
	IntentShutdown Intent = "shutdown"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

type Outcome string

const (
	OutcomeChanged   Outcome = "changed"
	OutcomeUnchanged Outcome = "unchanged"
	OutcomeIgnored   Outcome = "ignored"
	OutcomeProtected Outcome = "protected"
	OutcomeBlocked   Outcome = "blocked"
	OutcomeFailed    Outcome = "failed"
	OutcomeCanceled  Outcome = "canceled"
)

type Reason string

const (
	ReasonAlreadyRunning      Reason = "already_running"
	ReasonAlreadyStopped      Reason = "already_stopped"
	ReasonRequestedIgnore     Reason = "requested_ignore"
	ReasonRequiredByIgnored   Reason = "required_by_ignored_container"
	ReasonDependencyIgnored   Reason = "dependency_ignored_unavailable"
	ReasonMissingDependency   Reason = "missing_dependency"
	ReasonDependencyFailed    Reason = "dependency_failed"
	ReasonDependencyUnhealthy Reason = "dependency_unhealthy"
	ReasonOperationFailed     Reason = "operation_failed"
	ReasonCanceledByShutdown  Reason = "canceled_by_shutdown"
	ReasonDeadlineExceeded    Reason = "deadline_exceeded"
	ReasonDependencyCycle     Reason = "dependency_cycle"
)

type Healthcheck string

const (
	HealthcheckNone       Healthcheck = "none"
	HealthcheckDisabled   Healthcheck = "disabled"
	HealthcheckConfigured Healthcheck = "configured"
)

type Health string

const (
	HealthNone      Health = "none"
	HealthStarting  Health = "starting"
	HealthHealthy   Health = "healthy"
	HealthUnhealthy Health = "unhealthy"
)

type Container struct {
	ID          string
	Name        string
	Labels      map[string]string
	Running     bool
	StopTimeout *time.Duration
}

type ContainerState struct {
	Running     bool
	Healthcheck Healthcheck
	Health      Health
}

type Runtime interface {
	ListContainers(context.Context) ([]Container, error)
	InspectContainer(context.Context, string) (ContainerState, error)
	StartContainer(context.Context, string) error
	StopContainer(context.Context, string, time.Duration) error
	Ping(context.Context) error
}

type Request struct {
	Operation Operation
	Intent    Intent
	Timeout   time.Duration
	Ignore    []string
}

type ContainerResult struct {
	Container string  `json:"container"`
	Outcome   Outcome `json:"outcome"`
	Reason    Reason  `json:"reason,omitempty"`
	Error     string  `json:"error,omitempty"`
}

type JobSnapshot struct {
	ID        string
	Operation Operation
	Status    Status
	CreatedAt time.Time
	StartedAt time.Time
	EndedAt   time.Time
	Timeout   time.Duration
	Ignore    []string
	Started   []string
	Stopped   []string
	Skipped   []string
	Failed    []string
	Outcomes  []ContainerResult
	Error     string
}

type ExecutionResult struct {
	Outcomes []ContainerResult
	Err      error
}

func (j JobSnapshot) clone() JobSnapshot {
	j.Ignore = append([]string(nil), j.Ignore...)
	j.Started = append([]string(nil), j.Started...)
	j.Stopped = append([]string(nil), j.Stopped...)
	j.Skipped = append([]string(nil), j.Skipped...)
	j.Failed = append([]string(nil), j.Failed...)
	j.Outcomes = append([]ContainerResult(nil), j.Outcomes...)
	return j
}
