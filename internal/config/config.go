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

	OpenRouterAPIKey string
	SystemPromptFile string
	RickModel        string
	RickMaxTokens    int64

	GiphyAPIKey string
	BraveAPIKey string

	WorkDir string
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

		OpenRouterAPIKey: getEnv("OPENROUTER_API_KEY", ""),
		SystemPromptFile: getEnv("SYSTEM_PROMPT_FILE", "system_prompt.txt"),
		RickModel:        getEnv("RICK_MODEL", "anthropic/claude-sonnet-5"),
		RickMaxTokens:    getEnvInt64("RICK_MAX_TOKENS", 512),

		GiphyAPIKey: getEnv("GIPHY_API_KEY", ""),
		BraveAPIKey: getEnv("BRAVE_API_KEY", ""),

		WorkDir: getEnv("RICK_WORK_DIR", "/rick/work"),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return fallback
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

func normalizeAddress(addr string) string {
	if !strings.HasPrefix(addr, ":") {
		return ":" + addr
	}
	return addr
}
