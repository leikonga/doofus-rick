package shell

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const missingUser = "rick-test-no-such-user"

func TestShellFallsBackWhenUserMissing(t *testing.T) {
	if _, err := Credential(missingUser); err == nil {
		t.Fatalf("ShellCredential(%q) succeeded, want error", missingUser)
	}
	s := New(t.TempDir(), 5*time.Second, missingUser)
	if s.cred != nil {
		t.Fatalf("cred = %+v, want nil fallback", s.cred)
	}
	got := strings.TrimSpace(s.Exec(context.Background(), "id -u", 0))
	if want := strconv.Itoa(os.Getuid()); got != want {
		t.Fatalf("id -u = %q, want %q", got, want)
	}
}

func TestLookupCredentialCurrentUser(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Skipf("current user: %v", err)
	}
	cred, err := lookupCredential(current.Username)
	if err != nil {
		t.Fatalf("lookupCredential: %v", err)
	}
	if int(cred.Uid) != os.Getuid() || int(cred.Gid) != os.Getgid() {
		t.Fatalf("cred uid/gid = %d/%d, want %d/%d", cred.Uid, cred.Gid, os.Getuid(), os.Getgid())
	}
}

func TestShellUmaskAndEnv(t *testing.T) {
	dir := t.TempDir()
	s := &Runner{workDir: dir, timeout: 5 * time.Second}
	got := s.Exec(context.Background(), "umask; echo $HOME; pwd", 0)
	want := "0002\n" + dir + "\n" + dir + "\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestShellTimeoutKillsBackgroundChildren(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "bg.pid")
	s := &Runner{workDir: dir, timeout: 300 * time.Millisecond}

	start := time.Now()
	out := s.Exec(context.Background(), "sleep 30 & echo $! > bg.pid; sleep 30", 0)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Exec took %s, want timeout kill", elapsed)
	}
	if !strings.Contains(out, "killed") {
		t.Fatalf("output %q, want killed error", out)
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse pid: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("background child %d still alive", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func processAlive(pid int) bool {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	_, afterComm, ok := strings.Cut(string(stat), ") ")
	return ok && !strings.HasPrefix(afterComm, "Z")
}
