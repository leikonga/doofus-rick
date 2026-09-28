package runtimehome

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestShareWithGroup(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "src", "pkg"), 0o755)
	mustWrite(t, filepath.Join(root, "src", "main.go"), 0o644)
	mustWrite(t, filepath.Join(root, "src", "pkg", "obj"), 0o444)
	mustWrite(t, filepath.Join(root, "run.sh"), 0o755)
	target := filepath.Join(t.TempDir(), "outside")
	mustWrite(t, target, 0o600)
	if err := os.Symlink(target, filepath.Join(root, "src", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ShareWithGroup(root, os.Getgid()); err != nil {
		t.Fatalf("ShareWithGroup: %v", err)
	}

	tests := []struct {
		path string
		want fs.FileMode
	}{
		{"", fs.ModeDir | fs.ModeSetgid | 0o775},
		{"src", fs.ModeDir | fs.ModeSetgid | 0o775},
		{"src/pkg", fs.ModeDir | fs.ModeSetgid | 0o775},
		{"src/main.go", 0o664},
		{"src/pkg/obj", 0o464},
		{"run.sh", 0o775},
	}
	for _, tt := range tests {
		info, err := os.Lstat(filepath.Join(root, tt.path))
		if err != nil {
			t.Fatalf("stat %q: %v", tt.path, err)
		}
		if got := info.Mode(); got != tt.want {
			t.Errorf("%q mode = %v, want %v", tt.path, got, tt.want)
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Gid) != os.Getgid() {
			t.Errorf("%q gid = %d, want %d", tt.path, st.Gid, os.Getgid())
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("symlink target mode = %v, want untouched 0600", got)
	}
	if _, err := os.Stat(filepath.Join(root, "runtime", sharedPermsMarker)); err != nil {
		t.Fatalf("marker missing: %v", err)
	}
}

func TestShareWithGroupRunsOnce(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "notes.txt")
	mustWrite(t, file, 0o644)

	if err := ShareWithGroup(root, -1); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ShareWithGroup(root, -1); err != nil {
		t.Fatalf("second run: %v", err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("second run changed mode to %v, want untouched 0644", got)
	}
}

func mustMkdir(t *testing.T, path string, perm fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string, perm fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatal(err)
	}
}
