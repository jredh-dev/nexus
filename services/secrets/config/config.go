package config

import "os"

// Config holds service configuration.
type Config struct {
	Port string
}

// Load reads config from environment variables with sensible defaults.
func Load() *Config {
	port := os.Getenv("SECRETS_PORT")
	if port == "" {
		port = "8082"
	}
	return &Config{Port: port}
}
