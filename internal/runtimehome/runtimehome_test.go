package runtimehome

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestEnsureLayoutIsIdempotent(t *testing.T) {
	work := t.TempDir()
	for range 2 {
		logsDir, crashDir, err := ensureLayout(work)
		if err != nil {
			t.Fatalf("ensureLayout: %v", err)
		}
		for _, dir := range []string{logsDir, crashDir} {
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				t.Fatalf("expected directory %s: %v", dir, err)
			}
		}
		if logsDir != filepath.Join(work, "runtime", "logs") || crashDir != filepath.Join(work, "runtime", "crash") {
			t.Fatalf("unexpected layout: %s %s", logsDir, crashDir)
		}
	}
}

func TestDailyHandlerSwitchesFileOnDateChange(t *testing.T) {
	dir := t.TempDir()
	logs := &dailyLog{dir: dir}
	t.Cleanup(func() { _ = logs.close() })
	logger := slog.New(newDailyHandler(logs)).With("component", "test")

	day1 := time.Date(2026, 9, 27, 23, 59, 0, 0, time.UTC)
	day2 := day1.Add(2 * time.Minute)
	records := []struct {
		at  time.Time
		msg string
	}{
		{day1, "first"},
		{day1.Add(30 * time.Second), "second"},
		{day2, "third"},
	}
	for _, rec := range records {
		r := slog.NewRecord(rec.at, slog.LevelInfo, rec.msg, 0)
		if err := logger.Handler().Handle(context.Background(), r); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}

	tests := []struct {
		file string
		want []string
	}{
		{"rick-2026-09-27.jsonl", []string{"first", "second"}},
		{"rick-2026-09-28.jsonl", []string{"third"}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got := readMessages(t, filepath.Join(dir, tt.file))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("messages = %v, want %v", got, tt.want)
			}
		})
	}
}

func readMessages(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	var msgs []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var line struct {
			Msg       string `json:"msg"`
			Component string `json:"component"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("invalid json line %q: %v", sc.Text(), err)
		}
		if line.Component != "test" {
			t.Fatalf("missing attr in %q", sc.Text())
		}
		msgs = append(msgs, line.Msg)
	}
	return msgs
}

func TestPruneLogs(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	files := []struct {
		name string
		kept bool
	}{
		{"rick-2026-09-28.jsonl", true},
		{"rick-2026-09-14.jsonl", true},
		{"rick-2026-09-13.jsonl", false},
		{"rick-2025-01-01.jsonl", false},
		{"rick-garbage.jsonl", true},
		{"other-2020-01-01.jsonl", true},
		{"rick-2020-01-01.txt", true},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := pruneLogs(dir, now); err != nil {
		t.Fatalf("pruneLogs: %v", err)
	}

	for _, f := range files {
		_, err := os.Stat(filepath.Join(dir, f.name))
		if exists := err == nil; exists != f.kept {
			t.Errorf("%s: exists=%v, want %v", f.name, exists, f.kept)
		}
	}
}

func TestRemoveEmptyCrashFiles(t *testing.T) {
	dir := t.TempDir()
	files := []struct {
		name    string
		content string
		kept    bool
	}{
		{"boot-100.txt", "", false},
		{"boot-200.txt", "panic: boom", true},
		{"boot-300.txt", "", true},
		{"notes.txt", "", true},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := removeEmptyCrashFiles(dir, "boot-300.txt"); err != nil {
		t.Fatalf("removeEmptyCrashFiles: %v", err)
	}

	for _, f := range files {
		_, err := os.Stat(filepath.Join(dir, f.name))
		if exists := err == nil; exists != f.kept {
			t.Errorf("%s: exists=%v, want %v", f.name, exists, f.kept)
		}
	}
}
