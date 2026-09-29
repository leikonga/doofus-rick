package archive

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

const (
	testRickID  = snowflake.ID(900)
	testBotID   = snowflake.ID(901)
	testHumanID = snowflake.ID(902)
)

type fakeREST struct {
	mu       sync.Mutex
	channels []discord.GuildChannel
	messages map[snowflake.ID][]discord.Message
}

func (f *fakeREST) GetGuildChannels(snowflake.ID, ...rest.RequestOpt) ([]discord.GuildChannel, error) {
	return f.channels, nil
}

func (f *fakeREST) GetMessages(channelID snowflake.ID, around, before, _ snowflake.ID, limit int, _ ...rest.RequestOpt) ([]discord.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// backfillChannel passes its paging cursor as `around`; the fake pages on either.
	if before == 0 {
		before = around
	}
	var out []discord.Message
	for _, m := range f.messages[channelID] {
		if before != 0 && m.ID >= before {
			continue
		}
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b discord.Message) int { return int(b.ID) - int(a.ID) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func textChannel(t *testing.T, id snowflake.ID) discord.GuildChannel {
	t.Helper()
	var ch discord.GuildTextChannel
	raw := []byte(`{"id":"` + id.String() + `","type":0}`)
	if err := json.Unmarshal(raw, &ch); err != nil {
		t.Fatalf("unmarshal channel: %v", err)
	}
	return ch
}

func testMessage(id snowflake.ID, author discord.User, content string) discord.Message {
	return discord.Message{
		ID:        id,
		Author:    author,
		Content:   content,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, int(id), 0, time.UTC),
	}
}

var (
	rickUser  = discord.User{ID: testRickID, Username: "rick", Bot: true}
	botUser   = discord.User{ID: testBotID, Username: "otherbot", Bot: true}
	humanUser = discord.User{ID: testHumanID, Username: "human"}
)

func newTestIngest(s *store.Store, client discordREST, cfg IngestConfig) *Ingest {
	return NewIngest(cfg, s, client, func() snowflake.ID { return testRickID }, nil, nil, nil)
}

func archivedIDs(t *testing.T) []uint64 {
	t.Helper()
	return pgtest.Query[uint64](t, "SELECT id FROM messages ORDER BY id")
}

func TestBackfillWorker(t *testing.T) {
	s := pgtest.Store(t)

	fake := &fakeREST{
		channels: []discord.GuildChannel{textChannel(t, 10), textChannel(t, 20)},
		messages: map[snowflake.ID][]discord.Message{
			10: {
				testMessage(101, humanUser, "one"),
				testMessage(102, rickUser, "rick says"),
				testMessage(103, botUser, "bot says"),
				testMessage(104, humanUser, "/cmd"),
				testMessage(105, humanUser, "two"),
			},
			20: {
				testMessage(201, humanUser, "three"),
				testMessage(202, rickUser, "rick again"),
				testMessage(203, humanUser, "four"),
			},
		},
	}
	i := newTestIngest(s, fake, IngestConfig{
		ArchiveEnabled:  true,
		BackfillEnabled: true,
		GuildID:         "1",
		BackfillDelay:   0,
		BackfillBatch:   2,
	})

	i.runBackfillWorker(context.Background())

	want := []uint64{101, 103, 105, 201, 203}
	if got := archivedIDs(t); !slices.Equal(got, want) {
		t.Errorf("archived ids = %v, want %v", got, want)
	}

	ctx := context.Background()
	for _, id := range []uint64{10, 20} {
		ch, err := s.GetBackfillChannel(ctx, id)
		if err != nil {
			t.Fatalf("GetBackfillChannel(%d): %v", id, err)
		}
		if !ch.Done {
			t.Errorf("channel %d not done", id)
		}
		if ch.LastError != nil {
			t.Errorf("channel %d last error = %q", id, *ch.LastError)
		}
	}

	state, err := s.GetBackfillState(ctx)
	if err != nil {
		t.Fatalf("GetBackfillState: %v", err)
	}
	if state.Status != "done" {
		t.Errorf("status = %q, want done", state.Status)
	}
	if state.ChannelsDone != 2 || state.ChannelsTotal != 2 {
		t.Errorf("channels done/total = %d/%d, want 2/2", state.ChannelsDone, state.ChannelsTotal)
	}
}

func waitForIDs(t *testing.T, want []uint64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := archivedIDs(t)
		if slices.Equal(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("archived ids = %v, want %v", got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRecordLive(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	i := newTestIngest(s, &fakeREST{}, IngestConfig{ArchiveEnabled: true})

	if !i.RecordLive(ctx, testMessage(1, humanUser, "hi"), 10) {
		t.Error("human message: RecordLive = false, want true")
	}
	waitForIDs(t, []uint64{1})

	if i.RecordLive(ctx, testMessage(2, rickUser, "hello"), 10) {
		t.Error("rick message: RecordLive = true, want false")
	}
	waitForIDs(t, []uint64{1, 2})

	if i.RecordLive(ctx, testMessage(3, botUser, "beep"), 10) {
		t.Error("other bot message: RecordLive = true, want false")
	}

	if err := s.ForgetAuthor(ctx, uint64(testHumanID)); err != nil {
		t.Fatalf("ForgetAuthor: %v", err)
	}
	if i.RecordLive(ctx, testMessage(4, humanUser, "forgotten"), 10) {
		t.Error("forgotten author: RecordLive = true, want false")
	}

	time.Sleep(200 * time.Millisecond)
	if got := archivedIDs(t); !slices.Equal(got, []uint64{1, 2}) {
		t.Errorf("archived ids after forget = %v, want [1 2]", got)
	}
}

func TestBackfillPageDelayHonoursContext(t *testing.T) {
	s := pgtest.Store(t)
	pages := []discord.Message{
		testMessage(101, humanUser, "one"),
		testMessage(102, humanUser, "two"),
		testMessage(103, humanUser, "three"),
	}
	fake := &fakeREST{
		channels: []discord.GuildChannel{textChannel(t, 10), textChannel(t, 20)},
		messages: map[snowflake.ID][]discord.Message{10: pages, 20: pages},
	}
	i := newTestIngest(s, fake, IngestConfig{
		ArchiveEnabled:  true,
		BackfillEnabled: true,
		GuildID:         "1",
		BackfillDelay:   time.Hour,
		BackfillBatch:   2,
	})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	defer cancel()

	start := time.Now()
	i.runBackfillWorker(ctx)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("worker took %v to stop, want well under 1s", elapsed)
	}

	state, err := s.GetBackfillState(context.Background())
	if err != nil {
		t.Fatalf("GetBackfillState: %v", err)
	}
	if state.Status != "failed" || state.LastError == nil || *state.LastError != "interrupted" {
		t.Errorf("state = %q / %v, want failed / interrupted", state.Status, state.LastError)
	}
}
