package archive

import "testing"

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

func TestBuildRecallBlock_WrapsContentInRecallTag(t *testing.T) {
	r := &Retriever{}
	got := r.BuildRecallBlock([]RetrievedChunk{{Content: "klaus trained again"}})
	want := "<recall>\nklaus trained again\n</recall>\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
