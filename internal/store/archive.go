package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (s *Store) CreateMessage(ctx context.Context, msg Message) error {
	if err := s.db.WithContext(ctx).Create(&msg).Error; err != nil {
		return fmt.Errorf("create message %d: %w", msg.ID, err)
	}
	return nil
}

func (s *Store) IsAuthorForgotten(ctx context.Context, authorID uint64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&ForgottenAuthor{}).Where("user_id = ?", authorID).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check author %d forgotten: %w", authorID, err)
	}
	return count > 0, nil
}

func (s *Store) ForgetAuthor(ctx context.Context, authorID uint64) error {
	err := s.db.WithContext(ctx).Create(&ForgottenAuthor{
		UserID:    authorID,
		CreatedAt: time.Now(),
	}).Error
	if err != nil {
		return fmt.Errorf("forget author %d: %w", authorID, err)
	}
	return nil
}

func (s *Store) DeleteMessage(ctx context.Context, id uint64) error {
	if err := s.db.WithContext(ctx).Delete(&Message{}, id).Error; err != nil {
		return fmt.Errorf("delete message %d: %w", id, err)
	}
	return nil
}

func (s *Store) DeleteQuotesByAuthor(ctx context.Context, authorID string) error {
	return s.db.WithContext(ctx).Where("creator = ? OR participants LIKE ?", authorID, "%"+authorID+"%").Delete(&Quote{}).Error
}

func (s *Store) GetBackfillState(ctx context.Context) (*BackfillState, error) {
	var state BackfillState
	err := s.db.WithContext(ctx).Where("id = 1").First(&state).Error
	if err != nil {
		return nil, fmt.Errorf("get backfill state: %w", mapNotFound(err))
	}
	return &state, nil
}

func (s *Store) GetOrCreateBackfillState(ctx context.Context) (*BackfillState, error) {
	state, err := s.GetBackfillState(ctx)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	state = &BackfillState{ID: 1, Status: "idle", UpdatedAt: time.Now()}
	if err := s.db.WithContext(ctx).Create(state).Error; err != nil {
		return nil, fmt.Errorf("create backfill state: %w", err)
	}
	return state, nil
}

// Existing rows (including completed ones) are left untouched.
func (s *Store) SeedBackfillChannels(ctx context.Context, channelIDs []uint64) (int, error) {
	if len(channelIDs) == 0 {
		return 0, nil
	}

	values := strings.TrimSuffix(strings.Repeat("(?, now()),", len(channelIDs)), ",")
	args := make([]any, len(channelIDs))
	for i, id := range channelIDs {
		args[i] = id
	}

	tx := s.db.WithContext(ctx).Exec(
		"insert into backfill_channels (channel_id, updated_at) values "+values+" on conflict (channel_id) do nothing", args...)
	if tx.Error != nil {
		return 0, fmt.Errorf("seed backfill channels: %w", tx.Error)
	}
	return int(tx.RowsAffected), nil
}

func (s *Store) UpdateBackfillState(ctx context.Context, state *BackfillState) error {
	if err := s.db.WithContext(ctx).Save(state).Error; err != nil {
		return fmt.Errorf("update backfill state: %w", err)
	}
	return nil
}

func (s *Store) GetBackfillChannels(ctx context.Context, limit int) ([]BackfillChannel, error) {
	var channels []BackfillChannel
	err := s.db.WithContext(ctx).Where("done = false").Order("oldest_fetched").Limit(limit).Find(&channels).Error
	return channels, err
}

func (s *Store) GetBackfillChannel(ctx context.Context, channelID uint64) (*BackfillChannel, error) {
	var channel BackfillChannel
	err := s.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&channel).Error
	if err != nil {
		return &channel, fmt.Errorf("get backfill channel %d: %w", channelID, mapNotFound(err))
	}
	return &channel, nil
}

func (s *Store) SaveBackfillChannel(ctx context.Context, channel *BackfillChannel) error {
	if err := s.db.WithContext(ctx).Save(channel).Error; err != nil {
		return fmt.Errorf("save backfill channel %d: %w", channel.ChannelID, err)
	}
	return nil
}

func (s *Store) CreateChunk(ctx context.Context, chunk Chunk) error {
	if err := s.db.WithContext(ctx).Create(&chunk).Error; err != nil {
		return fmt.Errorf("create chunk: %w", err)
	}
	return nil
}

func (s *Store) SaveChunkEmbedding(ctx context.Context, embedding ChunkEmbedding) error {
	if err := s.db.WithContext(ctx).Create(&embedding).Error; err != nil {
		return fmt.Errorf("save chunk embedding: %w", err)
	}
	return nil
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
	if err != nil {
		return &chunk, fmt.Errorf("get chunk %d: %w", id, mapNotFound(err))
	}
	return &chunk, nil
}

