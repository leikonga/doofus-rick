package ambient

import (
	"context"
	"errors"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/store"
	"gorm.io/gorm"
)

type GateConfig struct {
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

type Gate struct {
	config GateConfig
	store  *store.Store
}

func NewGate(config GateConfig, s *store.Store) *Gate {
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
	return &Gate{config: config, store: s}
}

func (g *Gate) CheckGate(ctx context.Context, channelID snowflake.ID, rickID snowflake.ID, msgs []store.Message) GateResult {
	if !g.config.Enabled {
		return GateResult{Passed: false, Reason: "ambient disabled"}
	}

	if len(msgs) < g.config.MinMsgs {
		return GateResult{Passed: false, Reason: "not enough messages"}
	}

	authors := make(map[uint64]bool)
	for _, msg := range msgs {
		if msg.IsBot {
			if msg.AuthorID == uint64(rickID) {
				return GateResult{Passed: false, Reason: "rick already spoke in this burst"}
			}
			continue
		}
		authors[msg.AuthorID] = true
	}
	if len(authors) < g.config.MinAuthors {
		return GateResult{Passed: false, Reason: "not enough authors"}
	}

	state, err := g.store.GetAmbientState(ctx, uint64(channelID))
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return GateResult{Passed: false, Reason: "failed to get state"}
		}
		// No state row yet means ambient has never fired in this channel.
		state = &store.AmbientState{ChannelID: uint64(channelID)}
	}

	if state.LastUnpromptedIgnored {
		return GateResult{Passed: false, Reason: "last unprompted message was ignored"}
	}

	now := time.Now()

	if state.LastFire != nil {
		if now.Sub(*state.LastFire) < g.config.Cooldown {
			return GateResult{Passed: false, Reason: "in cooldown"}
		}
	}

	if state.FiresToday >= g.config.DailyCap {
		return GateResult{Passed: false, Reason: "daily cap reached"}
	}

	if now.Sub(state.LastEval) < g.config.EvalDebounce {
		return GateResult{Passed: false, Reason: "eval debounce"}
	}

	return GateResult{Passed: true}
}

func (g *Gate) LogFire(ctx context.Context, channelID snowflake.ID, score int, hook string) error {
	log := store.AmbientLog{
		ChannelID: uint64(channelID),
		FiredAt:   time.Now(),
		Score:     score,
		Hook:      &[]string{hook}[0],
	}
	return g.store.LogAmbientFire(ctx, log)
}

// UpdateState records a fire and the ID of the unprompted message Rick just
// sent, so a later mechanism can mark it ignored if nobody engages with it.
// unpromptedMsgID is 0 when the send failed and no message exists to track.
func (g *Gate) UpdateState(ctx context.Context, channelID snowflake.ID, score int, hook string, unpromptedMsgID uint64) error {
	state, err := g.store.GetAmbientState(ctx, uint64(channelID))
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		state = &store.AmbientState{ChannelID: uint64(channelID)}
	}

	state.LastEval = time.Now()
	state.LastFire = &[]time.Time{time.Now()}[0]
	state.LastUnpromptedIgnored = false
	if unpromptedMsgID != 0 {
		state.LastUnpromptedID = &unpromptedMsgID
	} else {
		state.LastUnpromptedID = nil
	}
	state.UpdatedAt = time.Now()

	if err := g.store.UpdateAmbientState(ctx, state); err != nil {
		return err
	}

	return g.store.IncrementAmbientFiresToday(ctx, uint64(channelID))
}

// EvalTouch records that the gate was evaluated for a channel without
// firing, so EvalDebounce is honored even when the gate rejects early.
func (g *Gate) EvalTouch(ctx context.Context, channelID snowflake.ID) error {
	state, err := g.store.GetAmbientState(ctx, uint64(channelID))
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		state = &store.AmbientState{ChannelID: uint64(channelID)}
	}
	state.LastEval = time.Now()
	state.UpdatedAt = time.Now()
	return g.store.UpdateAmbientState(ctx, state)
}
