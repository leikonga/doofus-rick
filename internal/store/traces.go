package store

import (
	"context"
)

func (s *Store) SaveTokenUsage(ctx context.Context, u TokenUsage) error {
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Create(&u).Error
}

func (s *Store) SaveFailureTrace(ctx context.Context, t FailureTrace) error {
	return s.db.WithContext(ctx).Create(&t).Error
}

func (s *Store) GetFailureTraces(ctx context.Context, limit int) ([]FailureTrace, error) {
	var traces []FailureTrace
	err := s.db.WithContext(ctx).Order("created_at desc").Limit(limit).Find(&traces).Error
	return traces, err
}

func (s *Store) GetFailureTraceByTraceID(ctx context.Context, id string) (FailureTrace, error) {
	var ft FailureTrace
	err := s.db.WithContext(ctx).Where("trace_id = ?", id).First(&ft).Error
	return ft, mapNotFound(err)
}
