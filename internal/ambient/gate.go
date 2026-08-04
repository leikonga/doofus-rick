package ambient

import (
	"context"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/store"
)

type AmbientConfig struct {
	Enabled      bool
	Window       time.Duration
	MinMsgs      int
	MinAuthors   int
	Cooldown     time.Duration
	DailyCap     int
	EvalDebounce time.Duration
	MinScore     int
	Model        string
	MaxTokens    int64
}

type GateResult struct {
	Passed bool
	Reason string
}

type Ambient struct {
	config AmbientConfig
	store  *store.Store
}

func NewAmbient(config AmbientConfig, s *store.Store) *Ambient {
	if config.Window == 0 {
		config.Window = 90 * time.Second
	}
	if config.MinMsgs == 0 {
		config.MinMsgs = 4
	}
	if config.MinAuthors == 0 {
		config.MinAuthors = 2
	}
	if config.Cooldown == 0 {
		config.Cooldown = 60 * time.Minute
	}
	if config.DailyCap == 0 {
		config.DailyCap = 5
	}
	if config.EvalDebounce == 0 {
		config.EvalDebounce = 60 * time.Second
	}
	if config.MinScore == 0 {
		config.MinScore = 90
	}
	return &Ambient{config: config, store: s}
}

func (a *Ambient) CheckGate(ctx context.Context, channelID snowflake.ID, rickID snowflake.ID, msgs []store.Message) GateResult {
	if !a.config.Enabled {
		return GateResult{Passed: false, Reason: "ambient disabled"}
	}

	if len(msgs) < a.config.MinMsgs {
		return GateResult{Passed: false, Reason: "not enough messages"}
	}

	authors := make(map[uint64]bool)
	for _, msg := range msgs {
		if !msg.IsBot {
			authors[msg.AuthorID] = true
		}
	}
	if len(authors) < a.config.MinAuthors {
		return GateResult{Passed: false, Reason: "not enough authors"}
	}

	state, err := a.store.GetAmbientState(ctx, uint64(channelID))
	if err != nil {
		return GateResult{Passed: false, Reason: "failed to get state"}
	}

	now := time.Now()

	if state.LastFire != nil {
		if now.Sub(*state.LastFire) < a.config.Cooldown {
			return GateResult{Passed: false, Reason: "in cooldown"}
		}
	}

	if state.FiresToday >= a.config.DailyCap {
		return GateResult{Passed: false, Reason: "daily cap reached"}
	}

	if now.Sub(state.LastEval) < a.config.EvalDebounce {
		return GateResult{Passed: false, Reason: "eval debounce"}
	}

	return GateResult{Passed: true}
}

func (a *Ambient) LogFire(ctx context.Context, channelID snowflake.ID, score int, hook string) error {
	log := store.AmbientLog{
		ChannelID: uint64(channelID),
		FiredAt:   time.Now(),
		Score:     score,
		Hook:      &[]string{hook}[0],
	}
	return a.store.LogAmbientFire(ctx, log)
}

func (a *Ambient) UpdateState(ctx context.Context, channelID snowflake.ID, score int, hook string) error {
	state, err := a.store.GetAmbientState(ctx, uint64(channelID))
	if err != nil {
		state = &store.AmbientState{
			ChannelID: uint64(channelID),
		}
	}

	state.LastEval = time.Now()
	state.LastFire = &[]time.Time{time.Now()}[0]
	state.LastUnpromptedID = nil
	state.LastUnpromptedIgnored = false
	state.UpdatedAt = time.Now()

	if err := a.store.UpdateAmbientState(ctx, state); err != nil {
		return err
	}

	return a.store.IncrementAmbientFiresToday(ctx, uint64(channelID))
}
