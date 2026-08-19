package selfcode

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ExecRunner runs commands via os/exec. If env is nil the child process
// inherits the current process environment, matching os/exec semantics.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
