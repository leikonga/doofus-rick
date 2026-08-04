package store

import (
	"context"
	"time"

	"gorm.io/gorm"
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

func (s *Store) GetBackfillState(ctx context.Context) (*BackfillState, error) {
	var state BackfillState
	err := s.db.WithContext(ctx).Where("id = 1").First(&state).Error
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *Store) UpdateBackfillState(ctx context.Context, state *BackfillState) error {
	return s.db.WithContext(ctx).Save(state).Error
}

func (s *Store) GetBackfillChannels(ctx context.Context, limit int) ([]BackfillChannel, error) {
	var channels []BackfillChannel
	err := s.db.WithContext(ctx).Where("done = false").Order("oldest_fetched").Limit(limit).Find(&channels).Error
	return channels, err
}

func (s *Store) GetBackfillChannel(ctx context.Context, channelID uint64) (*BackfillChannel, error) {
	var channel BackfillChannel
	err := s.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&channel).Error
	return &channel, err
}

func (s *Store) SaveBackfillChannel(ctx context.Context, channel *BackfillChannel) error {
	return s.db.WithContext(ctx).Save(channel).Error
}

func (s *Store) GetOldestFetchedMessage(ctx context.Context, channelID uint64) (*Message, error) {
	var msg Message
	err := s.db.WithContext(ctx).Where("channel_id = ?", channelID).Order("id desc").First(&msg).Error
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *Store) GetMessagesBefore(ctx context.Context, channelID uint64, beforeID uint64, limit int) ([]Message, error) {
	var msgs []Message
	err := s.db.WithContext(ctx).Where("channel_id = ? AND id < ?", channelID, beforeID).Order("id desc").Limit(limit).Find(&msgs).Error
	return msgs, err
}

func (s *Store) GetMessagesSince(ctx context.Context, channelID uint64, sinceID uint64, limit int) ([]Message, error) {
	var msgs []Message
	err := s.db.WithContext(ctx).Where("channel_id = ? AND id > ?", channelID, sinceID).Order("id asc").Limit(limit).Find(&msgs).Error
	return msgs, err
}

func (s *Store) CreateChunk(ctx context.Context, chunk Chunk) error {
	return s.db.WithContext(ctx).Create(&chunk).Error
}

func (s *Store) SaveChunkEmbedding(ctx context.Context, embedding ChunkEmbedding) error {
	return s.db.WithContext(ctx).Create(&embedding).Error
}

func (s *Store) GetChunksWithoutEmbedding(ctx context.Context, model string, limit int) ([]Chunk, error) {
	var chunks []Chunk
	err := s.db.WithContext(ctx).
		Where("id NOT IN (SELECT chunk_id FROM chunk_embeddings WHERE model = ?)", model).
		Order("id").Limit(limit).Find(&chunks).Error
	return chunks, err
}

func (s *Store) GetChunk(ctx context.Context, id uint64) (*Chunk, error) {
	var chunk Chunk
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&chunk).Error
	return &chunk, err
}

func (s *Store) GetUnchunkedMessages(ctx context.Context, channelID uint64, sinceID uint64, limit int) ([]Message, error) {
	var msgs []Message
	err := s.db.WithContext(ctx).
		Where("channel_id = ? AND id > ? AND id NOT IN (SELECT last_message_id FROM chunks WHERE channel_id = ?)", channelID, sinceID, channelID).
		Order("id").Limit(limit).Find(&msgs).Error
	return msgs, err
}

func (s *Store) GetAffinity(ctx context.Context, userID uint64) (*UserAffinity, error) {
	var affinity UserAffinity
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&affinity).Error
	if err != nil {
		return nil, err
	}
	return &affinity, nil
}

func (s *Store) UpdateAffinity(ctx context.Context, affinity *UserAffinity) error {
	return s.db.WithContext(ctx).Save(affinity).Error
}

func (s *Store) GetAmbientLog(ctx context.Context, channelID uint64, limit int) ([]AmbientLog, error) {
	var logs []AmbientLog
	err := s.db.WithContext(ctx).Where("channel_id = ?", channelID).Order("fired_at desc").Limit(limit).Find(&logs).Error
	return logs, err
}

func (s *Store) LogAmbientFire(ctx context.Context, log AmbientLog) error {
	return s.db.WithContext(ctx).Create(&log).Error
}

func (s *Store) GetAmbientState(ctx context.Context, channelID uint64) (*AmbientState, error) {
	var state AmbientState
	err := s.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&state).Error
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *Store) UpdateAmbientState(ctx context.Context, state *AmbientState) error {
	return s.db.WithContext(ctx).Save(state).Error
}

func (s *Store) IncrementAmbientFiresToday(ctx context.Context, channelID uint64) error {
	return s.db.WithContext(ctx).Model(&AmbientState{}).
		Where("channel_id = ?", channelID).
		UpdateColumn("fires_today", gorm.Expr("fires_today + 1")).Error
}

func (s *Store) ResetAmbientFiresToday(ctx context.Context, channelID uint64) error {
	return s.db.WithContext(ctx).Model(&AmbientState{}).
		Where("channel_id = ?", channelID).
		UpdateColumn("fires_today", 0).Error
}
