package runtimehome

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

const sharedPermsMarker = ".perms-v1"

const keptModeBits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// ShareWithGroup gives gid read/write on everything under workDir once, recorded by a marker file.
// A negative gid leaves group ownership unchanged.
func ShareWithGroup(workDir string, gid int) error {
	marker := filepath.Join(workDir, "runtime", sharedPermsMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	}

	walkErr := filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("perms migration: cannot read path", "path", path, "error", err)
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		shareEntry(path, d, gid)
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("walk %s: %w", workDir, walkErr)
	}

	if err := os.MkdirAll(filepath.Dir(marker), 0o775); err != nil {
		return fmt.Errorf("create marker dir: %w", err)
	}
	if err := os.WriteFile(marker, nil, 0o664); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}
	return nil
}

func shareEntry(path string, d fs.DirEntry, gid int) {
	if gid >= 0 {
		if err := os.Lchown(path, -1, gid); err != nil {
			slog.Warn("perms migration: chgrp failed", "path", path, "error", err)
		}
	}
	// Stat after chgrp: the kernel clears setgid on files when their group changes.
	info, err := os.Lstat(path)
	if err != nil {
		slog.Warn("perms migration: stat failed", "path", path, "error", err)
		return
	}
	mode := info.Mode() & keptModeBits
	if d.IsDir() {
		mode |= 0o070 | fs.ModeSetgid
	} else {
		mode |= 0o060
	}
	if mode == info.Mode()&keptModeBits {
		return
	}
	if err := os.Chmod(path, mode); err != nil {
		slog.Warn("perms migration: chmod failed", "path", path, "error", err)
	}
}
