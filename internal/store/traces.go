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
