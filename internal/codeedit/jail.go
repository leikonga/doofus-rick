package codeedit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveExisting evaluates symlinks on the deepest existing ancestor
// of path, then rejoins any remaining nonexistent components lexically.
func resolveExisting(path string) (string, error) {
	path = filepath.Clean(path)
	if _, err := os.Lstat(path); err == nil {
		return filepath.EvalSymlinks(path)
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", fmt.Errorf("resolve %q: no existing ancestor", path)
	}
	resolvedParent, err := resolveExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}

func isWithin(path, root string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// resolve maps a caller-supplied path to a real filesystem path,
// rejecting traversal, absolute escapes, and symlinks out of root.
func (e *Editor) resolve(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}

	var candidate string
	if filepath.IsAbs(path) {
		candidate = filepath.Clean(path)
	} else {
		candidate = filepath.Clean(filepath.Join(e.root, path))
	}
	if !isWithin(candidate, e.root) {
		return "", fmt.Errorf("path %q escapes jail root %q", path, e.root)
	}

	resolved, err := resolveExisting(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	if !isWithin(resolved, e.root) {
		return "", fmt.Errorf("path %q escapes jail root %q via symlink", path, e.root)
	}

	return candidate, nil
}
