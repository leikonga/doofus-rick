package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

func createTask(t *testing.T, s *store.Store, prompt string, fireAt time.Time) store.Task {
	t.Helper()
	task, err := s.CreateTask(context.Background(), store.Task{ChannelID: 10, RequesterID: 20, Prompt: prompt, FireAt: fireAt})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

func taskByID(t *testing.T, s *store.Store, id uint64) store.Task {
	t.Helper()
	rows := pgtest.Query[store.Task](t, "SELECT * FROM tasks WHERE id = ?", id)
	if len(rows) != 1 {
		t.Fatalf("load task %d: %d rows", id, len(rows))
	}
	return rows[0]
}

func prompts(tasks []store.Task) []string {
	out := make([]string, len(tasks))
	for i, task := range tasks {
		out[i] = task.Prompt
	}
	return out
}

func TestTaskCreate(t *testing.T) {
	s := pgtest.Store(t)
	fireAt := time.Now().Add(time.Hour)
	task, err := s.CreateTask(context.Background(), store.Task{ChannelID: 1, RequesterID: 2, Prompt: "p", FireAt: fireAt, Status: store.TaskDone})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID != 1 {
		t.Errorf("ID = %d, want 1", task.ID)
	}
	if task.Status != store.TaskPending {
		t.Errorf("Status = %q, want pending regardless of input", task.Status)
	}
	got := taskByID(t, s, task.ID)
	if got.Status != "pending" || got.Prompt != "p" || got.ChannelID != 1 || got.RequesterID != 2 {
		t.Errorf("unexpected row: %+v", got)
	}
	if got.StartedAt != nil || got.FinishedAt != nil {
		t.Errorf("started/finished should be nil: %+v", got)
	}
}

func TestTaskClaimDue(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	now := time.Now()
	past := createTask(t, s, "past", now.Add(-time.Minute))
	older := createTask(t, s, "older", now.Add(-time.Hour))
	exact := createTask(t, s, "exact", now)
	future := createTask(t, s, "future", now.Add(time.Hour))

	claimed, err := s.ClaimDueTasks(ctx, now)
	if err != nil {
		t.Fatalf("ClaimDueTasks: %v", err)
	}
	ids := make([]uint64, len(claimed))
	for i, c := range claimed {
		ids[i] = c.ID
		if c.Status != store.TaskRunning {
			t.Errorf("task %d status = %q, want running", c.ID, c.Status)
		}
		if c.StartedAt == nil {
			t.Errorf("task %d StartedAt nil", c.ID)
		}
	}
	slices.Sort(ids)
	want := []uint64{past.ID, older.ID, exact.ID}
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Errorf("claimed ids = %v, want %v", ids, want)
	}
	if got := taskByID(t, s, future.ID); got.Status != store.TaskPending {
		t.Errorf("future status = %q, want pending", got.Status)
	}

	again, err := s.ClaimDueTasks(ctx, now)
	if err != nil {
		t.Fatalf("second ClaimDueTasks: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second claim returned %d tasks, want 0", len(again))
	}

	later, err := s.ClaimDueTasks(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("later ClaimDueTasks: %v", err)
	}
	if len(later) != 1 || later[0].ID != future.ID {
		t.Errorf("later claim = %+v, want only future task", later)
	}
}

func TestTaskFinish(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, s *store.Store, id uint64)
		wantStatus store.TaskStatus
		wantResult string
		wantFinish bool
	}{
		{
			name: "running task",
			prepare: func(t *testing.T, s *store.Store, id uint64) {
				if _, err := s.ClaimDueTasks(context.Background(), time.Now()); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: store.TaskDone,
			wantResult: "ok",
			wantFinish: true,
		},
		{
			name:       "pending task untouched",
			prepare:    func(t *testing.T, s *store.Store, id uint64) {},
			wantStatus: store.TaskPending,
		},
		{
			name: "cancelled task stays cancelled",
			prepare: func(t *testing.T, s *store.Store, id uint64) {
				if _, err := s.CancelTask(context.Background(), id); err != nil {
					t.Fatal(err)
				}
			},
			wantStatus: store.TaskCancelled,
			wantFinish: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := pgtest.Store(t)
			task := createTask(t, s, "p", time.Now().Add(-time.Minute))
			tt.prepare(t, s, task.ID)
			if err := s.FinishTask(context.Background(), task.ID, store.TaskDone, "ok"); err != nil {
				t.Fatalf("FinishTask: %v", err)
			}
			got := taskByID(t, s, task.ID)
			if got.Status != tt.wantStatus || got.Result != tt.wantResult {
				t.Errorf("status/result = %q/%q, want %q/%q", got.Status, got.Result, tt.wantStatus, tt.wantResult)
			}
			if (got.FinishedAt != nil) != tt.wantFinish {
				t.Errorf("FinishedAt set = %v, want %v", got.FinishedAt != nil, tt.wantFinish)
			}
		})
	}
}

