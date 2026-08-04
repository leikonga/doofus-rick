package archive

import (
	"math"
	"testing"
)

func TestTruncateTo1024_NoTruncationBelowLimit(t *testing.T) {
	vec := []float32{1, 2, 3}
	got := truncateTo1024(vec)
	if len(got) != 3 {
		t.Fatalf("got len %d, want 3", len(got))
	}
	for i, v := range vec {
		if got[i] != v {
			t.Errorf("got[%d] = %v, want %v (should pass through unchanged)", i, got[i], v)
		}
	}
}

func TestTruncateTo1024_TruncatesAndL2Normalizes(t *testing.T) {
	vec := make([]float32, 2000)
	for i := range vec {
		vec[i] = 1
	}

	got := truncateTo1024(vec)
	if len(got) != 1024 {
		t.Fatalf("got len %d, want 1024", len(got))
	}

	var sumSq float64
	for _, v := range got {
		sumSq += float64(v) * float64(v)
	}
	norm := math.Sqrt(sumSq)
	if math.Abs(norm-1.0) > 1e-4 {
		t.Fatalf("L2 norm = %v, want ~1.0 (regression: normalization must divide by sqrt(sum of squares), not sum of squares)", norm)
	}

	// Every input component was 1, so after truncation and unit-normalizing a
	// 1024-length all-ones vector each component must equal 1/sqrt(1024).
	want := float32(1.0 / math.Sqrt(1024))
	if math.Abs(float64(got[0]-want)) > 1e-6 {
		t.Errorf("got[0] = %v, want %v", got[0], want)
	}
}

func TestTruncateTo1024_ZeroVectorLeftUnscaled(t *testing.T) {
	vec := make([]float32, 2000)
	got := truncateTo1024(vec)
	for i, v := range got {
		if v != 0 {
			t.Fatalf("got[%d] = %v, want 0 (division by zero must be avoided)", i, v)
		}
	}
}
