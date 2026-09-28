package client

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const DefaultOutputLimit = 4000

type Shell struct {
	workDir string
	timeout time.Duration
	cred    *syscall.Credential
}

// NewShell runs commands as username; if that user cannot be used it warns and runs as the current user.
func NewShell(workDir string, timeout time.Duration, username string) *Shell {
	cred, err := ShellCredential(username)
	if err != nil {
		slog.Warn("shell user unavailable, sys_shell runs as the bot user", "user", username, "error", err)
	}
	return &Shell{workDir: workDir, timeout: timeout, cred: cred}
}

// ShellCredential resolves username and proves this process can start children as that user.
func ShellCredential(username string) (*syscall.Credential, error) {
	cred, err := lookupCredential(username)
	if err != nil {
		return nil, err
	}
	probe := exec.Command("true")
	probe.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	if err := probe.Run(); err != nil {
		return nil, fmt.Errorf("start process as %q: %w", username, err)
	}
	return cred, nil
}

func lookupCredential(username string) (*syscall.Credential, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("uid of %q: %w", username, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("gid of %q: %w", username, err)
	}
	groupIDs, err := u.GroupIds()
	if err != nil {
		return nil, fmt.Errorf("groups of %q: %w", username, err)
	}
	groups := make([]uint32, 0, len(groupIDs))
	for _, g := range groupIDs {
		id, err := strconv.ParseUint(g, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("group id %q of %q: %w", g, username, err)
		}
		groups = append(groups, uint32(id))
	}
	return &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: groups}, nil
}

func (s *Shell) Exec(ctx context.Context, command string, outputLimit int) string {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	c := exec.CommandContext(ctx, "bash", "-c", "umask 002; "+command)
	c.Dir = s.workDir
	c.Env = []string{
		"PATH=" + filepath.Join(s.workDir, "go", "bin") + ":/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + s.workDir,
	}
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: s.cred}
	c.Cancel = func() error {
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	c.WaitDelay = 5 * time.Second

	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	err := c.Run()

	result := out.String()
	if outputLimit > 0 && len(result) > outputLimit {
		result = result[:outputLimit] + "... (truncated)"
	}
	if err != nil {
		if result != "" {
			result += "\n"
		}
		result += "error: " + err.Error()
	}
	if result == "" {
		return "(no output)"
	}
	return result
}
