package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"runtime/pprof"
	"strings"
	"testing"
)

func TestRecoverTurn(t *testing.T) {
	tests := []struct {
		name      string
		wantError bool
	}{
		{"mention without error", false},
		{"ambient with error", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			var err error
			errPtr := &err
			if !tt.wantError {
				errPtr = nil
			}
			pprof.Do(context.Background(), pprof.Labels("handler", "test"), func(ctx context.Context) {
				func() {
					defer recoverTurn(ctx, errPtr)
					panic("boom")
				}()
			})

			var entry struct {
				Level  string            `json:"level"`
				Panic  string            `json:"panic"`
				Stack  string            `json:"stack"`
				Labels map[string]string `json:"labels"`
			}
			if jerr := json.Unmarshal(buf.Bytes(), &entry); jerr != nil {
				t.Fatalf("invalid log output %q: %v", buf.String(), jerr)
			}
			if entry.Level != "ERROR" || entry.Panic != "boom" || entry.Labels["handler"] != "test" {
				t.Fatalf("unexpected log entry: %+v", entry)
			}
			if !strings.Contains(entry.Stack, "recover_test.go") {
				t.Fatalf("stack missing panic site: %s", entry.Stack)
			}
			if tt.wantError && (err == nil || !strings.Contains(err.Error(), "boom")) {
				t.Fatalf("err = %v, want panic error", err)
			}
			if !tt.wantError && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}
