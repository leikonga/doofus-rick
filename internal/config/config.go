package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	DiscordToken string
	DiscordGuild string

	DiscordClientID     string
	DiscordClientSecret string
	DiscordRedirectURI  string

	DBHost string
	DBUser string
	DBPass string
	DBName string
	DBPort string

	Port          string
	SessionSecret string

	OpenRouterAPIKey    string
	SystemPromptFile    string
	RickModel           string
	RickFallbackModels  []string
	RickMaxTokens       int64
	RickMaxToolIter     int
	RickReasoningEffort string
	CodeReasoningEffort string
	RickTurnTimeout     time.Duration
	CodeMaxTokens       int64
	CodeMaxToolIter     int
	ShellTimeout        time.Duration
	ShellUser           string
	PprofAddr           string

	GiphyAPIKey string
	BraveAPIKey string

	WorkDir     string
	RickRepoDir string
	BackupsDir  string

	GitHubToken    string
	GitAuthorName  string
	GitAuthorEmail string

	ArchiveEnabled      bool
	ArchiveDenyChannels string

	BackfillEnabled bool
	BackfillDelay   time.Duration
	BackfillBatch   int

	ChunkGap      time.Duration
	ChunkMaxMsgs  int
	ChunkMaxChars int

	RickEmbedModel string

	RecallEnabled        bool
	RecallTopK           int
	RecallMinSimilarity  float64
	RecallNeighborChunks int

	AmbientEnabled      bool
	AmbientWindow       time.Duration
	AmbientMinMsgs      int
	AmbientMinAuthors   int
	AmbientCooldown     time.Duration
	AmbientDailyCap     int
	AmbientEvalDebounce time.Duration
	AmbientMinScore     int
	AmbientModel        string
	AmbientMaxTokens    int64

	AffinityEnabled  bool
	AffinityBaseline int
	AffinityModel    string

	TypingTheatre  bool
	TypingMaxDelay time.Duration
	TypingChance   float64
}

func (c *Config) DSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		c.DBHost, c.DBUser, c.DBPass, c.DBName, c.DBPort)
}

