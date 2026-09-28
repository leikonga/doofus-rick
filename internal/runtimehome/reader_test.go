package runtimehome

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func logLine(at time.Time, level, msg string) string {
	return fmt.Sprintf(`{"time":%q,"level":%q,"msg":%q}`, at.Format(time.RFC3339Nano), level, msg)
}

func messages(entries []LogEntry) []string {
	msgs := make([]string, len(entries))
	for i, e := range entries {
		msgs[i] = e.Msg
	}
	return msgs
}

func TestReadProblems(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	writeLines(t, filepath.Join(dir, "rick-2026-09-26.jsonl"),
		logLine(now.Add(-40*time.Hour), "ERROR", "too old"),
		logLine(now.Add(-38*time.Hour), "WARN", "day one warn"),
	)
	writeLines(t, filepath.Join(dir, "rick-2026-09-28.jsonl"),
		logLine(now.Add(-2*time.Hour), "INFO", "info skipped"),
		"not json at all",
		`{"level":"WARN","msg":"no time"}`,
		`{"time":"2026-09-28T11:00:00Z","level":"LOUD","msg":"bad level"}`,
		logLine(now.Add(-time.Hour), "ERROR", "day three error"),
		logLine(now.Add(-30*time.Minute), "WARN+2", "raised warn"),
		logLine(now.Add(-10*time.Minute), "DEBUG", "debug skipped"),
	)

	entries, err := readProblems(dir, now.Add(-39*time.Hour), now)
	if err != nil {
		t.Fatalf("readProblems: %v", err)
	}
	want := []string{"day one warn", "day three error", "raised warn"}
	if got := messages(entries); !slices.Equal(got, want) {
		t.Fatalf("messages = %v, want %v", got, want)
	}
	if entries[1].Level != slog.LevelError {
		t.Errorf("level = %v, want ERROR", entries[1].Level)
	}
}

func TestReadProblemsCapsKeepNewest(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var lines []string
	for i := range maxProblems + 10 {
		lines = append(lines, logLine(now.Add(time.Duration(i-100)*time.Minute), "WARN", fmt.Sprintf("m%d", i)))
	}
	writeLines(t, filepath.Join(dir, "rick-2026-09-28.jsonl"), lines...)

	entries, err := readProblems(dir, now.Add(-24*time.Hour), now)
	if err != nil {
		t.Fatalf("readProblems: %v", err)
	}
	if len(entries) != maxProblems {
		t.Fatalf("len = %d, want %d", len(entries), maxProblems)
	}
	if first, last := entries[0].Msg, entries[len(entries)-1].Msg; first != "m10" || last != fmt.Sprintf("m%d", maxProblems+9) {
		t.Fatalf("kept %s..%s, want newest", first, last)
	}
}

func TestReadProblemsMissingDirIsEmpty(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	entries, err := readProblems(filepath.Join(t.TempDir(), "absent"), now.Add(-72*time.Hour), now)
	if err != nil || len(entries) != 0 {
		t.Fatalf("got %v, %v; want no entries and no error", entries, err)
	}
}

func TestParseLogLineFlattensAttrs(t *testing.T) {
	line := `{"time":"2026-09-28T14:32:05Z","level":"WARN","msg":"hi","error":"boom","n":3,"g":{"a":true,"b":{"c":"x y"}}}`
	entry, ok := parseLogLine([]byte(line))
	if !ok {
		t.Fatal("parseLogLine rejected valid line")
	}
	want := []LogAttr{{"error", "boom"}, {"n", "3"}, {"g.a", "true"}, {"g.b.c", "x y"}}
	if !slices.Equal(entry.Attrs, want) {
		t.Fatalf("attrs = %v, want %v", entry.Attrs, want)
	}
}

func TestListCrashReports(t *testing.T) {
	dir := t.TempDir()
	files := []struct {
		name    string
		content string
	}{
		{"boot-100.txt", ""},
		{"boot-200.txt", "panic: boom"},
		{"notes.txt", "not a crash"},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	reports, err := listCrashReports(dir)
	if err != nil {
		t.Fatalf("listCrashReports: %v", err)
	}
	if len(reports) != 1 || reports[0].Path != filepath.Join(dir, "boot-200.txt") || reports[0].Size != int64(len("panic: boom")) {
		t.Fatalf("reports = %+v", reports)
	}
}
