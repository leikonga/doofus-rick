package store

import (
	"context"
	"time"
)

func (s *Store) CreateReminder(ctx context.Context, r Reminder) error {
	return s.db.WithContext(ctx).Create(&r).Error
}

func (s *Store) GetDueReminders(ctx context.Context) ([]Reminder, error) {
	var reminders []Reminder
	err := s.db.WithContext(ctx).Where("fired = false AND fire_at <= ?", time.Now()).Find(&reminders).Error
	return reminders, err
}

func (s *Store) MarkReminderFired(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Model(&Reminder{}).Where("id = ?", id).Update("fired", true).Error
}
