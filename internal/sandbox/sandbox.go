package sandbox

import (
	"bufio"
	_ "embed"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

//go:embed tools.txt
var manifest string

type Tool struct {
	Package  string
	Commands []string
	Purpose  string
}

func Manifest() ([]Tool, error) {
	return Parse(strings.NewReader(manifest))
}

func Parse(r io.Reader) ([]Tool, error) {
	var tools []Tool
	sc := bufio.NewScanner(r)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tool, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("tools manifest line %d: %w", lineNo, err)
		}
		tools = append(tools, tool)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read tools manifest: %w", err)
	}
	return tools, nil
}

func parseLine(line string) (Tool, error) {
	fields := strings.SplitN(line, ";", 3)
	if len(fields) != 3 {
		return Tool{}, fmt.Errorf("want \"package; commands; purpose\", got %q", line)
	}
	pkg := strings.TrimSpace(fields[0])
	if pkg == "" || strings.ContainsAny(pkg, " \t") {
		return Tool{}, fmt.Errorf("invalid package name %q", pkg)
	}
	purpose := strings.TrimSpace(fields[2])
	if purpose == "" {
		return Tool{}, fmt.Errorf("package %q has no purpose", pkg)
	}
	var commands []string
	for cmd := range strings.SplitSeq(fields[1], ",") {
		if cmd = strings.TrimSpace(cmd); cmd != "" {
			commands = append(commands, cmd)
		}
	}
	return Tool{Package: pkg, Commands: commands, Purpose: purpose}, nil
}

func Available(tools []Tool) []Tool {
	return availableWith(tools, exec.LookPath)
}

func availableWith(tools []Tool, lookPath func(string) (string, error)) []Tool {
	var out []Tool
	for _, t := range tools {
		if allFound(t.Commands, lookPath) {
			out = append(out, t)
		}
	}
	return out
}

func allFound(commands []string, lookPath func(string) (string, error)) bool {
	for _, cmd := range commands {
		if _, err := lookPath(cmd); err != nil {
			return false
		}
	}
	return true
}
