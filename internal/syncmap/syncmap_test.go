package syncmap

import (
	"maps"
	"testing"
)

func TestLoadStoreDelete(t *testing.T) {
	var m Map[int, string]
	if _, ok := m.Load(1); ok {
		t.Fatal("Load on empty map reported ok")
	}
	m.Store(1, "a")
	if v, ok := m.Load(1); !ok || v != "a" {
		t.Fatalf("Load(1) = %q, %v, want a, true", v, ok)
	}
	m.Delete(1)
	if _, ok := m.Load(1); ok {
		t.Fatal("Load after Delete reported ok")
	}
}

func TestLoadOrStore(t *testing.T) {
	var m Map[int, string]
	if v, loaded := m.LoadOrStore(1, "a"); loaded || v != "a" {
		t.Fatalf("first LoadOrStore = %q, %v, want a, false", v, loaded)
	}
	if v, loaded := m.LoadOrStore(1, "b"); !loaded || v != "a" {
		t.Fatalf("second LoadOrStore = %q, %v, want a, true", v, loaded)
	}
}

func TestLoadAndDelete(t *testing.T) {
	var m Map[int, func() int]
	if _, ok := m.LoadAndDelete(1); ok {
		t.Fatal("LoadAndDelete on empty map reported ok")
	}
	m.Store(1, func() int { return 7 })
	f, ok := m.LoadAndDelete(1)
	if !ok || f() != 7 {
		t.Fatal("LoadAndDelete did not return stored value")
	}
	if _, ok := m.Load(1); ok {
		t.Fatal("key still present after LoadAndDelete")
	}
}

func TestRange(t *testing.T) {
	var m Map[int, string]
	want := map[int]string{1: "a", 2: "b", 3: "c"}
	for k, v := range want {
		m.Store(k, v)
	}
	got := map[int]string{}
	m.Range(func(k int, v string) bool {
		got[k] = v
		return true
	})
	if !maps.Equal(got, want) {
		t.Fatalf("Range collected %v, want %v", got, want)
	}
}
