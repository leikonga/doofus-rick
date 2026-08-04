package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// GetOrCreateBackfillState returns the singleton backfill state row,
// creating it as idle if this is the first time backfill has ever run.
func (s *Store) GetOrCreateBackfillState(ctx context.Context) (*BackfillState, error) {
	state, err := s.GetBackfillState(ctx)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	state = &BackfillState{ID: 1, Status: "idle", UpdatedAt: time.Now()}
	if err := s.db.WithContext(ctx).Create(state).Error; err != nil {
		return nil, err
	}
	return state, nil
}

// SeedBackfillChannels inserts a pending row per channel ID not already
// tracked, so newly enabled backfill picks up every channel in the guild.
// Existing rows (including completed ones) are left untouched.
func (s *Store) SeedBackfillChannels(ctx context.Context, channelIDs []uint64) (int, error) {
	if len(channelIDs) == 0 {
		return 0, nil
	}

	rows := make([]BackfillChannel, len(channelIDs))
	for i, id := range channelIDs {
		rows[i] = BackfillChannel{ChannelID: id, UpdatedAt: time.Now()}
	}

	tx := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows)
	if tx.Error != nil {
		return 0, tx.Error
	}
	return int(tx.RowsAffected), nil
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

// GetLastChunkedMessageID returns the highest message ID already covered by
// a chunk for the channel, or 0 if none exist, so incremental chunking can
// resume without rescanning already-chunked messages.
func (s *Store) GetLastChunkedMessageID(ctx context.Context, channelID uint64) (uint64, error) {
	var lastID uint64
	err := s.db.WithContext(ctx).Model(&Chunk{}).
		Where("channel_id = ?", channelID).
		Select("COALESCE(MAX(last_message_id), 0)").
		Scan(&lastID).Error
	return lastID, err
}

// GetRecentMessagesSince returns human (non-bot) messages in a channel
// created after the given time, oldest first.
func (s *Store) GetRecentMessagesSince(ctx context.Context, channelID uint64, since time.Time, limit int) ([]Message, error) {
	var msgs []Message
	err := s.db.WithContext(ctx).
		Where("channel_id = ? AND created_at > ?", channelID, since).
		Order("id asc").Limit(limit).Find(&msgs).Error
	return msgs, err
}

// ActiveAuthor is one row of the archive-activity leaderboard used to build
// the roster: an author's most recent display name and how much they've
// posted in the lookback window.
type ActiveAuthor struct {
	AuthorID   uint64 `gorm:"column:author_id"`
	AuthorName string `gorm:"column:author_name"`
	MsgCount   int64  `gorm:"column:msg_count"`
}

// GetActiveAuthors ranks non-bot authors by message volume since the given
// time, most active first, using each author's most recent display name.
func (s *Store) GetActiveAuthors(ctx context.Context, since time.Time, limit int) ([]ActiveAuthor, error) {
	var authors []ActiveAuthor
	err := s.db.WithContext(ctx).Raw(`
		SELECT m.author_id AS author_id,
		       (SELECT m2.author_name FROM messages m2
		        WHERE m2.author_id = m.author_id
		        ORDER BY m2.created_at DESC LIMIT 1) AS author_name,
		       COUNT(*) AS msg_count
		FROM messages m
		WHERE m.created_at > ? AND m.is_bot = ?
		GROUP BY m.author_id
		ORDER BY msg_count DESC
		LIMIT ?
	`, since, false, limit).Scan(&authors).Error
	return authors, err
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
