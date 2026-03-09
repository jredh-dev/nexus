package config

import (
	"fmt"
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

// DBConfig holds PostgreSQL connection settings.
type DBConfig struct {
	Host     string // postgres hostname (default: localhost)
	Port     string // postgres port (default: 5432)
	Name     string // database name (default: portal)
	User     string // database role (default: portal)
	Password string // injected by Vault Agent via PORTAL_DB_PASSWORD
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
			Host:     getEnv("PORTAL_DB_HOST", "localhost"),
			Port:     getEnv("PORTAL_DB_PORT", "5432"),
			Name:     getEnv("PORTAL_DB_NAME", "portal"),
			User:     getEnv("PORTAL_DB_USER", "portal"),
			Password: getEnv("PORTAL_DB_PASSWORD", "portal-dev-password"),
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

// DSN returns a libpq-style connection string for pgxpool.
func (d DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s",
		d.Host, d.Port, d.Name, d.User, d.Password,
	)
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
