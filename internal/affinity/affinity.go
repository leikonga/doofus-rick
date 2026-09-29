package affinity

import (
	"context"
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

func (a *Ledger) Get(ctx context.Context, userID uint64) (*Result, error) {
	affinity, err := a.store.GetAffinity(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &Result{
		UserID: affinity.UserID,
		Score:  affinity.Score,
		LastReason: func() string {
			if affinity.LastReason != nil {
				return *affinity.LastReason
			}
			return ""
		}(),
	}, nil
}

func (a *Ledger) Update(ctx context.Context, userID uint64, reason string, delta int) error {
	affinity, err := a.store.GetAffinity(ctx, userID)
	if err != nil {
		affinity = &store.UserAffinity{
			UserID:     userID,
			Score:      a.config.Baseline,
			LastReason: &[]string{reason}[0],
			UpdatedAt:  time.Now(),
		}
	}

	affinity.Score += delta
	affinity.Score = clamp(affinity.Score, -100, 100)
	affinity.LastReason = &[]string{reason}[0]
	affinity.UpdatedAt = time.Now()

	return a.store.UpdateAffinity(ctx, affinity)
}

func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
