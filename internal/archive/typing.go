package archive

import (
	"math/rand/v2"
	"time"
)

type TypingTheatre struct {
	config *TypingTheatreConfig
}

type TypingTheatreConfig struct {
	Enabled  bool
	MaxDelay time.Duration
	Chance   float64
}

func NewTypingTheatre(config TypingTheatreConfig) *TypingTheatre {
	if config.MaxDelay == 0 {
		config.MaxDelay = 20 * time.Second
	}
	if config.Chance == 0 {
		config.Chance = 0.25
	}
	return &TypingTheatre{config: &config}
}

func (t *TypingTheatre) ShouldType() bool {
	if !t.config.Enabled {
		return false
	}
	return t.config.Chance >= 1.0 || rand.Float64() <= t.config.Chance
}

// GetTypingSequence returns [type, silent, type] durations proportioned
// 25/60/15 of MaxDelay - the ratio behind the plan's reference sequence
// (5s/12s/3s against the 20s default) - or nil if the theatre doesn't fire
// this time.
func (t *TypingTheatre) GetTypingSequence() []time.Duration {
	if !t.ShouldType() {
		return nil
	}

	total := t.config.MaxDelay
	return []time.Duration{
		total * 25 / 100,
		total * 60 / 100,
		total * 15 / 100,
	}
}
