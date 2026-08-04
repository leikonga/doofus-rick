package store

import (
	"context"
	"time"
)

func (s *Store) CreateMessage(ctx context.Context, msg Message) error {
	return s.db.WithContext(ctx).Create(&msg).Error
}

func (s *Store) IsAuthorForgotten(ctx context.Context, authorID uint64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&ForgottenAuthor{}).Where("user_id = ?", authorID).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) ForgetAuthor(ctx context.Context, authorID uint64) error {
	return s.db.WithContext(ctx).Create(&ForgottenAuthor{
		UserID:    authorID,
		CreatedAt: time.Now(),
	}).Error
}

func (s *Store) DeleteMessage(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&Message{}, id).Error
}

func (s *Store) DeleteMessagesByAuthor(ctx context.Context, authorID uint64) error {
	return s.db.WithContext(ctx).Where("author_id = ?", authorID).Delete(&Message{}).Error
}

func (s *Store) DeleteQuotesByAuthor(ctx context.Context, authorID string) error {
	return s.db.WithContext(ctx).Where("creator = ? OR participants LIKE ?", authorID, "%"+authorID+"%").Delete(&Quote{}).Error
}
