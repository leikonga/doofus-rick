package archive

import (
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
	return t.config.Chance >= 1.0 || randFloat64() <= t.config.Chance
}

func randFloat64() float64 {
	return float64(time.Now().UnixNano()%1000) / 1000.0
}

func (t *TypingTheatre) GetTypingSequence() []time.Duration {
	if !t.ShouldType() {
		return nil
	}

	return []time.Duration{
		5 * time.Second,
		12 * time.Second,
		3 * time.Second,
	}
}
