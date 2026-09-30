package archive

import (
	"strings"
	"testing"
	"time"
)

func TestBuildRecallBlock_EmptyChunksReturnsEmptyString(t *testing.T) {
	r := &Retriever{}
	if got := r.BuildRecallBlock(nil); got != "" {
		t.Fatalf("got %q, want empty string", got)
	}
}

func TestBuildRecallBlock_WrapsDatedChunksInRecallTag(t *testing.T) {
	r := &Retriever{}
	got := r.BuildRecallBlock([]RetrievedChunk{{Content: "[21:04 klaus]: trained again\n", LastActive: time.Date(2026, 8, 12, 21, 4, 0, 0, time.UTC)}})
	want := "<recall>\n" + recallNote + "\n<chunk date=\"2026-08-12\">\n[21:04 klaus]: trained again\n</chunk>\n</recall>\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildRecallBlock_NotesChunksMayBeUnrelated(t *testing.T) {
	r := &Retriever{}
	got := r.BuildRecallBlock([]RetrievedChunk{{Content: "x\n"}})
	if !strings.HasPrefix(got, "<recall>\nPossibly related old chat snippets. Often unrelated; ignore unless clearly relevant.\n") {
		t.Fatalf("missing relevance note: %q", got)
	}
}
