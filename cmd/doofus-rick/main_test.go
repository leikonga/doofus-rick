package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