// GetNeighborChunks returns chunks in chronological order.
func (s *Store) GetNeighborChunks(ctx context.Context, channelID, chunkID uint64, before, after int) ([]Chunk, error) {
	var prev, next []Chunk
	if before > 0 {
		if err := s.db.WithContext(ctx).Where("channel_id = ? AND id < ?", channelID, chunkID).
			Order("id desc").Limit(before).Find(&prev).Error; err != nil {
			return nil, fmt.Errorf("get chunks before %d: %w", chunkID, err)
		}
		slices.Reverse(prev)
	}
	if after > 0 {
		if err := s.db.WithContext(ctx).Where("channel_id = ? AND id > ?", channelID, chunkID).
			Order("id asc").Limit(after).Find(&next).Error; err != nil {
			return nil, fmt.Errorf("get chunks after %d: %w", chunkID, err)
		}
	}
	return append(prev, next...), nil
}

func (s *Store) GetChannelsWithUnchunkedMessages(ctx context.Context, limit int) ([]uint64, error) {
	var ids []uint64
	err := s.db.WithContext(ctx).Raw(`
		select m.channel_id
		from (select channel_id, max(id) as max_id from messages group by channel_id) m
		left join (select channel_id, max(last_message_id) as max_chunked from chunks group by channel_id) c
			on c.channel_id = m.channel_id
		where m.max_id > coalesce(c.max_chunked, 0)
		limit ?
	`, limit).Scan(&ids).Error
	return ids, err
}

func (s *Store) GetUnchunkedMessages(ctx context.Context, channelID uint64, limit int) ([]Message, error) {
	var msgs []Message
	err := s.db.WithContext(ctx).
		Where("channel_id = ? AND id > (SELECT COALESCE(MAX(last_message_id), 0) FROM chunks WHERE channel_id = ?)", channelID, channelID).
		Order("id").Limit(limit).Find(&msgs).Error
	return msgs, err
}

// GetRecentMessagesSince includes bot messages; ambient.Gate relies on them to detect Rick having spoken.
func (s *Store) GetRecentMessagesSince(ctx context.Context, channelID uint64, since time.Time, limit int) ([]Message, error) {
	var msgs []Message
	err := s.db.WithContext(ctx).
		Where("channel_id = ? AND created_at > ?", channelID, since).
		Order("id asc").Limit(limit).Find(&msgs).Error
	return msgs, err
}

type ActiveAuthor struct {
	AuthorID   uint64 `gorm:"column:author_id"`
	AuthorName string `gorm:"column:author_name"`
	MsgCount   int64  `gorm:"column:msg_count"`
}

// GetActiveAuthors excludes bots, ranks most active first and uses each author's latest display name.
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
		return nil, fmt.Errorf("get affinity %d: %w", userID, mapNotFound(err))
	}
	return &affinity, nil
}

func (s *Store) UpdateAffinity(ctx context.Context, affinity *UserAffinity) error {
	if err := s.db.WithContext(ctx).Save(affinity).Error; err != nil {
		return fmt.Errorf("update affinity %d: %w", affinity.UserID, err)
	}
	return nil
}

func (s *Store) LogAmbientFire(ctx context.Context, log AmbientLog) error {
	if err := s.db.WithContext(ctx).Create(&log).Error; err != nil {
		return fmt.Errorf("log ambient fire: %w", err)
	}
	return nil
}

func (s *Store) GetAmbientState(ctx context.Context, channelID uint64) (*AmbientState, error) {
	var state AmbientState
	err := s.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&state).Error
	if err != nil {
		return nil, fmt.Errorf("get ambient state %d: %w", channelID, mapNotFound(err))
	}
	return &state, nil
}

func (s *Store) UpdateAmbientState(ctx context.Context, state *AmbientState) error {
	if err := s.db.WithContext(ctx).Save(state).Error; err != nil {
		return fmt.Errorf("update ambient state %d: %w", state.ChannelID, err)
	}
	return nil
}

func (s *Store) IncrementAmbientFiresToday(ctx context.Context, channelID uint64) error {
	return s.db.WithContext(ctx).Model(&AmbientState{}).
		Where("channel_id = ?", channelID).
		UpdateColumn("fires_today", gorm.Expr("fires_today + 1")).Error
}

// FindAuthorsByName matches any name an author has used, case-insensitively, and returns each author's latest name.
func (s *Store) FindAuthorsByName(ctx context.Context, name string) ([]ActiveAuthor, error) {
	var authors []ActiveAuthor
	err := s.db.WithContext(ctx).Raw(`
		SELECT m.author_id AS author_id,
		       (SELECT m2.author_name FROM messages m2
		        WHERE m2.author_id = m.author_id
		        ORDER BY m2.created_at DESC LIMIT 1) AS author_name,
		       COUNT(*) AS msg_count
		FROM messages m
		WHERE lower(m.author_name) = lower(?) AND m.is_bot = ?
		GROUP BY m.author_id
		ORDER BY msg_count DESC
		LIMIT 10
	`, name, false).Scan(&authors).Error
	if err != nil {
		return nil, fmt.Errorf("find authors by name: %w", err)
	}
	return authors, nil
}
