package ambient

import (
	"context"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

const (
	testChannel = snowflake.ID(500)
	testRickID  = snowflake.ID(999)
)

func integrationGate(t *testing.T) (*Gate, *store.Store) {
	t.Helper()
	s := pgtest.Store(t)
	g := NewGate(GateConfig{
		Enabled:      true,
		MinMsgs:      2,
		MinAuthors:   2,
		Cooldown:     time.Hour,
		DailyCap:     3,
		EvalDebounce: time.Minute,
	}, s)
	return g, s
}

func burst() []store.Message {
	return []store.Message{humanMsg(1), humanMsg(2)}
}

func check(g *Gate) GateResult {
	return g.CheckGate(context.Background(), testChannel, testRickID, burst())
}

func saveState(t *testing.T, s *store.Store, state store.AmbientState) {
	t.Helper()
	state.ChannelID = uint64(testChannel)
	state.UpdatedAt = time.Now()
	if err := s.UpdateAmbientState(context.Background(), &state); err != nil {
		t.Fatalf("UpdateAmbientState: %v", err)
	}
}

func ptr[T any](v T) *T {
	return &v
}

func TestCheckGateAgainstStore(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		state      *store.AmbientState
		wantPassed bool
		wantReason string
	}{
		{name: "no state row", wantPassed: true},
		{name: "empty state row", state: &store.AmbientState{}, wantPassed: true},
		{
			name:       "last unprompted ignored",
			state:      &store.AmbientState{LastUnpromptedIgnored: true},
			wantReason: "last unprompted message was ignored",
		},
		{
			name:       "ignored wins over cooldown",
			state:      &store.AmbientState{LastUnpromptedIgnored: true, LastFire: ptr(now.Add(-time.Minute))},
			wantReason: "last unprompted message was ignored",
		},
		{
			name:       "inside cooldown",
			state:      &store.AmbientState{LastFire: ptr(now.Add(-30 * time.Minute))},
			wantReason: "in cooldown",
		},
		{
			name:       "outside cooldown",
			state:      &store.AmbientState{LastFire: ptr(now.Add(-2 * time.Hour))},
			wantPassed: true,
		},
		{
			name:       "daily cap reached",
			state:      &store.AmbientState{FiresToday: 3},
			wantReason: "daily cap reached",
		},
		{
			name:       "daily cap exceeded",
			state:      &store.AmbientState{FiresToday: 10},
			wantReason: "daily cap reached",
		},
		{
			name:       "below daily cap",
			state:      &store.AmbientState{FiresToday: 2},
			wantPassed: true,
		},
		{
			name:       "inside eval debounce",
			state:      &store.AmbientState{LastEval: now.Add(-10 * time.Second)},
			wantReason: "eval debounce",
		},
		{
			name:       "outside eval debounce",
			state:      &store.AmbientState{LastEval: now.Add(-2 * time.Minute)},
			wantPassed: true,
		},
		{
			name:       "cooldown wins over cap and debounce",
			state:      &store.AmbientState{LastFire: ptr(now.Add(-time.Minute)), FiresToday: 5, LastEval: now},
			wantReason: "in cooldown",
		},
		{
			name:       "cap wins over debounce",
			state:      &store.AmbientState{FiresToday: 5, LastEval: now},
			wantReason: "daily cap reached",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, s := integrationGate(t)
			if tt.state != nil {
				saveState(t, s, *tt.state)
			}
			got := check(g)
			if got.Passed != tt.wantPassed || got.Reason != tt.wantReason {
				t.Errorf("got %+v, want passed=%v reason=%q", got, tt.wantPassed, tt.wantReason)
			}
		})
	}
}

