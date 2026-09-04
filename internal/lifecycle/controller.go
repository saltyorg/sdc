package lifecycle

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/saltyorg/sdc/pkg/logger"
)

type Controller struct {
	runtime  Runtime
	executor *executor
	logger   *logger.Logger
	commands chan any
	finished chan executionDone
	done     chan struct{}
}

type jobRecord struct {
	request  Request
	snapshot JobSnapshot
}

type submitCommand struct {
	request Request
	reply   chan submitReply
}

type submitReply struct {
	job JobSnapshot
	err error
}

type jobCommand struct {
	id    string
	reply chan submitReply
}

type blockCommand struct {
	duration time.Duration
	reply    chan error
}

type unblockCommand struct{ reply chan error }

type stateCommand struct{ reply chan error }

type shutdownCommand struct {
	ctx   context.Context
	reply chan error
}

type executionDone struct {
	id     string
	result ExecutionResult
}

func NewController(containerRuntime Runtime, logger *logger.Logger) *Controller {
	controller := &Controller{
		runtime:  containerRuntime,
		executor: newExecutor(containerRuntime, logger, max(runtime.GOMAXPROCS(0), 1)),
		logger:   logger,
		commands: make(chan any),
		finished: make(chan executionDone, 1),
		done:     make(chan struct{}),
	}
	go controller.run()
	return controller
}

func (c *Controller) Submit(ctx context.Context, request Request) (JobSnapshot, error) {
	if request.Timeout <= 0 || (request.Operation != OperationStart && request.Operation != OperationStop) || (request.Intent == IntentShutdown && request.Operation != OperationStop) {
		return JobSnapshot{}, ErrInvalidRequest
	}
	reply := make(chan submitReply, 1)
	if err := c.send(ctx, submitCommand{request: request, reply: reply}); err != nil {
		return JobSnapshot{}, err
	}
	select {
	case response := <-reply:
		return response.job, response.err
	case <-ctx.Done():
		return JobSnapshot{}, ctx.Err()
	case <-c.done:
		return JobSnapshot{}, ErrClosing
	}
}

func (c *Controller) Job(ctx context.Context, id string) (JobSnapshot, error) {
	reply := make(chan submitReply, 1)
	if err := c.send(ctx, jobCommand{id: id, reply: reply}); err != nil {
		return JobSnapshot{}, err
	}
	select {
	case response := <-reply:
		return response.job, response.err
	case <-ctx.Done():
		return JobSnapshot{}, ctx.Err()
	case <-c.done:
		return JobSnapshot{}, ErrClosing
	}
}

func (c *Controller) Block(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ErrInvalidRequest
	}
	reply := make(chan error, 1)
	if err := c.send(ctx, blockCommand{duration: duration, reply: reply}); err != nil {
		return err
	}
	return waitForError(ctx, c.done, reply)
}

func (c *Controller) Unblock(ctx context.Context) error {
	reply := make(chan error, 1)
	if err := c.send(ctx, unblockCommand{reply: reply}); err != nil {
		return err
	}
	return waitForError(ctx, c.done, reply)
}

func (c *Controller) Ready(ctx context.Context) error {
	reply := make(chan error, 1)
	if err := c.send(ctx, stateCommand{reply: reply}); err != nil {
		return err
	}
	if err := waitForError(ctx, c.done, reply); err != nil {
		return err
	}
	if err := c.runtime.Ping(ctx); err != nil {
		return fmt.Errorf("docker is unavailable: %w", err)
	}
	return nil
}

func (c *Controller) Shutdown(ctx context.Context) error {
	select {
	case <-c.done:
		return nil
	default:
	}
	reply := make(chan error, 1)
	if err := c.send(ctx, shutdownCommand{ctx: ctx, reply: reply}); err != nil {
		return err
	}
	return waitForError(ctx, c.done, reply)
}

func (c *Controller) send(ctx context.Context, command any) error {
	select {
	case c.commands <- command:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return ErrClosing
	}
}

