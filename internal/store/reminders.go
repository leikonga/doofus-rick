package store

import "time"

func (s *Store) CreateReminder(r Reminder) error {
	return s.db.Create(&r).Error
}

func (s *Store) GetDueReminders() ([]Reminder, error) {
	var reminders []Reminder
	err := s.db.Where("fired = false AND fire_at <= ?", time.Now()).Find(&reminders).Error
	return reminders, err
}

func (s *Store) MarkReminderFired(id uint) error {
	return s.db.Model(&Reminder{}).Where("id = ?", id).Update("fired", true).Error
}