func TestEvalTouchDebouncesNextCheck(t *testing.T) {
	g, s := integrationGate(t)
	ctx := context.Background()

	if r := check(g); !r.Passed {
		t.Fatalf("first check = %+v, want pass", r)
	}
	if err := g.EvalTouch(ctx, testChannel); err != nil {
		t.Fatalf("EvalTouch: %v", err)
	}
	if r := check(g); r.Passed || r.Reason != "eval debounce" {
		t.Errorf("after touch = %+v, want eval debounce", r)
	}

	state, err := s.GetAmbientState(ctx, uint64(testChannel))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(state.LastEval) > time.Minute || state.LastFire != nil || state.FiresToday != 0 {
		t.Errorf("unexpected state after touch: %+v", state)
	}

	old := time.Now().Add(-time.Hour)
	saveState(t, s, store.AmbientState{LastEval: old, FiresToday: 2})
	if err := g.EvalTouch(ctx, testChannel); err != nil {
		t.Fatal(err)
	}
	state, err = s.GetAmbientState(ctx, uint64(testChannel))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(state.LastEval) > time.Minute || state.FiresToday != 2 {
		t.Errorf("touch should update LastEval only: %+v", state)
	}
}

func TestUpdateState(t *testing.T) {
	g, s := integrationGate(t)
	ctx := context.Background()

	if err := g.UpdateState(ctx, testChannel, 4242); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	state, err := s.GetAmbientState(ctx, uint64(testChannel))
	if err != nil {
		t.Fatal(err)
	}
	if state.LastFire == nil || time.Since(*state.LastFire) > time.Minute {
		t.Errorf("LastFire = %v", state.LastFire)
	}
	if time.Since(state.LastEval) > time.Minute {
		t.Errorf("LastEval = %v", state.LastEval)
	}
	if state.LastUnpromptedID == nil || *state.LastUnpromptedID != 4242 {
		t.Errorf("LastUnpromptedID = %v, want 4242", state.LastUnpromptedID)
	}
	if state.FiresToday != 1 {
		t.Errorf("FiresToday = %d, want 1", state.FiresToday)
	}
	if r := check(g); r.Reason != "in cooldown" {
		t.Errorf("check after fire = %+v, want in cooldown", r)
	}

	saveState(t, s, store.AmbientState{LastFire: state.LastFire, FiresToday: 1, LastUnpromptedID: ptr(uint64(4242)), LastUnpromptedIgnored: true})
	if err := g.UpdateState(ctx, testChannel, 0); err != nil {
		t.Fatal(err)
	}
	state, err = s.GetAmbientState(ctx, uint64(testChannel))
	if err != nil {
		t.Fatal(err)
	}
	if state.LastUnpromptedID != nil {
		t.Errorf("LastUnpromptedID = %d, want nil for zero message id", *state.LastUnpromptedID)
	}
	if state.LastUnpromptedIgnored {
		t.Error("LastUnpromptedIgnored should reset to false")
	}
	if state.FiresToday != 2 {
		t.Errorf("FiresToday = %d, want 2", state.FiresToday)
	}
}

func TestLogFire(t *testing.T) {
	g, _ := integrationGate(t)
	ctx := context.Background()

	if err := g.LogFire(ctx, testChannel, 91, "a hook"); err != nil {
		t.Fatalf("LogFire: %v", err)
	}
	if err := g.LogFire(ctx, snowflake.ID(501), 50, "other"); err != nil {
		t.Fatal(err)
	}
	logs := pgtest.Query[store.AmbientLog](t, "SELECT * FROM ambient_logs WHERE channel_id = ? ORDER BY fired_at DESC", uint64(testChannel))
	if len(logs) != 1 {
		t.Fatalf("logs = %d, want 1", len(logs))
	}
	got := logs[0]
	if got.ChannelID != uint64(testChannel) || got.Score != 91 || got.Hook == nil || *got.Hook != "a hook" || time.Since(got.FiredAt) > time.Minute {
		t.Errorf("unexpected log: %+v", got)
	}
	if n := pgtest.Query[int64](t, "SELECT count(*) FROM ambient_logs")[0]; n != 2 {
		t.Errorf("ambient_logs rows = %d, want 2", n)
	}
}
