package codeedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, name, content string) string {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func TestNew(t *testing.T) {
	root := t.TempDir()
	if _, err := New(root); err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}
	if _, err := New(filepath.Join(root, "does-not-exist")); err == nil {
		t.Fatal("New() on nonexistent root: expected error, got nil")
	}
}

func TestJailEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "top secret\n")

	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cases := []struct {
		name string
		path string
	}{
		{"relative traversal", "../secret.txt"},
		{"absolute escape", filepath.Join(outside, "secret.txt")},
		{"symlink escape", "link/secret.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ed.Read(tc.path, 0, 0); err == nil {
				t.Fatalf("Read(%q): expected jail error, got nil", tc.path)
			}
		})
	}
}

func TestRead(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "one\ntwo\nthree\nfour\nfive\n")
	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Run("full file", func(t *testing.T) {
		got, err := ed.Read("f.txt", 0, 0)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		for _, want := range []string{"1\tone", "2\ttwo", "5\tfive"} {
			if !strings.Contains(got, want) {
				t.Errorf("Read() = %q, want substring %q", got, want)
			}
		}
	})

	t.Run("offset skips leading lines", func(t *testing.T) {
		got, err := ed.Read("f.txt", 2, 0)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if strings.Contains(got, "one") || strings.Contains(got, "two") {
			t.Errorf("Read() with offset=2 should not contain first two lines, got %q", got)
		}
		if !strings.Contains(got, "3\tthree") {
			t.Errorf("Read() with offset=2 = %q, want to contain line 3", got)
		}
	})

	t.Run("limit caps output", func(t *testing.T) {
		got, err := ed.Read("f.txt", 0, 2)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if !strings.Contains(got, "1\tone") || !strings.Contains(got, "2\ttwo") {
			t.Errorf("Read() with limit=2 missing expected lines, got %q", got)
		}
		if strings.Contains(got, "three") {
			t.Errorf("Read() with limit=2 should not contain line 3, got %q", got)
		}
	})

	t.Run("offset and limit combined", func(t *testing.T) {
		got, err := ed.Read("f.txt", 1, 2)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if !strings.Contains(got, "2\ttwo") || !strings.Contains(got, "3\tthree") {
			t.Errorf("Read() offset=1 limit=2 = %q, want lines 2-3", got)
		}
		if strings.Contains(got, "one") || strings.Contains(got, "four") {
			t.Errorf("Read() offset=1 limit=2 = %q, want only lines 2-3", got)
		}
	})

	t.Run("offset past EOF returns placeholder", func(t *testing.T) {
		got, err := ed.Read("f.txt", 100, 0)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if got != "(no output)" {
			t.Errorf("Read() past EOF = %q, want %q", got, "(no output)")
		}
	})

	t.Run("empty file returns placeholder", func(t *testing.T) {
		writeFile(t, root, "empty.txt", "")
		got, err := ed.Read("empty.txt", 0, 0)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if got != "(no output)" {
			t.Errorf("Read() on empty file = %q, want %q", got, "(no output)")
		}
	})

	t.Run("nonexistent file errors", func(t *testing.T) {
		if _, err := ed.Read("nope.txt", 0, 0); err == nil {
			t.Fatal("Read() on nonexistent file: expected error, got nil")
		}
	})
}

func TestWrite(t *testing.T) {
	root := t.TempDir()
	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Run("creates a new file", func(t *testing.T) {
		if err := ed.Write("new.txt", "hello\n"); err != nil {
			t.Fatalf("Write: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(root, "new.txt"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(got) != "hello\n" {
			t.Errorf("file content = %q, want %q", got, "hello\n")
		}
	})

	t.Run("nonexistent directory errors", func(t *testing.T) {
		if err := ed.Write("missing-dir/new.txt", "hello\n"); err == nil {
			t.Fatal("Write() into nonexistent directory: expected error, got nil")
		}
	})
}

func TestReplaceExactMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "foo\nbar\nbaz\n")
	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	n, err := ed.Replace("f.txt", "bar", "qux", false)
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if n != 1 {
		t.Fatalf("Replace() = %d, want 1", n)
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != "foo\nqux\nbaz\n" {
		t.Errorf("file content = %q, want %q", got, "foo\nqux\nbaz\n")
	}
}

func TestReplaceWhitespaceDrift(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "func x() {\n    return 1\n}\n")
	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// old has different indentation than the file, exact match fails, whitespace-trimmed match succeeds.
	n, err := ed.Replace("f.txt", "  return 1", "  return 2", false)
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if n != 1 {
		t.Fatalf("Replace() = %d, want 1", n)
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if !strings.Contains(string(got), "return 2") {
		t.Errorf("file content = %q, want to contain %q", got, "return 2")
	}
}

