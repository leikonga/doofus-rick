package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/leikonga/doofus-rick/internal/config"
)

func writePrompt(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writePrompt: %v", err)
	}
	return path
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		config  func(t *testing.T) *config.Config
		wantErr bool
	}{
		{
			name: "good sqlite config",
			config: func(t *testing.T) *config.Config {
				return &config.Config{
					DBDriver:         "sqlite",
					DBPath:           filepath.Join(t.TempDir(), "check.db"),
					SystemPromptFile: writePrompt(t, "you are rick"),
				}
			},
			wantErr: false,
		},
		{
			name: "unreachable postgres",
			config: func(t *testing.T) *config.Config {
				return &config.Config{
					DBDriver:         "postgres",
					DBHost:           "127.0.0.1",
					DBPort:           "1",
					DBUser:           "nobody",
					DBPass:           "nobody",
					DBName:           "nobody",
					SystemPromptFile: writePrompt(t, "you are rick"),
				}
			},
			wantErr: true,
		},
		{
			name: "missing system prompt file",
			config: func(t *testing.T) *config.Config {
				return &config.Config{
					DBDriver:         "sqlite",
					DBPath:           filepath.Join(t.TempDir(), "check.db"),
					SystemPromptFile: filepath.Join(t.TempDir(), "does-not-exist.txt"),
				}
			},
			wantErr: true,
		},
		{
			name: "empty system prompt file",
			config: func(t *testing.T) *config.Config {
				return &config.Config{
					DBDriver:         "sqlite",
					DBPath:           filepath.Join(t.TempDir(), "check.db"),
					SystemPromptFile: writePrompt(t, "   \n\t  "),
				}
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := check(tc.config(t))
			if (err != nil) != tc.wantErr {
				t.Errorf("check() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