func TestTaskCancel(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()

	pending := createTask(t, s, "pending", time.Now().Add(time.Hour))
	before, err := s.CancelTask(ctx, pending.ID)
	if err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if before.Status != store.TaskPending {
		t.Errorf("returned status = %q, want pre-cancel pending", before.Status)
	}
	got := taskByID(t, s, pending.ID)
	if got.Status != store.TaskCancelled || got.FinishedAt == nil {
		t.Errorf("after cancel: %+v", got)
	}

	before, err = s.CancelTask(ctx, pending.ID)
	if !errors.Is(err, store.ErrTaskFinished) {
		t.Fatalf("second cancel err = %v, want ErrTaskFinished", err)
	}
	if before.Status != store.TaskCancelled {
		t.Errorf("returned status = %q, want cancelled", before.Status)
	}

	if _, err := s.CancelTask(ctx, 9999); !errors.Is(err, store.ErrTaskNotFound) {
		t.Errorf("missing cancel err = %v, want ErrTaskNotFound", err)
	}

	running := createTask(t, s, "running", time.Now().Add(-time.Minute))
	if _, err := s.ClaimDueTasks(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err = s.CancelTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("cancel running: %v", err)
	}
	if before.Status != store.TaskRunning {
		t.Errorf("returned status = %q, want running", before.Status)
	}

	done := createTask(t, s, "done", time.Now().Add(-time.Minute))
	if _, err := s.ClaimDueTasks(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTask(ctx, done.ID, store.TaskDone, "r"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelTask(ctx, done.ID); !errors.Is(err, store.ErrTaskFinished) {
		t.Errorf("cancel done err = %v, want ErrTaskFinished", err)
	}
	if got := taskByID(t, s, done.ID); got.Status != store.TaskDone {
		t.Errorf("done task status = %q after failed cancel", got.Status)
	}
}

func TestTaskInterruptRunning(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	running := createTask(t, s, "running", time.Now().Add(-time.Minute))
	pending := createTask(t, s, "pending", time.Now().Add(time.Hour))
	if _, err := s.ClaimDueTasks(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}

	got, err := s.InterruptRunningTasks(ctx)
	if err != nil {
		t.Fatalf("InterruptRunningTasks: %v", err)
	}
	if len(got) != 1 || got[0].ID != running.ID {
		t.Fatalf("interrupted = %+v, want only running task", got)
	}
	if got[0].Status != store.TaskInterrupted || got[0].FinishedAt == nil {
		t.Errorf("returned row: %+v", got[0])
	}
	if row := taskByID(t, s, running.ID); row.Status != store.TaskInterrupted {
		t.Errorf("stored status = %q", row.Status)
	}
	if row := taskByID(t, s, pending.ID); row.Status != store.TaskPending {
		t.Errorf("pending status = %q", row.Status)
	}

	again, err := s.InterruptRunningTasks(ctx)
	if err != nil || len(again) != 0 {
		t.Errorf("second interrupt = %v, %v", again, err)
	}
}

func TestTaskListRecentFinished(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	base := time.Now().Add(-24 * time.Hour)

	createTask(t, s, "pending-late", base.Add(3*time.Hour))
	createTask(t, s, "pending-early", base.Add(time.Hour))
	runningTask := createTask(t, s, "running", base.Add(2*time.Hour))
	pgtest.Exec(t, "UPDATE tasks SET status = 'running' WHERE id = ?", runningTask.ID)

	for i, name := range []string{"fin-oldest", "fin-mid", "fin-newest"} {
		task := createTask(t, s, name, base)
		pgtest.Exec(t, "UPDATE tasks SET status = 'done', finished_at = ? WHERE id = ?", base.Add(time.Duration(i+1)*time.Minute), task.ID)
	}
	nullFinished := createTask(t, s, "fin-null", base)
	pgtest.Exec(t, "UPDATE tasks SET status = 'failed' WHERE id = ?", nullFinished.ID)

	tests := []struct {
		name   string
		recent int
		want   []string
	}{
		{"zero", 0, []string{"pending-early", "running", "pending-late"}},
		{"two", 2, []string{"pending-early", "running", "pending-late", "fin-newest", "fin-mid"}},
		{"all", 10, []string{"pending-early", "running", "pending-late", "fin-newest", "fin-mid", "fin-oldest", "fin-null"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ListTasks(ctx, tt.recent)
			if err != nil {
				t.Fatalf("ListTasks: %v", err)
			}
			if !slices.Equal(prompts(got), tt.want) {
				t.Errorf("got %v, want %v", prompts(got), tt.want)
			}
		})
	}
}
