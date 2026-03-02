// Package config provides a minimal config loader for go-http services.
package config

import "os"

// Config holds service configuration.
type Config struct {
	Port string
}

// Load reads config from environment variables with sensible defaults.
// PORT (Cloud Run standard) is checked first, then SERVICE_PORT.
func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = os.Getenv("SERVICE_PORT")
	}
	if port == "" {
		port = "8080"
	}
	return &Config{Port: port}
}
