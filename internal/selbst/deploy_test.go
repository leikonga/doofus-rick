package selbst

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/leikonga/doofus-rick/internal/runtimehome"
)

const (
	oldCommit = "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newCommit = "2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	crashA    = "/w/runtime/crash/boot-100.txt"
	crashB    = "/w/runtime/crash/boot-200.txt"
	current   = "/w/runtime/crash/boot-300.txt"
)

var shipAt = time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)

func ship() runtimehome.DeployRecord {
	return runtimehome.DeployRecord{Kind: runtimehome.DeployShip, Commit: newCommit, ChannelID: "42", Requester: "7", Summary: "fix thing\n\nbody", At: shipAt}
}

func boot(commit, crashFile string, at time.Time) runtimehome.DeployRecord {
	return runtimehome.DeployRecord{Kind: runtimehome.DeployBoot, Commit: commit, CrashFile: crashFile, At: at}
}

func reported(commit, crashFile string) runtimehome.DeployRecord {
	return runtimehome.DeployRecord{Kind: runtimehome.DeployReported, Commit: commit, CrashFile: crashFile, At: shipAt.Add(time.Hour)}
}

func crash(path string, at time.Time) runtimehome.CrashReport {
	return runtimehome.CrashReport{Path: path, Size: 10, ModTime: at}
}

func TestEvaluateBoot(t *testing.T) {
	after := shipAt.Add(5 * time.Minute)
	tests := []struct {
		name        string
		records     []runtimehome.DeployRecord
		running     string
		crashes     []runtimehome.CrashReport
		want        DeployOutcome
		wantCrash   string
		wantCrashed string
	}{
		{name: "no ship", records: []runtimehome.DeployRecord{boot(oldCommit, crashA, shipAt)}, running: oldCommit},
		{
			name:    "success",
			records: []runtimehome.DeployRecord{ship(), boot(newCommit, current, after)},
			running: newCommit,
			want:    DeploySucceeded,
		},
		{
			name:    "success already reported",
			records: []runtimehome.DeployRecord{ship(), reported(newCommit, "")},
			running: newCommit,
		},
		{
			name:    "running older than latest ship",
			records: []runtimehome.DeployRecord{ship()},
			running: oldCommit,
		},
		{
			name:    "unknown running commit",
			records: []runtimehome.DeployRecord{ship()},
		},
		{
			name:        "crash after ship",
			records:     []runtimehome.DeployRecord{ship(), boot(newCommit, crashA, after)},
			running:     newCommit,
			crashes:     []runtimehome.CrashReport{crash(crashA, after.Add(time.Minute))},
			want:        DeployCrashed,
			wantCrash:   crashA,
			wantCrashed: newCommit,
		},
		{
			name:        "crash while ship pending",
			records:     []runtimehome.DeployRecord{boot(oldCommit, crashA, shipAt.Add(-time.Hour)), ship()},
			running:     oldCommit,
			crashes:     []runtimehome.CrashReport{crash(crashA, after)},
			want:        DeployCrashed,
			wantCrash:   crashA,
			wantCrashed: oldCommit,
		},
		{
			name:    "crash after ship already reported",
			records: []runtimehome.DeployRecord{ship(), boot(newCommit, crashA, after), reported(newCommit, crashA), boot(newCommit, crashB, after)},
			running: newCommit,
			crashes: []runtimehome.CrashReport{crash(crashA, after), crash(crashB, after.Add(time.Minute))},
		},
		{
			name:    "crash before ship is not attributed",
			records: []runtimehome.DeployRecord{ship()},
			running: oldCommit,
			crashes: []runtimehome.CrashReport{crash(crashA, shipAt.Add(-time.Minute))},
		},
		{
			name:    "crash with no ship",
			records: []runtimehome.DeployRecord{boot(oldCommit, crashA, shipAt)},
			running: oldCommit,
			crashes: []runtimehome.CrashReport{crash(crashA, after)},
		},
		{
			name:    "current boot crash file ignored",
			records: []runtimehome.DeployRecord{ship(), reported(newCommit, "")},
			running: newCommit,
			crashes: []runtimehome.CrashReport{crash(current, after)},
		},
		{
			name:        "latest of several crashes",
			records:     []runtimehome.DeployRecord{ship(), boot(newCommit, crashA, after), boot(newCommit, crashB, after)},
			running:     newCommit,
			crashes:     []runtimehome.CrashReport{crash(crashB, after.Add(2*time.Minute)), crash(crashA, after.Add(time.Minute))},
			want:        DeployCrashed,
			wantCrash:   crashB,
			wantCrashed: newCommit,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := EvaluateBoot(tc.records, tc.running, tc.crashes, current)
			if ok != (tc.want != "") {
				t.Fatalf("EvaluateBoot ok = %v, want announcement %q (got %+v)", ok, tc.want, got)
			}
			if !ok {
				return
			}
			if got.Outcome != tc.want || got.CrashFile != tc.wantCrash || got.CrashedCommit != tc.wantCrashed {
				t.Errorf("EvaluateBoot = %+v, want outcome %q crash %q crashed %q", got, tc.want, tc.wantCrash, tc.wantCrashed)
			}
			if got.Ship.Commit != newCommit {
				t.Errorf("announcement ship = %q, want latest ship", got.Ship.Commit)
			}
		})
	}
}

