package archive

import (
	"context"
	"time"

	"github.com/leikonga/doofus-rick/internal/store"
)

type AffinityConfig struct {
	Enabled     bool
	Baseline    int
	DecayPerDay float64
	Model       string
}

type Affinity struct {
	config AffinityConfig
	store  *store.Store
}

func NewAffinity(config AffinityConfig, s *store.Store) *Affinity {
	if config.Baseline == 0 {
		config.Baseline = -20
	}
	if config.DecayPerDay == 0 {
		config.DecayPerDay = 0.10
	}
	return &Affinity{config: config, store: s}
}

type AffinityResult struct {
	UserID     uint64
	Score      int
	LastReason string
}

func (a *Affinity) Get(ctx context.Context, userID uint64) (*AffinityResult, error) {
	affinity, err := a.store.GetAffinity(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &AffinityResult{
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

func (a *Affinity) Update(ctx context.Context, userID uint64, reason string, delta int) error {
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

func (a *Affinity) Decay(ctx context.Context, userID uint64, days float64) error {
	affinity, err := a.store.GetAffinity(ctx, userID)
	if err != nil {
		return err
	}

	decay := int(float64(a.config.Baseline-affinity.Score) * a.config.DecayPerDay * days)
	affinity.Score += decay
	affinity.Score = clamp(affinity.Score, -100, 100)
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
