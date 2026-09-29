package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type TaskStatus string

const (
	TaskPending     TaskStatus = "pending"
	TaskRunning     TaskStatus = "running"
	TaskDone        TaskStatus = "done"
	TaskFailed      TaskStatus = "failed"
	TaskCancelled   TaskStatus = "cancelled"
	TaskInterrupted TaskStatus = "interrupted"
)

func (s TaskStatus) Active() bool {
	return s == TaskPending || s == TaskRunning
}

type Task struct {
	ID          uint64 `gorm:"primaryKey"`
	ChannelID   uint64
	RequesterID uint64
	Prompt      string
	FireAt      time.Time
	Status      TaskStatus
	Result      string
	CreatedAt   time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
}

var (
	ErrTaskNotFound = errors.New("task not found")
	ErrTaskFinished = errors.New("task already finished")
)

func (s *Store) CreateTask(ctx context.Context, t Task) (Task, error) {
	t.Status = TaskPending
	err := s.db.WithContext(ctx).Create(&t).Error
	return t, err
}

func (s *Store) ListTasks(ctx context.Context, recentFinished int) ([]Task, error) {
	active := []TaskStatus{TaskPending, TaskRunning}
	var tasks []Task
	if err := s.db.WithContext(ctx).Where("status IN ?", active).Order("fire_at, id").Find(&tasks).Error; err != nil {
		return nil, err
	}
	var finished []Task
	err := s.db.WithContext(ctx).Where("status NOT IN ?", active).
		Order("finished_at DESC NULLS LAST, id DESC").Limit(recentFinished).Find(&finished).Error
	if err != nil {
		return nil, err
	}
	return append(tasks, finished...), nil
}

// CancelTask marks a pending or running task cancelled and returns it as it was before.
func (s *Store) CancelTask(ctx context.Context, id uint64) (Task, error) {
	var before Task
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT * FROM tasks WHERE id = ? FOR UPDATE`, id).Scan(&before).Error; err != nil {
			return err
		}
		if before.ID == 0 {
			return ErrTaskNotFound
		}
		if !before.Status.Active() {
			return fmt.Errorf("%w: status %s", ErrTaskFinished, before.Status)
		}
		return tx.Exec(`UPDATE tasks SET status = ?, finished_at = now() WHERE id = ?`, TaskCancelled, id).Error
	})
	return before, err
}

func (s *Store) ClaimDueTasks(ctx context.Context, now time.Time) ([]Task, error) {
	var tasks []Task
	err := s.db.WithContext(ctx).Raw(`
		UPDATE tasks SET status = ?, started_at = now()
		WHERE id IN (
			SELECT id FROM tasks WHERE status = ? AND fire_at <= ?
			ORDER BY fire_at FOR UPDATE SKIP LOCKED
		)
		RETURNING *`, TaskRunning, TaskPending, now).Scan(&tasks).Error
	return tasks, err
}

// FinishTask only touches running tasks, so a cancellation that raced the turn stays cancelled.
func (s *Store) FinishTask(ctx context.Context, id uint64, status TaskStatus, result string) error {
	return s.db.WithContext(ctx).Exec(`UPDATE tasks SET status = ?, result = ?, finished_at = now() WHERE id = ? AND status = ?`,
		status, result, id, TaskRunning).Error
}

func (s *Store) InterruptRunningTasks(ctx context.Context) ([]Task, error) {
	var tasks []Task
	err := s.db.WithContext(ctx).Raw(`
		UPDATE tasks SET status = ?, finished_at = now()
		WHERE status = ?
		RETURNING *`, TaskInterrupted, TaskRunning).Scan(&tasks).Error
	return tasks, err
}
