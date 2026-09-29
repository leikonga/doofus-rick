package config

import (
	"os"
	"testing"
	"time"
)

func TestGetEnvDuration(t *testing.T) {
	const key = "TEST_ENV_DURATION"
	fallback := 5 * time.Second
	tests := []struct {
		name  string
		set   bool
		value string
		want  time.Duration
	}{
		{"unset", false, "", fallback},
		{"empty", true, "", fallback},
		{"invalid", true, "abc", fallback},
		{"zero", true, "0", 0},
		{"compound", true, "2m30s", 2*time.Minute + 30*time.Second},
		{"negative", true, "-1s", -time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnv(t, key)
			if tt.set {
				t.Setenv(key, tt.value)
			}
			if got := getEnvDuration(key, fallback); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadConfigDurationDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	keys := []string{"CHUNK_GAP", "BACKFILL_DELAY", "AMBIENT_WINDOW", "AMBIENT_COOLDOWN", "AMBIENT_EVAL_DEBOUNCE", "TYPING_MAX_DELAY", "RICK_TURN_TIMEOUT", "SHELL_TIMEOUT"}
	for _, k := range keys {
		unsetEnv(t, k)
	}
	c := LoadConfig()
	got := map[string]time.Duration{
		"ChunkGap":            c.ChunkGap,
		"BackfillDelay":       c.BackfillDelay,
		"AmbientWindow":       c.AmbientWindow,
		"AmbientCooldown":     c.AmbientCooldown,
		"AmbientEvalDebounce": c.AmbientEvalDebounce,
		"TypingMaxDelay":      c.TypingMaxDelay,
		"RickTurnTimeout":     c.RickTurnTimeout,
		"ShellTimeout":        c.ShellTimeout,
	}
	want := map[string]time.Duration{
		"ChunkGap":            10 * time.Minute,
		"BackfillDelay":       time.Second,
		"AmbientWindow":       90 * time.Second,
		"AmbientCooldown":     60 * time.Minute,
		"AmbientEvalDebounce": 60 * time.Second,
		"TypingMaxDelay":      20 * time.Second,
		"RickTurnTimeout":     10 * time.Minute,
		"ShellTimeout":        120 * time.Second,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: got %v, want %v", name, got[name], w)
		}
	}
}

func TestLoadConfigPprofAddr(t *testing.T) {
	t.Setenv("APP_ENV", "production")

	unsetEnv(t, "RICK_PPROF_ADDR")
	if got := LoadConfig().PprofAddr; got != "127.0.0.1:6060" {
		t.Errorf("unset: got %q", got)
	}

	t.Setenv("RICK_PPROF_ADDR", "")
	if got := LoadConfig().PprofAddr; got != "" {
		t.Errorf("empty: got %q, want empty", got)
	}
}

func TestDSN(t *testing.T) {
	c := &Config{DBHost: "db.example", DBUser: "rick", DBPass: "s3cret", DBName: "ricks", DBPort: "5433"}
	want := "host=db.example user=rick password=s3cret dbname=ricks port=5433 sslmode=disable"
	if got := c.DSN(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
