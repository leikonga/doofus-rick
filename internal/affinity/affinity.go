package affinity

import (
	"context"
	"errors"
	"time"

	"github.com/leikonga/doofus-rick/internal/store"
)

type Config struct {
	Baseline int
}

type Ledger struct {
	config Config
	store  *store.Store
}

func New(config Config, s *store.Store) *Ledger {
	if config.Baseline == 0 {
		config.Baseline = -20
	}
	return &Ledger{config: config, store: s}
}

type Result struct {
	UserID     uint64
	Score      int
	LastReason string
}

func (l *Ledger) Get(ctx context.Context, userID uint64) (*Result, error) {
	row, err := l.store.GetAffinity(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &Result{
		UserID: row.UserID,
		Score:  row.Score,
		LastReason: func() string {
			if row.LastReason != nil {
				return *row.LastReason
			}
			return ""
		}(),
	}, nil
}

func (l *Ledger) Update(ctx context.Context, userID uint64, reason string, delta int) error {
	row, err := l.store.GetAffinity(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		row = &store.UserAffinity{UserID: userID, Score: l.config.Baseline}
	} else if err != nil {
		return err
	}

	row.Score += delta
	row.Score = clamp(row.Score, -100, 100)
	row.LastReason = new(reason)
	row.UpdatedAt = time.Now()

	return l.store.UpdateAffinity(ctx, row)
}

func clamp(val, lo, hi int) int {
	return min(max(val, lo), hi)
}
