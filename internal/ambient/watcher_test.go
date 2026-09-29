package ambient

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

type fakeClassifier struct {
	result ClassifierResult
	err    error
	calls  [][]llm.Message
}

func (f *fakeClassifier) Classify(_ context.Context, _ uint64, messages []llm.Message) (ClassifierResult, error) {
	f.calls = append(f.calls, messages)
	return f.result, f.err
}

type hookCall struct {
	channel snowflake.ID
	hook    string
}

type fakeResponder struct {
	sentID snowflake.ID
	err    error
	calls  []hookCall
}

func (f *fakeResponder) HandleAmbient(_ context.Context, channelID snowflake.ID, hook string) (snowflake.ID, error) {
	f.calls = append(f.calls, hookCall{channelID, hook})
	return f.sentID, f.err
}

type watcherEnv struct {
	w          *Watcher
	store      *store.Store
	classifier *fakeClassifier
	responder  *fakeResponder
}

func newWatcherEnv(t *testing.T) *watcherEnv {
	t.Helper()
	s := pgtest.Store(t)
	gate := NewGate(GateConfig{
		Enabled:      true,
		Window:       time.Hour,
		MinMsgs:      2,
		MinAuthors:   2,
		Cooldown:     time.Hour,
		DailyCap:     3,
		EvalDebounce: time.Minute,
	}, s)
	c := &fakeClassifier{}
	r := &fakeResponder{}
	w := NewWatcher(WatcherConfig{Enabled: true, Window: time.Hour}, s, gate, c, r, func() snowflake.ID { return testRickID })
	return &watcherEnv{w: w, store: s, classifier: c, responder: r}
}

func (e *watcherEnv) seed(t *testing.T, id, authorID uint64, name, content string, isBot bool) {
	t.Helper()
	err := e.store.CreateMessage(context.Background(), store.Message{
		ID:         id,
		ChannelID:  uint64(testChannel),
		AuthorID:   authorID,
		AuthorName: name,
		Content:    content,
		IsBot:      isBot,
		CreatedAt:  time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
}

func (e *watcherEnv) seedBurst(t *testing.T) {
	t.Helper()
	e.seed(t, 1, 1, "alice", "hello", false)
	e.seed(t, 2, 2, "bob", "world", false)
}

func (e *watcherEnv) state(t *testing.T) *store.AmbientState {
	t.Helper()
	state, err := e.store.GetAmbientState(context.Background(), uint64(testChannel))
	if err != nil {
		t.Fatalf("GetAmbientState: %v", err)
	}
	return state
}

type logRow struct {
	Score int
	Hook  string
}

func (e *watcherEnv) logs(t *testing.T) []logRow {
	t.Helper()
	return pgtest.Query[logRow](t, "SELECT score, hook FROM ambient_logs WHERE channel_id = ?", uint64(testChannel))
}

func TestWatcherCheckGateRejects(t *testing.T) {
	e := newWatcherEnv(t)
	e.seed(t, 1, 1, "alice", "hello", false)

	e.w.check(context.Background(), testChannel)

	if len(e.classifier.calls) != 0 {
		t.Errorf("classifier called %d times, want 0", len(e.classifier.calls))
	}
	if state := e.state(t); time.Since(state.LastEval) > time.Minute {
		t.Errorf("LastEval = %v, want recent", state.LastEval)
	}
}

func TestWatcherCheckRickAlreadySpoke(t *testing.T) {
	e := newWatcherEnv(t)
	e.seedBurst(t)
	e.seed(t, 3, uint64(testRickID), "rick", "burp", true)

	e.w.check(context.Background(), testChannel)

	if len(e.classifier.calls) != 0 {
		t.Errorf("classifier called %d times, want 0", len(e.classifier.calls))
	}
	if len(e.responder.calls) != 0 {
		t.Errorf("responder called %d times, want 0", len(e.responder.calls))
	}
}

func TestWatcherCheckEmptyHook(t *testing.T) {
	e := newWatcherEnv(t)
	e.seedBurst(t)

	e.w.check(context.Background(), testChannel)

	if len(e.classifier.calls) != 1 {
		t.Fatalf("classifier called %d times, want 1", len(e.classifier.calls))
	}
	if len(e.responder.calls) != 0 {
		t.Errorf("responder called %d times, want 0", len(e.responder.calls))
	}
	if got := e.logs(t); len(got) != 0 {
		t.Errorf("ambient_logs = %v, want none", got)
	}
}

func TestWatcherCheckFires(t *testing.T) {
	e := newWatcherEnv(t)
	e.seedBurst(t)
	e.classifier.result = ClassifierResult{Score: 95, Hook: "h"}
	e.responder.sentID = 777

	e.w.check(context.Background(), testChannel)

	if !slices.Equal(e.responder.calls, []hookCall{{testChannel, "h"}}) {
		t.Errorf("responder calls = %v", e.responder.calls)
	}
	if got := e.logs(t); !slices.Equal(got, []logRow{{Score: 95, Hook: "h"}}) {
		t.Errorf("ambient_logs = %v", got)
	}
	state := e.state(t)
	if state.LastUnpromptedID == nil || *state.LastUnpromptedID != 777 {
		t.Errorf("LastUnpromptedID = %v, want 777", state.LastUnpromptedID)
	}
	if state.FiresToday != 1 {
		t.Errorf("FiresToday = %d, want 1", state.FiresToday)
	}
	if state.LastFire == nil || time.Since(*state.LastFire) > time.Minute {
		t.Errorf("LastFire = %v", state.LastFire)
	}
}

func TestWatcherCheckResponderError(t *testing.T) {
	e := newWatcherEnv(t)
	e.seedBurst(t)
	e.classifier.result = ClassifierResult{Score: 95, Hook: "h"}
	e.responder.err = errors.New("send failed")

	e.w.check(context.Background(), testChannel)

	if len(e.responder.calls) != 1 {
		t.Errorf("responder called %d times, want 1", len(e.responder.calls))
	}
	if got := e.logs(t); len(got) != 0 {
		t.Errorf("ambient_logs = %v, want none", got)
	}
	if state := e.state(t); state.FiresToday != 0 || state.LastFire != nil {
		t.Errorf("state changed after responder error: %+v", state)
	}
}

func TestWatcherCheckFormatsMessages(t *testing.T) {
	e := newWatcherEnv(t)
	e.seed(t, 1, 1, "alice", "hello", false)
	e.seed(t, 2, 2, "bob", "world", false)
	e.seed(t, 3, 50, "helper", "beep", true)

	e.w.check(context.Background(), testChannel)

	if len(e.classifier.calls) != 1 {
		t.Fatalf("classifier called %d times, want 1", len(e.classifier.calls))
	}
	var got []string
	for _, m := range e.classifier.calls[0] {
		got = append(got, m.Parts[0].Text)
	}
	want := []string{"[alice]: hello", "[bob]: world", "[helper (bot)]: beep"}
	if !slices.Equal(got, want) {
		t.Errorf("classifier messages = %q, want %q", got, want)
	}
}

func TestWatcherCheckDisabled(t *testing.T) {
	c := &fakeClassifier{}
	r := &fakeResponder{}
	w := NewWatcher(WatcherConfig{Enabled: false, Window: time.Hour}, nil, nil, c, r, func() snowflake.ID { return testRickID })

	w.Check(testChannel)

	if len(c.calls) != 0 || len(r.calls) != 0 {
		t.Errorf("disabled watcher did work: classifier=%d responder=%d", len(c.calls), len(r.calls))
	}
}
