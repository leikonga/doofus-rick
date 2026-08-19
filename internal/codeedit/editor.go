package codeedit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const noOutput = "(no output)"

// Editor reads and edits files inside a jailed directory tree.
type Editor struct {
	root string
}

// New resolves root to its real, absolute path and returns an Editor
// jailed to it. root must exist and be a directory.
func New(root string) (*Editor, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root %q: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve root %q: %w", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root %q is not a directory", root)
	}
	return &Editor{root: resolved}, nil
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	trimmed := strings.TrimSuffix(content, "\n")
	return strings.Split(trimmed, "\n")
}

// Read returns the file content cat -n style. offset skips leading
// lines (0-based); limit caps lines returned, 0 meaning no cap.
func (e *Editor) Read(path string, offset, limit int) (string, error) {
	resolved, err := e.resolve(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", path, err)
	}

	lines := splitLines(string(data))
	if offset < 0 {
		offset = 0
	}
	if offset >= len(lines) {
		return noOutput, nil
	}
	end := len(lines)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}

	var sb strings.Builder
	for i := offset; i < end; i++ {
		fmt.Fprintf(&sb, "%6d\t%s\n", i+1, lines[i])
	}
	return sb.String(), nil
}

// Write creates or overwrites path with content. The parent directory
// must already exist.
func (e *Editor) Write(path, content string) error {
	resolved, err := e.resolve(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(resolved)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("write %q: parent directory: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("write %q: %q is not a directory", path, dir)
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

// Insert adds text as a new line after line (0 = beginning); a line
// number past EOF appends at the end.
func (e *Editor) Insert(path string, line int, text string) error {
	if line < 0 {
		return fmt.Errorf("insert %q: line %d is negative", path, line)
	}
	resolved, err := e.resolve(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("insert %q: %w", path, err)
	}

	lines := splitLines(string(data))
	if line > len(lines) {
		line = len(lines)
	}

	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:line]...)
	out = append(out, text)
	out = append(out, lines[line:]...)

	newContent := joinLines(out, strings.HasSuffix(string(data), "\n"))
	if err := os.WriteFile(resolved, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("insert %q: %w", path, err)
	}
	return nil
}

func joinLines(lines []string, trailingNewline bool) string {
	content := strings.Join(lines, "\n")
	if trailingNewline {
		content += "\n"
	}
	return content
}
