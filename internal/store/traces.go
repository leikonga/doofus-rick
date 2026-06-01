package store

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/leikonga/doofus-rick/internal/tracer"
)

func (s *Store) SaveTokenUsage(ctx context.Context, channelID, userID, model string, input, output int64) {
	if input == 0 && output == 0 {
		return
	}
	if err := s.db.WithContext(ctx).Create(&TokenUsage{
		ChannelID:    channelID,
		UserID:       userID,
		ModelName:    model,
		InputTokens:  input,
		OutputTokens: output,
	}).Error; err != nil {
		slog.Warn("failed to save token usage", "error", err)
	}
}

func (s *Store) SaveFailureTrace(ctx context.Context, e *tracer.Entry) {
	blob, err := json.Marshal(e)
	if err != nil {
		slog.Warn("failed to marshal failure trace", "error", err)
		return
	}
	if err := s.db.WithContext(ctx).Create(&FailureTrace{
		TraceID:   e.ID,
		ChannelID: e.ChannelID,
		UserID:    e.UserID,
		Blob:      string(blob),
		Decline:   e.Decline,
		ErrMsg:    e.Err,
	}).Error; err != nil {
		slog.Warn("failed to save failure trace", "error", err)
	}
}

func (s *Store) GetFailureTraces(ctx context.Context, limit int) ([]FailureTrace, error) {
	var traces []FailureTrace
	err := s.db.WithContext(ctx).Order("created_at desc").Limit(limit).Find(&traces).Error
	return traces, err
}

func (s *Store) GetFailureTraceByTraceID(ctx context.Context, id string) (FailureTrace, error) {
	var ft FailureTrace
	err := s.db.WithContext(ctx).Where("trace_id = ?", id).First(&ft).Error
	return ft, err
}

type TokenLeaderboardEntry struct {
	UserID       string
	InputTokens  int64
	OutputTokens int64
}

func (s *Store) GetTokenLeaderboard(ctx context.Context) ([]TokenLeaderboardEntry, error) {
	var results []TokenLeaderboardEntry
	err := s.db.WithContext(ctx).Model(&TokenUsage{}).
		Select("user_id, sum(input_tokens) as input_tokens, sum(output_tokens) as output_tokens").
		Group("user_id").
		Order("input_tokens + output_tokens desc").
		Scan(&results).Error
	return results, err
}
