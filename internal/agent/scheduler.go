package agent

import (
	"context"
	"errors"
	"log/slog"
	"runtime/pprof"
	"strconv"
	"sync"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/syncmap"
)

type taskStore interface {
	CreateTask(ctx context.Context, t store.Task) (store.Task, error)
	ListTasks(ctx context.Context, recentFinished int) ([]store.Task, error)
	CancelTask(ctx context.Context, id uint64) (store.Task, error)
	ClaimDueTasks(ctx context.Context, now time.Time) ([]store.Task, error)
	FinishTask(ctx context.Context, id uint64, status store.TaskStatus, result string) error
	InterruptRunningTasks(ctx context.Context) ([]store.Task, error)
}

type Scheduler struct {
	store       taskStore
	turn        func(ctx context.Context, task store.Task) (taskReply, error)
	post        func(ctx context.Context, channelID snowflake.ID, content string) error
	userName    func(id snowflake.ID) string
	turnTimeout time.Duration
	wakeups     chan struct{}
	cancels     taskCancels
	interrupted chan []store.Task
	wg          sync.WaitGroup
}

func newScheduler(
	s taskStore,
	turnTimeout time.Duration,
	turn func(ctx context.Context, task store.Task) (taskReply, error),
	post func(ctx context.Context, channelID snowflake.ID, content string) error,
	userName func(id snowflake.ID) string,
) *Scheduler {
	return &Scheduler{
		store:       s,
		turn:        turn,
		post:        post,
		userName:    userName,
		turnTimeout: turnTimeout,
		wakeups:     make(chan struct{}, 1),
		interrupted: make(chan []store.Task, 1),
	}
}

// Run interrupts tasks a previous process left running, then fires due tasks until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	s.wg.Go(func() { s.loop(ctx) })
}

func (s *Scheduler) Wait() {
	s.wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context) {
	interrupted, err := s.store.InterruptRunningTasks(ctx)
	if err != nil {
		slog.Warn("failed to interrupt leftover running tasks", "error", err)
	}
	select {
	case s.interrupted <- interrupted:
	default:
	}

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.fireDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wakeups:
		}
	}
}

func (s *Scheduler) interruptedTasks() <-chan []store.Task {
	return s.interrupted
}

func (s *Scheduler) wake() {
	select {
	case s.wakeups <- struct{}{}:
	default:
	}
}

func (s *Scheduler) fireDue(ctx context.Context) {
	tasks, err := s.store.ClaimDueTasks(ctx, time.Now())
	if err != nil {
		slog.Warn("failed to claim due tasks", "error", err)
		return
	}
	for _, task := range tasks {
		labels := pprof.Labels("handler", "task", "task", strconv.FormatUint(task.ID, 10), "channel", strconv.FormatUint(task.ChannelID, 10))
		s.wg.Go(func() { pprof.Do(ctx, labels, func(ctx context.Context) { s.runTask(ctx, task) }) })
	}
}

func (s *Scheduler) runTask(ctx context.Context, task store.Task) {
	ctx, cancel := context.WithTimeout(ctx, s.turnTimeout)
	s.cancels.register(task.ID, cancel)
	defer func() {
		s.cancels.forget(task.ID)
		cancel()
	}()

	reply, err := s.turn(ctx, task)
	if abandonedByCancelOrShutdown(ctx) {
		return
	}

	status, result := store.TaskDone, reply.Text
	if err == nil && !reply.Declined && reply.Text != "" {
		err = s.post(ctx, snowflake.ID(task.ChannelID), taskMessage(snowflake.ID(task.RequesterID), reply.Text))
	}
	if err != nil {
		slog.Warn("task failed", "task", task.ID, "error", err)
		status, result = store.TaskFailed, err.Error()
	}

	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	if err := s.store.FinishTask(finishCtx, task.ID, status, capText(result, maxTaskResultLen)); err != nil {
		slog.Warn("failed to record task result", "task", task.ID, "status", status, "error", err)
	}
}

func abandonedByCancelOrShutdown(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

type taskCancels struct {
	byID syncmap.Map[uint64, context.CancelFunc]
}

func (c *taskCancels) register(id uint64, cancel context.CancelFunc) {
	c.byID.Store(id, cancel)
}

func (c *taskCancels) forget(id uint64) {
	c.byID.Delete(id)
}

func (c *taskCancels) cancel(id uint64) {
	if cancel, ok := c.byID.LoadAndDelete(id); ok {
		cancel()
	}
}
