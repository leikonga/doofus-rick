package archive

import (
	"context"
	"time"

	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/store"
)

type BudgetGuard struct {
	config *config.Config
	store  *store.Store
}

func NewBudgetGuard(c *config.Config, s *store.Store) *BudgetGuard {
	return &BudgetGuard{config: c, store: s}
}

func (b *BudgetGuard) Check(ctx context.Context) (bool, error) {
	if b.config.BudgetMonthlyUSD <= 0 {
		return true, nil
	}

	used, err := b.GetSpent(ctx)
	if err != nil {
		return false, err
	}

	percent := used / b.config.BudgetMonthlyUSD
	return percent < 1.0, nil
}

func (b *BudgetGuard) ShouldDisableAmbient(ctx context.Context) (bool, error) {
	if b.config.BudgetMonthlyUSD <= 0 {
		return false, nil
	}

	used, err := b.GetSpent(ctx)
	if err != nil {
		return false, err
	}

	percent := used / b.config.BudgetMonthlyUSD
	return percent >= 0.80, nil
}

// GetSpent totals estimated spend since the start of the current calendar
// month, so BUDGET_MONTHLY_USD is checked against this month's usage, not
// all-time usage.
func (b *BudgetGuard) GetSpent(ctx context.Context) (float64, error) {
	var total float64

	var usage []struct {
		InputTokens  int64  `gorm:"column:input_tokens"`
		OutputTokens int64  `gorm:"column:output_tokens"`
		ModelName    string `gorm:"column:model_name"`
	}

	err := b.store.DB().WithContext(ctx).Model(&store.TokenUsage{}).
		Where("created_at >= ?", startOfMonth(time.Now())).
		Find(&usage).Error
	if err != nil {
		return 0, err
	}

	for _, u := range usage {
		cost := b.estimateCost(u.ModelName, u.InputTokens, u.OutputTokens)
		total += cost
	}

	return total, nil
}

func startOfMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, t.Location())
}

func (b *BudgetGuard) estimateCost(model string, input, output int64) float64 {
	switch model {
	case "anthropic/claude-sonnet-5":
		return float64(input)*0.000003 + float64(output)*0.000015
	case "qwen/qwen3-embedding-8b":
		return float64(input) * 0.0000000625
	default:
		return float64(input)*0.0000005 + float64(output)*0.000001
	}
}
