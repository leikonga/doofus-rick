package profile

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/archive"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

type fakeStore struct {
	mu         sync.Mutex
	candidates []store.ProfileCandidate
	messages   map[uint64][]store.Message
	profiles   map[uint64]*store.PersonProfile
	saved      map[uint64]store.PersonProfile
	usage      []store.TokenUsage
	gotMin     int
	gotChans   []uint64
}

func (f *fakeStore) GetProfileCandidates(_ context.Context, channelIDs []uint64, minNew, _ int) ([]store.ProfileCandidate, error) {
	f.gotMin, f.gotChans = minNew, channelIDs
	return f.candidates, nil
}

func (f *fakeStore) GetAuthorMessagesAfter(_ context.Context, authorID, _ uint64, _ []uint64, _ int) ([]store.Message, error) {
	return f.messages[authorID], nil
}

func (f *fakeStore) GetPersonProfile(_ context.Context, userID uint64) (*store.PersonProfile, error) {
	if p, ok := f.profiles[userID]; ok {
		return p, nil
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) SavePersonProfile(_ context.Context, p store.PersonProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saved == nil {
		f.saved = map[uint64]store.PersonProfile{}
	}
	f.saved[p.UserID] = p
	return nil
}

func (f *fakeStore) SaveTokenUsage(_ context.Context, u store.TokenUsage) error {
	f.usage = append(f.usage, u)
	return nil
}

type fakeLLM struct {
	reqs    []llm.CompletionRequest
	replies []string
	errs    []error
}

func (f *fakeLLM) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	i := len(f.reqs)
	f.reqs = append(f.reqs, req)
	if i < len(f.errs) && f.errs[i] != nil {
		return llm.CompletionResponse{}, f.errs[i]
	}
	reply := `{"profile":"updated profile"}`
	if i < len(f.replies) {
		reply = f.replies[i]
	}
	return llm.CompletionResponse{Message: llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.TextPart(reply)}}, InputTokens: 11, OutputTokens: 7}, nil
}

type fakeChannels struct {
	ids []uint64
	err error
}

func (f fakeChannels) PublicChannelIDs(context.Context) ([]uint64, error) { return f.ids, f.err }

type fakeNamer map[uint64]string

func (f fakeNamer) ChannelName(_ context.Context, id uint64) string { return f[id] }

func msgAt(id, channel, author uint64, content string) store.Message {
	return store.Message{ID: id, ChannelID: channel, AuthorID: author, AuthorName: "klaus", Content: content, CreatedAt: time.Date(2025, 3, 14, 23, 0, 0, 0, time.UTC)}
}

func newTestUpdater(st *fakeStore, c *fakeLLM, ch fakeChannels, names fakeNamer) *Updater {
	u := NewUpdater(Config{Model: "m", MinNewMessages: 5}, c, st, ch, archive.NoChannelNames{})
	if names != nil {
		u.names = names
	}
	return u
}

func TestRunOnceBuildsPromptAndSavesWatermark(t *testing.T) {
	st := &fakeStore{
		candidates: []store.ProfileCandidate{{AuthorID: 7, Watermark: 50}},
		messages:   map[uint64][]store.Message{7: {msgAt(60, 1, 7, "first line\nsecond"), msgAt(70, 2, 7, "other channel")}},
		profiles:   map[uint64]*store.PersonProfile{7: {UserID: 7, Summary: "EXISTING SUMMARY"}},
	}
	c := &fakeLLM{}
	newTestUpdater(st, c, fakeChannels{ids: []uint64{1, 2}}, fakeNamer{1: "general"}).runOnce(context.Background())

	if len(c.reqs) != 1 {
		t.Fatalf("requests = %d", len(c.reqs))
	}
	req := c.reqs[0]
	prompt := req.Messages[0].Text()
	for _, want := range []string{"EXISTING SUMMARY", "[2025-03-14 #general] first line second", "[2025-03-14] other channel"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if req.Model != "m" || req.ResponseSchema == nil {
		t.Errorf("request = %+v", req)
	}
	if st.gotMin != 5 || len(st.gotChans) != 2 {
		t.Errorf("candidate query args = %d, %v", st.gotMin, st.gotChans)
	}
	saved := st.saved[7]
	if saved.Summary != "updated profile" || saved.WatermarkMessageID != 70 {
		t.Errorf("saved = %+v", saved)
	}
	if len(st.usage) != 1 || st.usage[0].UserID != "profile-updater" || st.usage[0].InputTokens != 11 {
		t.Errorf("usage = %+v", st.usage)
	}
}

func TestRunOnceWithoutExistingProfile(t *testing.T) {
	st := &fakeStore{
		candidates: []store.ProfileCandidate{{AuthorID: 7}},
		messages:   map[uint64][]store.Message{7: {msgAt(60, 1, 7, "hello")}},
	}
	c := &fakeLLM{}
	newTestUpdater(st, c, fakeChannels{ids: []uint64{1}}, fakeNamer(nil)).runOnce(context.Background())
	if !strings.Contains(c.reqs[0].Messages[0].Text(), "none yet") {
		t.Errorf("prompt = %s", c.reqs[0].Messages[0].Text())
	}
}

func TestRunOnceOneFailureDoesNotStopOthers(t *testing.T) {
	st := &fakeStore{
		candidates: []store.ProfileCandidate{{AuthorID: 1}, {AuthorID: 2}, {AuthorID: 3}},
		messages: map[uint64][]store.Message{
			1: {msgAt(10, 1, 1, "aaa")}, 2: {msgAt(20, 1, 2, "bbb")}, 3: {msgAt(30, 1, 3, "ccc")},
		},
	}
	c := &fakeLLM{errs: []error{errors.New("boom")}, replies: []string{"", `{"profile":"   "}`, `{"profile":"ok"}`}}
	newTestUpdater(st, c, fakeChannels{ids: []uint64{1}}, fakeNamer(nil)).runOnce(context.Background())

	if len(c.reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(c.reqs))
	}
	if _, ok := st.saved[1]; ok {
		t.Error("author 1 failed and must not be saved")
	}
	if _, ok := st.saved[2]; ok {
		t.Error("author 2 returned an empty profile and must not be saved")
	}
	if st.saved[3].Summary != "ok" {
		t.Errorf("author 3 = %+v", st.saved[3])
	}
}

