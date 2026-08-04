package archive

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/config"
)

func TestStartOfMonth(t *testing.T) {
	got := startOfMonth(time.Date(2026, 3, 17, 14, 30, 0, 0, time.UTC))
	want := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("startOfMonth() = %v, want %v", got, want)
	}
}

func TestBudgetGuard_UnlimitedWhenNotConfigured(t *testing.T) {
	b := &BudgetGuard{config: &config.Config{BudgetMonthlyUSD: 0}}

	ok, err := b.Check(context.Background())
	if err != nil || !ok {
		t.Fatalf("Check() = (%v, %v), want (true, nil) when unconfigured", ok, err)
	}

	disable, err := b.ShouldDisableAmbient(context.Background())
	if err != nil || disable {
		t.Fatalf("ShouldDisableAmbient() = (%v, %v), want (false, nil) when unconfigured", disable, err)
	}
}

func TestEstimateCost(t *testing.T) {
	b := &BudgetGuard{}

	tests := []struct {
		name   string
		model  string
		input  int64
		output int64
		want   float64
	}{
		{"known chat model", "anthropic/claude-sonnet-5", 1000, 1000, 1000*0.000003 + 1000*0.000015},
		{"embedding model", "qwen/qwen3-embedding-8b", 1000, 0, 1000 * 0.0000000625},
		{"unknown model falls back", "some/other-model", 1000, 1000, 1000*0.0000005 + 1000*0.000001},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := b.estimateCost(tc.model, tc.input, tc.output)
			if math.Abs(got-tc.want) > 1e-12 {
				t.Errorf("estimateCost(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}