func waitForError(ctx context.Context, done <-chan struct{}, reply <-chan error) error {
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (c *Controller) run() {
	defer close(c.done)
	jobs := make(map[string]*jobRecord)
	queue := make([]*jobRecord, 0, DefaultQueueCapacity)
	var active *jobRecord
	var activeCancel context.CancelFunc
	blocked := false
	shutdownMode := false
	shutdownJobID := ""
	closing := false
	var shutdownReply chan error
	var shutdownContext context.Context
	var shutdownDone <-chan struct{}
	var shutdownErr error
	cleanupTicker := time.NewTicker(CleanupInterval)
	defer cleanupTicker.Stop()
	var blockTimer *time.Timer
	var blockDone <-chan time.Time

	finishClosing := func() bool {
		if !closing || active != nil || len(queue) != 0 {
			return false
		}
		if shutdownReply != nil {
			shutdownReply <- shutdownErr
		}
		return true
	}

	for {
		if active == nil && len(queue) > 0 {
			active = queue[0]
			queue = queue[1:]
			active.snapshot.Status = StatusRunning
			active.snapshot.StartedAt = time.Now()
			executionContext, cancel := context.WithCancel(context.Background())
			activeCancel = cancel
			job := active
			go func() {
				c.finished <- executionDone{id: job.snapshot.ID, result: c.executor.execute(executionContext, job.request)}
			}()
		}
		if finishClosing() {
			return
		}

		select {
		case raw := <-c.commands:
			switch command := raw.(type) {
			case submitCommand:
				if closing {
					command.reply <- submitReply{err: ErrClosing}
					continue
				}
				if blocked {
					command.reply <- submitReply{err: ErrBlocked}
					continue
				}
				if command.request.Operation == OperationStart && shutdownMode {
					command.reply <- submitReply{err: ErrShutdownInProgress}
					continue
				}
				if command.request.Intent == IntentShutdown && shutdownJobID != "" {
					command.reply <- submitReply{job: jobs[shutdownJobID].snapshot.clone()}
					continue
				}
				if len(queue) >= DefaultQueueCapacity {
					command.reply <- submitReply{err: ErrQueueFull}
					continue
				}
				job := newJob(command.request)
				jobs[job.snapshot.ID] = job
				if command.request.Intent == IntentShutdown {
					shutdownMode = true
					shutdownJobID = job.snapshot.ID
					if active != nil && active.request.Operation == OperationStart {
						activeCancel()
					}
					queue = failQueuedStarts(queue, ReasonCanceledByShutdown)
					queue = append([]*jobRecord{job}, queue...)
				} else {
					queue = append(queue, job)
				}
				command.reply <- submitReply{job: job.snapshot.clone()}

			case jobCommand:
				job, exists := jobs[command.id]
				if !exists {
					command.reply <- submitReply{err: fmt.Errorf("%w: %s", ErrJobNotFound, command.id)}
					continue
				}
				command.reply <- submitReply{job: job.snapshot.clone()}

			case blockCommand:
				blocked = true
				if blockTimer == nil {
					blockTimer = time.NewTimer(command.duration)
				} else {
					if !blockTimer.Stop() {
						select {
						case <-blockTimer.C:
						default:
						}
					}
					blockTimer.Reset(command.duration)
				}
				blockDone = blockTimer.C
				command.reply <- nil

			case unblockCommand:
				blocked = false
				if blockTimer != nil && !blockTimer.Stop() {
					select {
					case <-blockTimer.C:
					default:
					}
				}
				blockDone = nil
				command.reply <- nil

			case stateCommand:
				if closing {
					command.reply <- ErrClosing
				} else {
					command.reply <- nil
				}

			case shutdownCommand:
				if closing {
					command.reply <- ErrClosing
					continue
				}
				closing = true
				shutdownReply = command.reply
				shutdownContext = command.ctx
				shutdownDone = command.ctx.Done()
				if active != nil && active.request.Operation == OperationStart {
					activeCancel()
				}
				queue = failQueuedStarts(queue, ReasonCanceledByShutdown)
			}

		case completed := <-c.finished:
			if active == nil || active.snapshot.ID != completed.id {
				continue
			}
			activeCancel()
			completeJob(active, completed.result)
			if active.snapshot.ID == shutdownJobID {
				shutdownMode = false
				shutdownJobID = ""
			}
			active = nil
			activeCancel = nil

		case <-blockDone:
			blocked = false
			blockDone = nil

		case <-cleanupTicker.C:
			cleanupJobs(jobs, time.Now())

		case <-shutdownDone:
			shutdownErr = shutdownContext.Err()
			shutdownDone = nil
			if active != nil {
				activeCancel()
			}
			for _, job := range queue {
				failPendingJob(job, ReasonDeadlineExceeded)
			}
			queue = nil
		}
	}
}

func newJob(request Request) *jobRecord {
	now := time.Now()
	return &jobRecord{request: request, snapshot: JobSnapshot{
		ID: uuid.NewString(), Operation: request.Operation, Status: StatusPending,
		CreatedAt: now, Timeout: request.Timeout, Ignore: append([]string(nil), request.Ignore...),
	}}
}

func failQueuedStarts(queue []*jobRecord, reason Reason) []*jobRecord {
	kept := queue[:0]
	for _, job := range queue {
		if job.request.Operation == OperationStart {
			failPendingJob(job, reason)
			continue
		}
		kept = append(kept, job)
	}
	return kept
}

func failPendingJob(job *jobRecord, reason Reason) {
	job.snapshot.Status = StatusFailed
	job.snapshot.EndedAt = time.Now()
	job.snapshot.Error = string(reason)
}

func completeJob(job *jobRecord, result ExecutionResult) {
	job.snapshot.EndedAt = time.Now()
	job.snapshot.Outcomes = append([]ContainerResult(nil), result.Outcomes...)
	for _, outcome := range result.Outcomes {
		switch outcome.Outcome {
		case OutcomeChanged:
			if job.request.Operation == OperationStart {
				job.snapshot.Started = append(job.snapshot.Started, outcome.Container)
			} else {
				job.snapshot.Stopped = append(job.snapshot.Stopped, outcome.Container)
			}
		case OutcomeUnchanged, OutcomeIgnored, OutcomeProtected:
			job.snapshot.Skipped = append(job.snapshot.Skipped, outcome.Container)
		case OutcomeFailed, OutcomeBlocked, OutcomeCanceled:
			job.snapshot.Failed = append(job.snapshot.Failed, outcome.Container)
		}
	}
	sort.Strings(job.snapshot.Started)
	sort.Strings(job.snapshot.Stopped)
	sort.Strings(job.snapshot.Skipped)
	sort.Strings(job.snapshot.Failed)
	if result.Err != nil {
		job.snapshot.Status = StatusFailed
		job.snapshot.Error = result.Err.Error()
	} else {
		job.snapshot.Status = StatusCompleted
	}
}

func cleanupJobs(jobs map[string]*jobRecord, now time.Time) {
	var terminal []*jobRecord
	for id, job := range jobs {
		if job.snapshot.Status != StatusCompleted && job.snapshot.Status != StatusFailed {
			continue
		}
		if !job.snapshot.EndedAt.IsZero() && now.Sub(job.snapshot.EndedAt) > MinJobRetention {
			delete(jobs, id)
			continue
		}
		terminal = append(terminal, job)
	}
	if len(terminal) <= MaxTerminalJobs {
		return
	}
	sort.Slice(terminal, func(i, j int) bool { return terminal[i].snapshot.EndedAt.Before(terminal[j].snapshot.EndedAt) })
	for _, job := range terminal[:len(terminal)-MaxTerminalJobs] {
		delete(jobs, job.snapshot.ID)
	}
}
