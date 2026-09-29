package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPprofServerRoutes(t *testing.T) {
	handler := newPprofServer("127.0.0.1:0").Handler
	tests := []struct {
		path string
		want string
	}{
		{"/debug/pprof/", "goroutineleak"},
		{"/debug/pprof/goroutine?debug=1", "goroutine profile"},
		{"/debug/pprof/cmdline", ""},
		{"/debug/pprof/symbol", "num_symbols"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d", tt.path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), tt.want) {
			t.Errorf("%s body missing %q", tt.path, tt.want)
		}
	}
}

func TestWaitAllFinishes(t *testing.T) {
	var done atomic.Int32
	wait := func() { done.Add(1) }
	if !waitAll(context.Background(), wait, wait) {
		t.Fatal("waitAll = false, want true")
	}
	if done.Load() != 2 {
		t.Errorf("waits run = %d, want 2", done.Load())
	}
}

func TestWaitAllGivesUpWhenContextExpires(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	if waitAll(ctx, func() { <-release }) {
		t.Fatal("waitAll = true, want false")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waitAll took %v, want prompt return", elapsed)
	}
}