func TestReplaceWhitespaceDriftPreservesMissingTrailingNewline(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "func x() {\n    return 1\n}")
	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := ed.Replace("f.txt", "  return 1", "  return 2", false); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	want := "func x() {\n    return 2\n}"
	if string(got) != want {
		t.Errorf("file content = %q, want %q", got, want)
	}
}

func TestReplaceZeroMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "foo\nbar\n")
	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = ed.Replace("f.txt", "nonexistent-snippet", "x", false)
	if err == nil {
		t.Fatal("Replace() with zero matches: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "f.txt") {
		t.Errorf("Replace() error = %q, want to name the file", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no match") {
		t.Errorf("Replace() error = %q, want to state no match", err)
	}
}

func TestReplaceMultiMatchWithoutReplaceAll(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "one\ndup\ntwo\ndup\nthree\n")
	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = ed.Replace("f.txt", "dup", "x", false)
	if err == nil {
		t.Fatal("Replace() with multiple matches and replaceAll=false: expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "2") {
		t.Errorf("Replace() error = %q, want to state the match count", msg)
	}
	if !strings.Contains(msg, "2:") && !strings.Contains(msg, "line 2") {
		t.Errorf("Replace() error = %q, want to quote line number 2", msg)
	}
	if !strings.Contains(msg, "4:") && !strings.Contains(msg, "line 4") {
		t.Errorf("Replace() error = %q, want to quote line number 4", msg)
	}
	if !strings.Contains(msg, "one") && !strings.Contains(msg, "two") && !strings.Contains(msg, "three") {
		t.Errorf("Replace() error = %q, want to include surrounding context", msg)
	}

	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != "one\ndup\ntwo\ndup\nthree\n" {
		t.Errorf("file should be unmodified after ambiguous replace, got %q", got)
	}
}

func TestReplaceAllMultiMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "f.txt", "one\ndup\ntwo\ndup\nthree\n")
	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	n, err := ed.Replace("f.txt", "dup", "x", true)
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if n != 2 {
		t.Fatalf("Replace() = %d, want 2", n)
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != "one\nx\ntwo\nx\nthree\n" {
		t.Errorf("file content = %q, want %q", got, "one\nx\ntwo\nx\nthree\n")
	}
}

func TestReplaceNonexistentFile(t *testing.T) {
	root := t.TempDir()
	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := ed.Replace("nope.txt", "a", "b", false); err == nil {
		t.Fatal("Replace() on nonexistent file: expected error, got nil")
	}
}

func TestInsert(t *testing.T) {
	root := t.TempDir()
	ed, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Run("line 0 prepends", func(t *testing.T) {
		writeFile(t, root, "a.txt", "one\ntwo\n")
		if err := ed.Insert("a.txt", 0, "zero"); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
		if string(got) != "zero\none\ntwo\n" {
			t.Errorf("file content = %q, want %q", got, "zero\none\ntwo\n")
		}
	})

	t.Run("mid-file insert", func(t *testing.T) {
		writeFile(t, root, "b.txt", "one\ntwo\nthree\n")
		if err := ed.Insert("b.txt", 1, "one-point-five"); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(root, "b.txt"))
		if string(got) != "one\none-point-five\ntwo\nthree\n" {
			t.Errorf("file content = %q, want %q", got, "one\none-point-five\ntwo\nthree\n")
		}
	})

	t.Run("past EOF appends", func(t *testing.T) {
		writeFile(t, root, "c.txt", "one\ntwo\n")
		if err := ed.Insert("c.txt", 100, "end"); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(root, "c.txt"))
		if string(got) != "one\ntwo\nend\n" {
			t.Errorf("file content = %q, want %q", got, "one\ntwo\nend\n")
		}
	})

	t.Run("preserves missing trailing newline", func(t *testing.T) {
		writeFile(t, root, "e.txt", "one\ntwo")
		if err := ed.Insert("e.txt", 1, "mid"); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(root, "e.txt"))
		if string(got) != "one\nmid\ntwo" {
			t.Errorf("file content = %q, want %q", got, "one\nmid\ntwo")
		}
	})

	t.Run("nonexistent file errors", func(t *testing.T) {
		if err := ed.Insert("nope.txt", 0, "x"); err == nil {
			t.Fatal("Insert() on nonexistent file: expected error, got nil")
		}
	})

	t.Run("negative line errors", func(t *testing.T) {
		writeFile(t, root, "d.txt", "one\n")
		if err := ed.Insert("d.txt", -1, "x"); err == nil {
			t.Fatal("Insert() with negative line: expected error, got nil")
		}
	})
}