func TestEvaluateBootCrashLoopAnnouncesOnce(t *testing.T) {
	after := shipAt.Add(5 * time.Minute)
	records := []runtimehome.DeployRecord{ship(), boot(newCommit, crashA, after), boot(newCommit, crashB, after.Add(time.Minute))}
	crashes := []runtimehome.CrashReport{crash(crashA, after.Add(30*time.Second))}

	first, ok := EvaluateBoot(records, newCommit, crashes, crashB)
	if !ok || first.Outcome != DeployCrashed {
		t.Fatalf("first boot after crash: %+v, %v; want crash announcement", first, ok)
	}
	records = append(records, first.Record(after.Add(2*time.Minute)), boot(newCommit, current, after.Add(3*time.Minute)))
	crashes = append(crashes, crash(crashB, after.Add(90*time.Second)))

	if next, ok := EvaluateBoot(records, newCommit, crashes, current); ok {
		t.Fatalf("next boot announced again: %+v", next)
	}
}

func TestEvaluateBootSuccessThenLaterCrash(t *testing.T) {
	after := shipAt.Add(5 * time.Minute)
	records := []runtimehome.DeployRecord{ship(), boot(newCommit, crashA, after), reported(newCommit, ""), boot(newCommit, current, after.Add(time.Hour))}
	got, ok := EvaluateBoot(records, newCommit, []runtimehome.CrashReport{crash(crashA, after.Add(30*time.Minute))}, current)
	if !ok || got.Outcome != DeployCrashed || got.CrashFile != crashA {
		t.Fatalf("EvaluateBoot = %+v, %v; want crash of %s", got, ok, crashA)
	}
}

func TestAnnouncementFactsAndRecord(t *testing.T) {
	success := Announcement{Outcome: DeploySucceeded, Ship: ship(), Running: newCommit}
	facts := success.Facts("")
	t.Log(facts)
	for _, want := range []string{"2222222", `"fix thing"`, "<@7>", "is now running"} {
		if !strings.Contains(facts, want) {
			t.Errorf("success facts %q missing %q", facts, want)
		}
	}
	if strings.Contains(facts, "body") {
		t.Errorf("facts should carry only the summary line: %q", facts)
	}

	crashed := Announcement{Outcome: DeployCrashed, Ship: ship(), Running: newCommit, CrashFile: crashA, CrashedCommit: oldCommit}
	facts = crashed.Facts("panic: boom")
	t.Log(facts)
	for _, want := range []string{"1111111 crashed", "2222222", "panic: boom"} {
		if !strings.Contains(facts, want) {
			t.Errorf("crash facts %q missing %q", facts, want)
		}
	}

	rec := crashed.Record(shipAt)
	if rec.Kind != runtimehome.DeployReported || rec.Commit != newCommit || rec.CrashFile != crashA {
		t.Errorf("Record = %+v", rec)
	}
}

func TestDeployStatus(t *testing.T) {
	now := shipAt.Add(12 * time.Minute)
	tests := []struct {
		name    string
		records []runtimehome.DeployRecord
		running string
		now     time.Time
		want    string
	}{
		{name: "none", records: []runtimehome.DeployRecord{boot(oldCommit, "", shipAt)}, running: oldCommit, now: now, want: "none"},
		{name: "ok", records: []runtimehome.DeployRecord{ship()}, running: newCommit, now: now, want: "ok"},
		{name: "pending minutes", records: []runtimehome.DeployRecord{ship()}, running: oldCommit, now: now, want: "pending:2222222:12m"},
		{name: "pending hours", records: []runtimehome.DeployRecord{ship()}, running: oldCommit, now: shipAt.Add(3*time.Hour + 5*time.Minute), want: "pending:2222222:3h5m"},
		{name: "pending days", records: []runtimehome.DeployRecord{ship()}, running: oldCommit, now: shipAt.Add(50 * time.Hour), want: "pending:2222222:2d2h"},
		{name: "unknown running", records: []runtimehome.DeployRecord{ship()}, now: now, want: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeployStatus(tc.records, tc.running, tc.now); got != tc.want {
				t.Errorf("DeployStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCrashExcerpt(t *testing.T) {
	var frames strings.Builder
	for i := range 200 {
		frames.WriteString("main.handler(...)\n\t/src/internal/agent/rick.go:" + strings.Repeat("9", i%5+1) + " +0x1f\n")
	}
	trace := "some log noise\n\npanic: runtime error: invalid memory address\n[signal SIGSEGV]\n\ngoroutine 1 [running]:\n" + frames.String()

	tests := []struct {
		name      string
		in        string
		wantStart string
		truncated bool
	}{
		{name: "panic capped", in: trace, wantStart: "panic: runtime error", truncated: true},
		{name: "fatal short", in: "fatal error: concurrent map writes\n\ngoroutine 5 [running]:\nmain.f()\n", wantStart: "fatal error: concurrent map writes"},
		{name: "no marker", in: "something odd\nhappened\n", wantStart: "something odd"},
		{name: "huge first line", in: "panic: " + strings.Repeat("ä", 2000), wantStart: "panic: ", truncated: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CrashExcerpt(tc.in)
			if len(got) > maxExcerptBytes {
				t.Errorf("excerpt is %d bytes, cap %d", len(got), maxExcerptBytes)
			}
			if !strings.HasPrefix(got, tc.wantStart) {
				t.Errorf("excerpt starts %q, want prefix %q", got[:min(len(got), 40)], tc.wantStart)
			}
			if strings.HasSuffix(got, "(truncated)") != tc.truncated {
				t.Errorf("truncated marker = %v, want %v", !tc.truncated, tc.truncated)
			}
			if !utf8.ValidString(got) {
				t.Error("excerpt is not valid utf-8")
			}
		})
	}
}

func TestReadCrashExcerpt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot-1.txt")
	if err := os.WriteFile(path, []byte("panic: boom\n\ngoroutine 1 [running]:\nmain.main()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCrashExcerpt(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "panic: boom\ngoroutine 1 [running]:\nmain.main()"; got != want {
		t.Errorf("ReadCrashExcerpt = %q, want %q", got, want)
	}
}
