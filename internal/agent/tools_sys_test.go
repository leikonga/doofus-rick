package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/client"
	"github.com/leikonga/doofus-rick/internal/runtimehome"
	"github.com/leikonga/doofus-rick/internal/sandbox"
)

type fakeRuntimeLogs struct {
	entries  []runtimehome.LogEntry
	crashes  []runtimehome.CrashReport
	logErr   error
	gotSince time.Time
}

func (f *fakeRuntimeLogs) Problems(since time.Time) ([]runtimehome.LogEntry, error) {
	f.gotSince = since
	return f.entries, f.logErr
}

func (f *fakeRuntimeLogs) CrashReports() ([]runtimehome.CrashReport, error) {
	return f.crashes, nil
}

func TestLogReportUnavailable(t *testing.T) {
	a := &Agent{}
	if got := a.logReport(24, time.Now()); !strings.Contains(got, "log directory unavailable") {
		t.Fatalf("logReport = %q", got)
	}
}

func TestLogReportHoursClamp(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in, want int
	}{
		{0, 24},
		{-5, 1},
		{1, 1},
		{48, 48},
		{336, 336},
		{10000, 336},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.in), func(t *testing.T) {
			logs := &fakeRuntimeLogs{}
			a := &Agent{runtimeLogs: logs}
			got := a.logReport(tt.in, now)
			if want := now.Add(-time.Duration(tt.want) * time.Hour); !logs.gotSince.Equal(want) {
				t.Errorf("since = %v, want %v", logs.gotSince, want)
			}
			if want := fmt.Sprintf("no warnings or errors in the last %dh", tt.want); got != want {
				t.Errorf("logReport = %q, want %q", got, want)
			}
		})
	}
}

func TestLogReportFormatting(t *testing.T) {
	logs := &fakeRuntimeLogs{
		entries: []runtimehome.LogEntry{
			{
				Time:  time.Date(2026, 9, 28, 14, 32, 5, 0, time.UTC),
				Level: slog.LevelWarn,
				Msg:   "llm retry",
				Attrs: []runtimehome.LogAttr{{Key: "model", Value: "x/y"}, {Key: "error", Value: "context deadline exceeded"}},
			},
			{
				Time:  time.Date(2026, 9, 28, 14, 33, 0, 0, time.FixedZone("CEST", 2*3600)),
				Level: slog.LevelError,
				Msg:   "boom",
			},
		},
		crashes: []runtimehome.CrashReport{
			{Path: "/rick/work/runtime/crash/boot-1.txt", Size: 42, ModTime: time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)},
		},
		logErr: errors.New("read rick-2026-09-28.jsonl: token too long"),
	}
	a := &Agent{runtimeLogs: logs}
	got := a.logReport(24, time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC))
	want := strings.Join([]string{
		"warnings and errors in the last 24h (UTC):",
		`2026-09-28 14:32:05 WARN llm retry model=x/y error="context deadline exceeded"`,
		"2026-09-28 12:33:00 ERROR boom",
		"",
		"crash reports from earlier boots:",
		"/rick/work/runtime/crash/boot-1.txt (42 bytes, 2026-09-27 08:00:00)",
		"",
		"read error: read rick-2026-09-28.jsonl: token too long",
	}, "\n")
	if got != want {
		t.Fatalf("logReport =\n%s\nwant\n%s", got, want)
	}
}

func TestLogReportStaysWithinOutputLimit(t *testing.T) {
	var entries []runtimehome.LogEntry
	for i := range 50 {
		entries = append(entries, runtimehome.LogEntry{
			Time:  time.Date(2026, 9, 28, 10, i, 0, 0, time.UTC),
			Level: slog.LevelWarn,
			Msg:   fmt.Sprintf("m%02d %s", i, strings.Repeat("x", 150)),
		})
	}
	a := &Agent{runtimeLogs: &fakeRuntimeLogs{entries: entries}}
	got := a.logReport(24, time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	if len(got) > client.DefaultOutputLimit {
		t.Fatalf("len = %d, exceeds %d", len(got), client.DefaultOutputLimit)
	}
	if !strings.Contains(got, "(older entries omitted)") || !strings.Contains(got, "m49 ") || strings.Contains(got, "m00 ") {
		t.Fatalf("expected newest entries kept and omission noted:\n%s", got)
	}
}

func TestShellDescriptionListsOnlyAvailableTools(t *testing.T) {
	tools := []sandbox.Tool{
		{Package: "busybox-sh", Commands: []string{"sh"}, Purpose: "posix shell here"},
		{Package: "ghost", Commands: []string{"rick-test-no-such-command"}, Purpose: "never installed"},
		{Package: "font-noto", Purpose: "fonts for rendering"},
	}
	desc := shellDescription("rick", "/rick/work", "127.0.0.1:6060", sandbox.Available(tools))

	for _, want := range []string{
		"- sh: posix shell here",
		"- font-noto: fonts for rendering",
		"user rick",
		"/rick/work",
		"go tool pprof -top http://127.0.0.1:6060/debug/pprof/heap",
		"internal/sandbox/tools.txt",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("description missing %q:\n%s", want, desc)
		}
	}
	for _, unwanted := range []string{"ghost", "never installed", "rick-test-no-such-command"} {
		if strings.Contains(desc, unwanted) {
			t.Errorf("description contains unavailable %q:\n%s", unwanted, desc)
		}
	}
}

func TestShellDescriptionTeachesParallelCallOrdering(t *testing.T) {
	desc := shellDescription("rick", "/rick/work", "", nil)
	for _, want := range []string{"run at the same time", "depend on each other belong in ONE call"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description missing %q:\n%s", want, desc)
		}
	}
}

func TestShellDescriptionOmitsPprofWhenDisabled(t *testing.T) {
	if desc := shellDescription("rick", "/rick/work", "", nil); strings.Contains(desc, "pprof") {
		t.Fatalf("description mentions pprof with empty addr:\n%s", desc)
	}
}
