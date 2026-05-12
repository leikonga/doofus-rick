package bot

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

const (
	shellExecTimeout = 10 * time.Second
	shellOutputLimit = 2000
)

func (b *Bot) shellExec(ctx context.Context, command string) string {
	ctx, cancel := context.WithTimeout(ctx, shellExecTimeout)
	defer cancel()

	c := exec.CommandContext(ctx, "sh", "-c", command)
	c.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}

	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	c.Run()

	result := out.String()
	if len(result) > shellOutputLimit {
		result = result[:shellOutputLimit] + "... (truncated)"
	}
	if result == "" {
		return "(no output)"
	}
	return result
}
