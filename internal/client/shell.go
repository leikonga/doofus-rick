package client

import (
	"bytes"
	"context"
	"time"

	"os/exec"
)

const (
	shellExecTimeout = 45 * time.Second
	shellOutputLimit = 4000
)

type Shell struct {
	workDir string
}

func NewShell(workDir string) *Shell {
	return &Shell{workDir: workDir}
}

func (s *Shell) Exec(ctx context.Context, command string) string {
	ctx, cancel := context.WithTimeout(ctx, shellExecTimeout)
	defer cancel()

	c := exec.CommandContext(ctx, "bash", "-c", command)
	c.Dir = s.workDir
	c.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/rick/work"}

	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	err := c.Run()

	result := out.String()
	if len(result) > shellOutputLimit {
		result = result[:shellOutputLimit] + "... (truncated)"
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
