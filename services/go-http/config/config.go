package config

import "os"

// Config holds service configuration.
type Config struct {
	Port string
}

// Load reads config from environment variables with sensible defaults.
// PORT (Cloud Run standard) takes precedence over SECRETS_PORT.
func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = os.Getenv("SECRETS_PORT")
	}
	if port == "" {
		port = "8082"
	}
	return &Config{Port: port}
}
