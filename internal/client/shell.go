package client

import (
	"bytes"
	"context"
	"time"

	"os/exec"
)

const DefaultOutputLimit = 4000

type Shell struct {
	workDir string
	timeout time.Duration
}

func NewShell(workDir string, timeout time.Duration) *Shell {
	return &Shell{workDir: workDir, timeout: timeout}
}

func (s *Shell) Exec(ctx context.Context, command string, outputLimit int) string {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	c := exec.CommandContext(ctx, "bash", "-c", command)
	c.Dir = s.workDir
	c.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/rick/work"}

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