func TestRunOnceSkipsWithoutPublicChannels(t *testing.T) {
	for name, ch := range map[string]fakeChannels{
		"error": {err: errors.New("discord down")},
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			st := &fakeStore{candidates: []store.ProfileCandidate{{AuthorID: 7}}, messages: map[uint64][]store.Message{7: {msgAt(1, 1, 7, "hello")}}}
			c := &fakeLLM{}
			newTestUpdater(st, c, ch, fakeNamer(nil)).runOnce(context.Background())
			if len(c.reqs) != 0 || len(st.saved) != 0 {
				t.Errorf("requests = %d, saved = %v", len(c.reqs), st.saved)
			}
		})
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	st := &fakeStore{}
	u := NewUpdater(Config{Model: "m", InitialDelay: time.Hour, Interval: time.Hour}, &fakeLLM{}, st, fakeChannels{ids: []uint64{1}}, archive.NoChannelNames{})
	ctx, cancel := context.WithCancel(context.Background())
	u.Start(ctx)
	cancel()

	done := make(chan struct{})
	go func() { u.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("updater did not stop after cancel")
	}
}

func TestRunStopsMidRun(t *testing.T) {
	st := &fakeStore{
		candidates: []store.ProfileCandidate{{AuthorID: 1}, {AuthorID: 2}},
		messages:   map[uint64][]store.Message{1: {msgAt(10, 1, 1, "aaa")}, 2: {msgAt(20, 1, 2, "bbb")}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &cancelingLLM{fakeLLM: &fakeLLM{}, cancel: cancel}
	u := NewUpdater(Config{Model: "m"}, c, st, fakeChannels{ids: []uint64{1}}, archive.NoChannelNames{})
	u.runOnce(ctx)
	if len(c.reqs) != 1 {
		t.Errorf("requests = %d, want 1 (second author skipped after cancel)", len(c.reqs))
	}
}

type cancelingLLM struct {
	*fakeLLM
	cancel func()
}

func (c *cancelingLLM) Complete(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	resp, err := c.fakeLLM.Complete(ctx, req)
	c.cancel()
	return resp, err
}

func TestRunFirstTickAfterInitialDelay(t *testing.T) {
	st := &fakeStore{
		candidates: []store.ProfileCandidate{{AuthorID: 7}},
		messages:   map[uint64][]store.Message{7: {msgAt(1, 1, 7, "hello")}},
	}
	u := NewUpdater(Config{Model: "m", InitialDelay: 5 * time.Millisecond, Interval: time.Hour}, &fakeLLM{}, st, fakeChannels{ids: []uint64{1}}, archive.NoChannelNames{})
	ctx, cancel := context.WithCancel(context.Background())
	u.Start(ctx)
	deadline := time.After(2 * time.Second)
	for {
		st.mu.Lock()
		n := len(st.saved)
		st.mu.Unlock()
		if n == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no profile saved after initial delay")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	u.Wait()
}
