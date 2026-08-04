package ambient

import (
	"context"
	"testing"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/store"
)

func humanMsg(authorID uint64) store.Message {
	return store.Message{AuthorID: authorID, IsBot: false}
}

func botMsg(authorID uint64) store.Message {
	return store.Message{AuthorID: authorID, IsBot: true}
}

func TestCheckGate_Disabled(t *testing.T) {
	g := &Gate{config: GateConfig{Enabled: false}}
	result := g.CheckGate(context.Background(), snowflake.ID(1), snowflake.ID(999), nil)
	if result.Passed {
		t.Fatal("expected gate to fail when disabled")
	}
}

func TestCheckGate_NotEnoughMessages(t *testing.T) {
	g := &Gate{config: GateConfig{Enabled: true, MinMsgs: 4}}
	msgs := []store.Message{humanMsg(1), humanMsg(2)}
	result := g.CheckGate(context.Background(), snowflake.ID(1), snowflake.ID(999), msgs)
	if result.Passed {
		t.Fatal("expected gate to fail with too few messages")
	}
}

func TestCheckGate_NotEnoughAuthors(t *testing.T) {
	g := &Gate{config: GateConfig{Enabled: true, MinMsgs: 2, MinAuthors: 2}}
	msgs := []store.Message{humanMsg(1), humanMsg(1)}
	result := g.CheckGate(context.Background(), snowflake.ID(1), snowflake.ID(999), msgs)
	if result.Passed {
		t.Fatal("expected gate to fail with only one distinct human author")
	}
}

func TestCheckGate_RickAlreadySpokeInBurst(t *testing.T) {
	// Regression test: rickID was previously accepted but never read, so
	// this condition (documented in the plan's gate checklist) never fired.
	g := &Gate{config: GateConfig{Enabled: true, MinMsgs: 2, MinAuthors: 2}}
	rickID := snowflake.ID(999)
	msgs := []store.Message{humanMsg(1), humanMsg(2), botMsg(uint64(rickID))}
	result := g.CheckGate(context.Background(), snowflake.ID(1), rickID, msgs)
	if result.Passed {
		t.Fatal("expected gate to fail once rick has already spoken in the burst")
	}
	if result.Reason != "rick already spoke in this burst" {
		t.Errorf("reason = %q, want %q", result.Reason, "rick already spoke in this burst")
	}
}

func TestCheckGate_OtherBotsDoNotCountAsRickSpeaking(t *testing.T) {
	// MinAuthors is set unreachably high so the gate fails on the author
	// count (never touching the store, which is nil in this test) rather
	// than proceeding - the only thing under test is that a different bot's
	// message isn't mistaken for rick having spoken.
	g := &Gate{config: GateConfig{Enabled: true, MinMsgs: 3, MinAuthors: 99}}
	rickID := snowflake.ID(999)
	otherBotID := uint64(111)
	msgs := []store.Message{humanMsg(1), humanMsg(2), botMsg(otherBotID)}
	result := g.CheckGate(context.Background(), snowflake.ID(1), rickID, msgs)
	if result.Reason == "rick already spoke in this burst" {
		t.Fatal("a different bot's message should not be attributed to rick")
	}
	if result.Reason != "not enough authors" {
		t.Fatalf("reason = %q, want %q", result.Reason, "not enough authors")
	}
}
