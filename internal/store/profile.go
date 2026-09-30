package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm/clause"
)

type ProfileCandidate struct {
	AuthorID  uint64 `gorm:"column:author_id"`
	Watermark uint64 `gorm:"column:watermark"`
}

func (s *Store) GetPersonProfile(ctx context.Context, userID uint64) (*PersonProfile, error) {
	var p PersonProfile
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&p).Error; err != nil {
		return nil, fmt.Errorf("get person profile %d: %w", userID, mapNotFound(err))
	}
	return &p, nil
}

func (s *Store) SavePersonProfile(ctx context.Context, p PersonProfile) error {
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now()
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"summary", "watermark_message_id", "updated_at"}),
	}).Create(&p).Error
	if err != nil {
		return fmt.Errorf("save person profile %d: %w", p.UserID, err)
	}
	return nil
}

func (s *Store) DeletePersonProfile(ctx context.Context, userID uint64) error {
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&PersonProfile{}).Error; err != nil {
		return fmt.Errorf("delete person profile %d: %w", userID, err)
	}
	return nil
}

// GetProfileCandidates counts only messages in channelIDs, most active authors first.
func (s *Store) GetProfileCandidates(ctx context.Context, channelIDs []uint64, minNew, limit int) ([]ProfileCandidate, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	var out []ProfileCandidate
	err := s.db.WithContext(ctx).Raw(`
		SELECT m.author_id AS author_id, COALESCE(p.watermark_message_id, 0) AS watermark
		FROM messages m
		LEFT JOIN person_profiles p ON p.user_id = m.author_id
		WHERE m.is_bot = false
		  AND m.channel_id IN ?
		  AND length(m.content) >= 3 AND m.content NOT LIKE '/%'
		  AND m.id > COALESCE(p.watermark_message_id, 0)
		  AND NOT EXISTS (SELECT 1 FROM forgotten_authors f WHERE f.user_id = m.author_id)
		GROUP BY m.author_id, p.watermark_message_id
		HAVING COUNT(*) >= ?
		ORDER BY COUNT(*) DESC, m.author_id
		LIMIT ?
	`, channelIDs, minNew, limit).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("get profile candidates: %w", err)
	}
	return out, nil
}

func (s *Store) GetAuthorMessagesAfter(ctx context.Context, authorID, afterID uint64, channelIDs []uint64, limit int) ([]Message, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	var msgs []Message
	err := s.db.WithContext(ctx).
		Where("author_id = ? AND id > ? AND is_bot = false AND channel_id IN ?", authorID, afterID, channelIDs).
		Where("length(content) >= 3 AND content NOT LIKE '/%'").
		Order("id").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, fmt.Errorf("get messages of author %d after %d: %w", authorID, afterID, err)
	}
	return msgs, nil
}
