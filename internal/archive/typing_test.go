package archive

import (
	"testing"
	"time"
)

func TestTypingTheatre_ShouldType_DisabledAlwaysFalse(t *testing.T) {
	tt := NewTypingTheatre(TypingTheatreConfig{Enabled: false, Chance: 1.0})
	if tt.ShouldType() {
		t.Fatal("expected disabled theatre to never type")
	}
}

func TestTypingTheatre_ShouldType_ChanceOneAlwaysTrue(t *testing.T) {
	tt := NewTypingTheatre(TypingTheatreConfig{Enabled: true, Chance: 1.0})
	for range 20 {
		if !tt.ShouldType() {
			t.Fatal("expected chance=1.0 to always type")
		}
	}
}

func TestTypingTheatre_GetTypingSequence_ScalesToMaxDelay(t *testing.T) {
	// Regression test: MaxDelay was configured but silently ignored in favor
	// of a hardcoded 5s/12s/3s sequence.
	tt := NewTypingTheatre(TypingTheatreConfig{Enabled: true, Chance: 1.0, MaxDelay: 40 * time.Second})
	seq := tt.GetTypingSequence()
	if len(seq) != 3 {
		t.Fatalf("expected 3-step sequence, got %d", len(seq))
	}

	var total time.Duration
	for _, d := range seq {
		total += d
	}
	if total != 40*time.Second {
		t.Errorf("sequence total = %v, want 40s (should scale with MaxDelay)", total)
	}
	if seq[0] != 10*time.Second {
		t.Errorf("type phase = %v, want 10s (25%% of 40s)", seq[0])
	}
	if seq[1] != 24*time.Second {
		t.Errorf("silent phase = %v, want 24s (60%% of 40s)", seq[1])
	}
}

func TestTypingTheatre_GetTypingSequence_EmptyWhenNotTyping(t *testing.T) {
	tt := NewTypingTheatre(TypingTheatreConfig{Enabled: false})
	if seq := tt.GetTypingSequence(); seq != nil {
		t.Fatalf("expected nil sequence when disabled, got %v", seq)
	}
}
