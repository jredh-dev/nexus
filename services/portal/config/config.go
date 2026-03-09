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
	SMTP    SMTPConfig
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

// SMTPConfig holds outbound email settings.
type SMTPConfig struct {
	Host string // SMTP server hostname (e.g. "mailpit" in Docker, "smtp.sendgrid.net" in prod)
	Port string // SMTP server port (default: "1025" for Mailpit, "587" for prod)
	From string // From address for outbound email
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
		SMTP: SMTPConfig{
			Host: getEnv("SMTP_HOST", "localhost"),
			Port: getEnv("SMTP_PORT", "1025"),
			From: getEnv("SMTP_FROM", "noreply@jredh.com"),
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