func LoadConfig() *Config {
	if os.Getenv("APP_ENV") != "production" {
		err := godotenv.Load()
		if err != nil {
			slog.Warn("failed to load env file", "error", err)
		}
	}

	workDir := getEnv("RICK_WORK_DIR", "/rick/work")

	return &Config{
		DiscordToken: getEnv("DISCORD_TOKEN", ""),
		DiscordGuild: getEnv("DISCORD_GUILD", ""),

		DiscordClientID:     getEnv("DISCORD_CLIENT_ID", ""),
		DiscordClientSecret: getEnv("DISCORD_CLIENT_SECRET", ""),
		DiscordRedirectURI:  getEnv("DISCORD_REDIRECT_URI", ""),

		DBHost: getEnv("DB_HOST", "localhost"),
		DBUser: getEnv("DB_USER", "postgres"),
		DBPass: getEnv("DB_PASS", ""),
		DBName: getEnv("DB_NAME", "postgres"),
		DBPort: getEnv("DB_PORT", "5432"),

		Port:          normalizeAddress(getEnv("PORT", ":8080")),
		SessionSecret: getEnv("SESSION_SECRET", ""),

		OpenRouterAPIKey:    getEnv("OPENROUTER_API_KEY", ""),
		SystemPromptFile:    getEnv("SYSTEM_PROMPT_FILE", "system_prompt.txt"),
		RickModel:           getEnv("RICK_MODEL", "anthropic/claude-sonnet-5"),
		RickFallbackModels:  getEnvList("RICK_FALLBACK_MODELS", []string{"z-ai/glm-5.2"}),
		RickMaxTokens:       getEnvInt64("RICK_MAX_TOKENS", 16000),
		RickMaxToolIter:     getEnvInt("RICK_MAX_TOOL_ITER", 8),
		RickReasoningEffort: getEnv("RICK_REASONING_EFFORT", "medium"),
		CodeReasoningEffort: getEnv("CODE_REASONING_EFFORT", "high"),
		RickTurnTimeout:     getEnvDuration("RICK_TURN_TIMEOUT", 10*time.Minute),
		CodeMaxTokens:       getEnvInt64("CODE_MAX_TOKENS", 64000),
		CodeMaxToolIter:     getEnvInt("CODE_MAX_TOOL_ITER", 24),
		ShellTimeout:        getEnvDuration("SHELL_TIMEOUT", 120*time.Second),
		ShellUser:           getEnv("RICK_SHELL_USER", "rick"),
		PprofAddr:           getEnvAllowEmpty("RICK_PPROF_ADDR", "127.0.0.1:6060"),

		GiphyAPIKey: getEnv("GIPHY_API_KEY", ""),
		BraveAPIKey: getEnv("BRAVE_API_KEY", ""),

		WorkDir:     workDir,
		RickRepoDir: getEnv("RICK_REPO_DIR", "/rick/work/src"),
		BackupsDir:  getEnv("RICK_BACKUPS_DIR", filepath.Join(workDir, "backups")),

		GitHubToken:    getEnv("GITHUB_TOKEN", ""),
		GitAuthorName:  getEnv("RICK_GIT_AUTHOR_NAME", "doofus-rick"),
		GitAuthorEmail: getEnv("RICK_GIT_AUTHOR_EMAIL", "rick@localhost"),

		ArchiveEnabled:      getEnvBool("ARCHIVE_ENABLED", true),
		ArchiveDenyChannels: getEnv("ARCHIVE_DENY_CHANNELS", ""),

		BackfillEnabled: getEnvBool("BACKFILL_ENABLED", false),
		BackfillDelay:   getEnvDuration("BACKFILL_DELAY", time.Second),
		BackfillBatch:   getEnvInt("BACKFILL_BATCH", 100),

		ChunkGap:      getEnvDuration("CHUNK_GAP", 10*time.Minute),
		ChunkMaxMsgs:  getEnvInt("CHUNK_MAX_MSGS", 15),
		ChunkMaxChars: getEnvInt("CHUNK_MAX_CHARS", 2000),

		RickEmbedModel: getEnv("RICK_EMBED_MODEL", "qwen/qwen3-embedding-8b"),

		RecallEnabled:        getEnvBool("RECALL_ENABLED", true),
		RecallTopK:           getEnvInt("RECALL_TOP_K", 3),
		RecallMinSimilarity:  getEnvFloat64("RECALL_MIN_SIMILARITY", 0),
		RecallNeighborChunks: getEnvInt("RECALL_NEIGHBOR_CHUNKS", 1),

		AmbientEnabled:      getEnvBool("AMBIENT_ENABLED", false),
		AmbientWindow:       getEnvDuration("AMBIENT_WINDOW", 90*time.Second),
		AmbientMinMsgs:      getEnvInt("AMBIENT_MIN_MSGS", 4),
		AmbientMinAuthors:   getEnvInt("AMBIENT_MIN_AUTHORS", 2),
		AmbientCooldown:     getEnvDuration("AMBIENT_COOLDOWN", 60*time.Minute),
		AmbientDailyCap:     getEnvInt("AMBIENT_DAILY_CAP", 5),
		AmbientEvalDebounce: getEnvDuration("AMBIENT_EVAL_DEBOUNCE", 60*time.Second),
		AmbientMinScore:     getEnvInt("AMBIENT_MIN_SCORE", 90),
		AmbientModel:        getEnv("AMBIENT_MODEL", ""),
		AmbientMaxTokens:    getEnvInt64("AMBIENT_MAX_TOKENS", 8000),

		AffinityEnabled:  getEnvBool("AFFINITY_ENABLED", true),
		AffinityBaseline: getEnvInt("AFFINITY_BASELINE", -20),
		AffinityModel:    getEnv("AFFINITY_MODEL", ""),

		TypingTheatre:  getEnvBool("TYPING_THEATRE", false),
		TypingMaxDelay: getEnvDuration("TYPING_MAX_DELAY", 20*time.Second),
		TypingChance:   getEnvFloat64("TYPING_CHANCE", 0.25),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return fallback
}

func getEnvAllowEmpty(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvList(key string, fallback []string) []string {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func getEnvInt64(key string, fallback int64) int64 {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		slog.Warn("invalid int env var, using fallback", "key", key, "value", value, "error", err)
		return fallback
	}
	return parsed
}

func getEnvInt(key string, fallback int) int {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		slog.Warn("invalid int env var, using fallback", "key", key, "value", value, "error", err)
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		slog.Warn("invalid duration env var, using fallback", "key", key, "value", value, "error", err)
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		slog.Warn("invalid bool env var, using fallback", "key", key, "value", value, "error", err)
		return fallback
	}
	return parsed
}

func getEnvFloat64(key string, fallback float64) float64 {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		slog.Warn("invalid float env var, using fallback", "key", key, "value", value, "error", err)
		return fallback
	}
	return parsed
}

func normalizeAddress(addr string) string {
	if !strings.HasPrefix(addr, ":") {
		return ":" + addr
	}
	return addr
}
