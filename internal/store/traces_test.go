package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

func TestSaveTokenUsageSkipsZeroCounts(t *testing.T) {
	s := pgtest.Store(t)
	if err := s.SaveTokenUsage(context.Background(), store.TokenUsage{ChannelID: "c", UserID: "u", ModelName: "m"}); err != nil {
		t.Fatalf("SaveTokenUsage: %v", err)
	}
	rows := pgtest.Query[store.TokenUsage](t, "SELECT * FROM token_usages")
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0", len(rows))
	}
}

func TestSaveTokenUsageWritesRow(t *testing.T) {
	s := pgtest.Store(t)
	in := store.TokenUsage{ChannelID: "c", UserID: "u", ModelName: "m", InputTokens: 7, OutputTokens: 3}
	if err := s.SaveTokenUsage(context.Background(), in); err != nil {
		t.Fatalf("SaveTokenUsage: %v", err)
	}
	rows := pgtest.Query[store.TokenUsage](t, "SELECT * FROM token_usages")
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.ChannelID != "c" || got.UserID != "u" || got.ModelName != "m" || got.InputTokens != 7 || got.OutputTokens != 3 {
		t.Errorf("unexpected row: %+v", got)
	}
}

func TestFailureTraceRoundTrip(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	in := store.FailureTrace{TraceID: "t1", ChannelID: "c", UserID: "u", Blob: `{"id":"t1"}`, Decline: true, ErrMsg: "boom"}
	if err := s.SaveFailureTrace(ctx, in); err != nil {
		t.Fatalf("SaveFailureTrace: %v", err)
	}
	got, err := s.GetFailureTraceByTraceID(ctx, "t1")
	if err != nil {
		t.Fatalf("GetFailureTraceByTraceID: %v", err)
	}
	if got.TraceID != in.TraceID || got.ChannelID != in.ChannelID || got.UserID != in.UserID ||
		got.Blob != in.Blob || got.Decline != in.Decline || got.ErrMsg != in.ErrMsg {
		t.Errorf("got %+v, want %+v", got, in)
	}
}

func TestFailureTraceMissing(t *testing.T) {
	s := pgtest.Store(t)
	_, err := s.GetFailureTraceByTraceID(context.Background(), "nope")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want store.ErrNotFound", err)
	}
}
