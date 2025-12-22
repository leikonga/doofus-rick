package config

import (
	"os"
	"strings"
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
}

func LoadConfig() *Config {
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
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return fallback
}

func normalizeAddress(addr string) string {
	if !strings.HasPrefix(addr, ":") {
		return ":" + addr
	}
	return addr
}
