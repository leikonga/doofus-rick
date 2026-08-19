package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DiscordToken string
	DiscordGuild string

	DiscordClientID     string
	DiscordClientSecret string
	DiscordRedirectURI  string

	DBDriver string
	DBPath   string
	DBHost   string
	DBUser   string
	DBPass   string
	DBName   string
	DBPort   string

	Port          string
	SessionSecret string

	OpenRouterAPIKey   string
	SystemPromptFile   string
	RickModel          string
	RickFallbackModels []string
	RickMaxTokens      int64
	CodeMaxTokens      int64
	CodeMaxToolIter    int
	ShellTimeout       string

	GiphyAPIKey string
	BraveAPIKey string

	WorkDir     string
	RickRepoDir string

	ArchiveEnabled      bool
	ArchiveDenyChannels string

	BackfillEnabled bool
	BackfillDelay   string
	BackfillBatch   int

	ChunkGap      string
	ChunkMaxMsgs  int
	ChunkMaxChars int

	RickEmbedModel string

	RecallEnabled        bool
	RecallTopK           int
	RecallMinScore       float64
	RecallNeighborChunks int

	AmbientEnabled      bool
	AmbientWindow       string
	AmbientMinMsgs      int
	AmbientMinAuthors   int
	AmbientCooldown     string
	AmbientDailyCap     int
	AmbientEvalDebounce string
	AmbientMinScore     int
	AmbientModel        string
	AmbientMaxTokens    int64

	AffinityEnabled     bool
	AffinityBaseline    int
	AffinityDecayPerDay float64
	AffinityModel       string

	TypingTheatre  bool
	TypingMaxDelay string
	TypingChance   float64
}

func LoadConfig() *Config {
	if os.Getenv("APP_ENV") != "production" {
		err := godotenv.Load()
		if err != nil {
			slog.Warn("failed to load env file", "error", err)
		}
	}

	return &Config{
		DiscordToken: getEnv("DISCORD_TOKEN", ""),
		DiscordGuild: getEnv("DISCORD_GUILD", ""),

		DiscordClientID:     getEnv("DISCORD_CLIENT_ID", ""),
		DiscordClientSecret: getEnv("DISCORD_CLIENT_SECRET", ""),
		DiscordRedirectURI:  getEnv("DISCORD_REDIRECT_URI", ""),

		DBDriver: getEnv("DB_DRIVER", "sqlite"),
		DBPath:   getEnv("DB_PATH", "doofus-rick.db"),
		DBHost:   getEnv("DB_HOST", "localhost"),
		DBUser:   getEnv("DB_USER", "postgres"),
		DBPass:   getEnv("DB_PASS", ""),
		DBName:   getEnv("DB_NAME", "postgres"),
		DBPort:   getEnv("DB_PORT", "5432"),

		Port:          normalizeAddress(getEnv("PORT", ":8080")),
		SessionSecret: getEnv("SESSION_SECRET", ""),

		OpenRouterAPIKey:   getEnv("OPENROUTER_API_KEY", ""),
		SystemPromptFile:   getEnv("SYSTEM_PROMPT_FILE", "system_prompt.txt"),
		RickModel:          getEnv("RICK_MODEL", "anthropic/claude-sonnet-5"),
		RickFallbackModels: getEnvList("RICK_FALLBACK_MODELS", nil),
		RickMaxTokens:      getEnvInt64("RICK_MAX_TOKENS", 512),
		CodeMaxTokens:      getEnvInt64("CODE_MAX_TOKENS", 4000),
		CodeMaxToolIter:    getEnvInt("CODE_MAX_TOOL_ITER", 24),
		ShellTimeout:       getEnv("SHELL_TIMEOUT", "120s"),

		GiphyAPIKey: getEnv("GIPHY_API_KEY", ""),
		BraveAPIKey: getEnv("BRAVE_API_KEY", ""),

		WorkDir:     getEnv("RICK_WORK_DIR", "/rick/work"),
		RickRepoDir: getEnv("RICK_REPO_DIR", "/rick/work/src"),

		ArchiveEnabled:      getEnvBool("ARCHIVE_ENABLED", true),
		ArchiveDenyChannels: getEnv("ARCHIVE_DENY_CHANNELS", ""),

		BackfillEnabled: getEnvBool("BACKFILL_ENABLED", false),
		BackfillDelay:   getEnv("BACKFILL_DELAY", "1s"),
		BackfillBatch:   getEnvInt("BACKFILL_BATCH", 100),

		ChunkGap:      getEnv("CHUNK_GAP", "10m"),
		ChunkMaxMsgs:  getEnvInt("CHUNK_MAX_MSGS", 15),
		ChunkMaxChars: getEnvInt("CHUNK_MAX_CHARS", 2000),

		RickEmbedModel: getEnv("RICK_EMBED_MODEL", "qwen/qwen3-embedding-8b"),

		RecallEnabled:        getEnvBool("RECALL_ENABLED", true),
		RecallTopK:           getEnvInt("RECALL_TOP_K", 3),
		RecallMinScore:       getEnvFloat64("RECALL_MIN_SCORE", 0.005),
		RecallNeighborChunks: getEnvInt("RECALL_NEIGHBOR_CHUNKS", 1),

		AmbientEnabled:      getEnvBool("AMBIENT_ENABLED", false),
		AmbientWindow:       getEnv("AMBIENT_WINDOW", "90s"),
		AmbientMinMsgs:      getEnvInt("AMBIENT_MIN_MSGS", 4),
		AmbientMinAuthors:   getEnvInt("AMBIENT_MIN_AUTHORS", 2),
		AmbientCooldown:     getEnv("AMBIENT_COOLDOWN", "60m"),
		AmbientDailyCap:     getEnvInt("AMBIENT_DAILY_CAP", 5),
		AmbientEvalDebounce: getEnv("AMBIENT_EVAL_DEBOUNCE", "60s"),
		AmbientMinScore:     getEnvInt("AMBIENT_MIN_SCORE", 90),
		AmbientModel:        getEnv("AMBIENT_MODEL", ""),
		AmbientMaxTokens:    getEnvInt64("AMBIENT_MAX_TOKENS", 120),

		AffinityEnabled:     getEnvBool("AFFINITY_ENABLED", true),
		AffinityBaseline:    getEnvInt("AFFINITY_BASELINE", -20),
		AffinityDecayPerDay: getEnvFloat64("AFFINITY_DECAY_PER_DAY", 0.10),
		AffinityModel:       getEnv("AFFINITY_MODEL", ""),

		TypingTheatre:  getEnvBool("TYPING_THEATRE", false),
		TypingMaxDelay: getEnv("TYPING_MAX_DELAY", "20s"),
		TypingChance:   getEnvFloat64("TYPING_CHANCE", 0.25),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
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
