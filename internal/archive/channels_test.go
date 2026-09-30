package archive

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

type fakeChannelGetter struct {
	calls int
	names map[snowflake.ID]string
}

func (f *fakeChannelGetter) GetChannel(id snowflake.ID, _ ...rest.RequestOpt) (discord.Channel, error) {
	f.calls++
	name, ok := f.names[id]
	if !ok {
		return nil, errors.New("unknown channel")
	}
	var ch discord.GuildTextChannel
	raw, _ := json.Marshal(map[string]any{"id": id.String(), "type": 0, "name": name})
	if err := json.Unmarshal(raw, &ch); err != nil {
		return nil, err
	}
	return ch, nil
}

func TestChannelNames_CachesHitsAndMisses(t *testing.T) {
	getter := &fakeChannelGetter{names: map[snowflake.ID]string{1: "general"}}
	names := NewChannelNames(getter)
	ctx := context.Background()

	for range 2 {
		if got := names.ChannelName(ctx, 1); got != "general" {
			t.Fatalf("got %q, want general", got)
		}
		if got := names.ChannelName(ctx, 2); got != "" {
			t.Fatalf("got %q, want empty for unknown channel", got)
		}
	}
	if getter.calls != 2 {
		t.Fatalf("got %d lookups, want 2", getter.calls)
	}
}

func TestChannelNames_CancelledContextIsNotCached(t *testing.T) {
	getter := &fakeChannelGetter{names: map[snowflake.ID]string{1: "general"}}
	names := NewChannelNames(getter)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := names.ChannelName(ctx, 1); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if got := names.ChannelName(context.Background(), 1); got != "general" {
		t.Fatalf("got %q, want general after retry", got)
	}
}
