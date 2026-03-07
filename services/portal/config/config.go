package config

import (
	"os"
	"strconv"
)

// Config holds all application configuration.
type Config struct {
	Server  ServerConfig
	DB      DBConfig
	Session SessionConfig
	JWT     JWTConfig
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port string
	Env  string
}

// DBConfig holds database settings.
type DBConfig struct {
	Path string // path to SQLite database file
}

// SessionConfig holds session/cookie settings.
type SessionConfig struct {
	Secret string // HMAC key for signing session cookies
	MaxAge int    // session duration in seconds (default: 7 days)
}

// JWTConfig holds settings for cross-service JWT tokens.
type JWTConfig struct {
	// SigningKey is the HMAC-SHA256 secret shared across all nexus services.
	// Loaded from JWT_SIGNING_KEY env var (GCP secret: jwt-signing-key-dev).
	// Empty in development — portal still mints tokens but Domain= is omitted.
	SigningKey string

	// CookieDomain is the domain attribute set on the "token" cookie.
	// In production this should be "jredh.com" so all *.jredh.com subdomains
	// receive the cookie. Empty in development (cookie is host-scoped).
	CookieDomain string
}

// Load returns application configuration from environment variables.
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnv("PORT", "8080"),
			Env:  getEnv("ENV", "development"),
		},
		DB: DBConfig{
			Path: getEnv("DB_PATH", "portal.db"),
		},
		Session: SessionConfig{
			Secret: getEnv("SESSION_SECRET", ""),
			MaxAge: getEnvInt("SESSION_MAX_AGE", 604800), // 7 days
		},
		JWT: JWTConfig{
			SigningKey:   getEnv("JWT_SIGNING_KEY", ""),
			CookieDomain: getEnv("JWT_COOKIE_DOMAIN", ""),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
