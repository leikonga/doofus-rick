package store

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
