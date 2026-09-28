package archive

import (
	"testing"
	"time"
)

func TestVectorLiteral_FormatsAsPgvectorArray(t *testing.T) {
	got := vectorLiteral([]float32{0.1, -0.25, 3})
	want := "[0.1,-0.25,3]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestVectorLiteral_Empty(t *testing.T) {
	got := vectorLiteral(nil)
	if got != "[]" {
		t.Fatalf("got %q, want %q", got, "[]")
	}
}

func TestBuildRecallBlock_EmptyChunksReturnsEmptyString(t *testing.T) {
	r := &Retriever{}
	if got := r.BuildRecallBlock(nil); got != "" {
		t.Fatalf("got %q, want empty string", got)
	}
}

func TestBuildRecallBlock_WrapsDatedChunksInRecallTag(t *testing.T) {
	r := &Retriever{}
	got := r.BuildRecallBlock([]RetrievedChunk{{Content: "[21:04 klaus]: trained again\n", LastActive: time.Date(2026, 8, 12, 21, 4, 0, 0, time.UTC)}})
	want := "<recall>\n<chunk date=\"2026-08-12\">\n[21:04 klaus]: trained again\n</chunk>\n</recall>\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
