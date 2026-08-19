package codeedit

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type lineMatch struct {
	start, end int // 0-based line indices, end exclusive
}

func findAllOffsets(content, old string) []int {
	var offsets []int
	pos := 0
	for {
		idx := strings.Index(content[pos:], old)
		if idx < 0 {
			break
		}
		offsets = append(offsets, pos+idx)
		pos += idx + len(old)
	}
	return offsets
}

func lineIndexAtOffset(content string, offset int) int {
	return strings.Count(content[:offset], "\n")
}

func offsetsToLineMatches(content, old string, offsets []int) []lineMatch {
	matches := make([]lineMatch, len(offsets))
	for i, off := range offsets {
		start := lineIndexAtOffset(content, off)
		end := lineIndexAtOffset(content, off+len(old)) + 1
		if end <= start {
			end = start + 1
		}
		matches[i] = lineMatch{start: start, end: end}
	}
	return matches
}

func linesEqualTrimmed(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

func findWhitespaceDriftMatches(lines, oldLines []string) []lineMatch {
	var matches []lineMatch
	for i := 0; i+len(oldLines) <= len(lines); i++ {
		if linesEqualTrimmed(lines[i:i+len(oldLines)], oldLines) {
			matches = append(matches, lineMatch{start: i, end: i + len(oldLines)})
		}
	}
	return matches
}

func multiMatchError(path string, lines []string, matches []lineMatch) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d matches for the given string in %s; add more surrounding context to disambiguate:\n", len(matches), path)
	for _, m := range matches {
		fmt.Fprintf(&sb, "\n  lines %d-%d:\n", m.start+1, m.end)
		ctxStart := m.start - 1
		if ctxStart < 0 {
			ctxStart = 0
		}
		ctxEnd := m.end + 1
		if ctxEnd > len(lines) {
			ctxEnd = len(lines)
		}
		for i := ctxStart; i < ctxEnd; i++ {
			fmt.Fprintf(&sb, "    %d: %s\n", i+1, lines[i])
		}
	}
	return errors.New(sb.String())
}

// Replace substitutes old with new in path: exact match first, falling
// back to a per-line whitespace-insensitive match.
func (e *Editor) Replace(path, old, new string, replaceAll bool) (int, error) {
	if old == "" {
		return 0, fmt.Errorf("replace %q: old string is empty", path)
	}
	resolved, err := e.resolve(path)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return 0, fmt.Errorf("replace %q: %w", path, err)
	}
	content := string(data)

	if offsets := findAllOffsets(content, old); len(offsets) > 0 {
		if len(offsets) > 1 && !replaceAll {
			return 0, multiMatchError(path, splitLines(content), offsetsToLineMatches(content, old, offsets))
		}
		n := 1
		newContent := strings.Replace(content, old, new, 1)
		if replaceAll {
			n = len(offsets)
			newContent = strings.ReplaceAll(content, old, new)
		}
		if err := os.WriteFile(resolved, []byte(newContent), 0o644); err != nil {
			return 0, fmt.Errorf("replace %q: %w", path, err)
		}
		return n, nil
	}

	lines := splitLines(content)
	oldLines := splitLines(old)
	matches := findWhitespaceDriftMatches(lines, oldLines)
	if len(matches) == 0 {
		return 0, fmt.Errorf("no match for %q in %s", old, path)
	}
	if len(matches) > 1 && !replaceAll {
		return 0, multiMatchError(path, lines, matches)
	}

	newLines := splitLines(new)
	var out []string
	n := 0
	if replaceAll {
		last := 0
		for _, m := range matches {
			out = append(out, lines[last:m.start]...)
			out = append(out, newLines...)
			last = m.end
			n++
		}
		out = append(out, lines[last:]...)
	} else {
		m := matches[0]
		out = append(out, lines[:m.start]...)
		out = append(out, newLines...)
		out = append(out, lines[m.end:]...)
		n = 1
	}

	if err := os.WriteFile(resolved, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		return 0, fmt.Errorf("replace %q: %w", path, err)
	}
	return n, nil
}
