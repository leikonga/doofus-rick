package agent

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/syncmap"
)

type typist struct {
	theatre  *typingTheatre
	channels syncmap.Map[snowflake.ID, struct{}]
}

func newTypist(config typingTheatreConfig) *typist {
	return &typist{theatre: newTypingTheatre(config)}
}

func (t *typist) start(ctx context.Context, event *events.MessageCreate) <-chan struct{} {
	if _, alreadyTyping := t.channels.LoadOrStore(event.ChannelID, struct{}{}); alreadyTyping {
		return nil
	}
	seq := t.theatre.GetTypingSequence()
	if len(seq) == 0 {
		go func() {
			defer t.channels.Delete(event.ChannelID)
			keepTyping(ctx, event)
		}()
		return nil
	}
	theatreDone := runTypingTheatre(ctx, event, seq)
	go func() {
		defer t.channels.Delete(event.ChannelID)
		<-theatreDone
		keepTyping(ctx, event)
	}()
	return theatreDone
}

func keepTyping(ctx context.Context, event *events.MessageCreate) {
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()
	for {
		if err := event.Client().Rest.SendTyping(event.ChannelID, rest.WithCtx(ctx)); err != nil {
			slog.Warn("failed to send typing indicator", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runTypingTheatre(ctx context.Context, event *events.MessageCreate, sequence []time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i, d := range sequence {
			if i%2 == 0 {
				if err := event.Client().Rest.SendTyping(event.ChannelID, rest.WithCtx(ctx)); err != nil {
					slog.Warn("failed to send typing indicator", "error", err)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
		}
	}()
	return done
}

type typingTheatre struct {
	config *typingTheatreConfig
}

type typingTheatreConfig struct {
	Enabled  bool
	MaxDelay time.Duration
	Chance   float64
}

func newTypingTheatre(config typingTheatreConfig) *typingTheatre {
	if config.MaxDelay == 0 {
		config.MaxDelay = 20 * time.Second
	}
	if config.Chance == 0 {
		config.Chance = 0.25
	}
	return &typingTheatre{config: &config}
}

func (t *typingTheatre) ShouldType() bool {
	if !t.config.Enabled {
		return false
	}
	return t.config.Chance >= 1.0 || rand.Float64() <= t.config.Chance
}

// GetTypingSequence returns [type, silent, type] durations proportioned 25/60/15 of MaxDelay, or nil if the theatre does not fire.
func (t *typingTheatre) GetTypingSequence() []time.Duration {
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
