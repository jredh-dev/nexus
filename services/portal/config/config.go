package config

import (
	"os"
	"strconv"
)

// Config holds all application configuration
type Config struct {
	Server   ServerConfig
	Firebase FirebaseConfig
	GitHub   GitHubConfig
	Slack    SlackConfig
	Stripe   StripeConfig
	Auth     AuthConfig
	JWT      JWTConfig
}

type ServerConfig struct {
	Port string
	Env  string
}

type FirebaseConfig struct {
	ProjectID         string
	CredentialsPath   string
	FirestoreDatabase string
	// Emulator support for integration testing
	UseEmulator           bool
	EmulatorAuthHost      string
	EmulatorFirestoreHost string
}

type GitHubConfig struct {
	Organization string
	Token        string
}

type SlackConfig struct {
	WebhookURL string
	BotToken   string
}

type StripeConfig struct {
	SecretKey      string
	PublishableKey string
	WebhookSecret  string
}

type AuthConfig struct {
	SessionCookieMaxAge         int  // seconds (default: 3600 = 1 hour)
	RememberMeMaxAge            int  // seconds (default: 604800 = 1 week)
	RequireEmailVerification    bool // require email verification before login
	MockVerificationMode        bool // use mock email/SMS verification (dev only)
	InviteExpirationDays        int  // invite link expiration (default: 7 days)
	VerificationExpirationHours int  // email verification expiration (default: 24 hours)
	SMSExpirationMinutes        int  // SMS code expiration (default: 10 minutes)
	SMSMaxAttempts              int  // max SMS verification attempts (default: 3)
}

type JWTConfig struct {
	SigningKey string // Secret key for JWT signing
	Issuer     string // JWT issuer claim
}

// Load returns application configuration from environment variables
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnv("PORT", "8080"),
			Env:  getEnv("ENV", "development"),
		},
		Firebase: FirebaseConfig{
			ProjectID:             getEnv("FIREBASE_PROJECT_ID", ""),
			CredentialsPath:       getEnv("FIREBASE_CREDENTIALS_PATH", ""),
			FirestoreDatabase:     getEnv("FIRESTORE_DATABASE", "(default)"),
			UseEmulator:           getEnvBool("USE_FIREBASE_EMULATOR", false),
			EmulatorAuthHost:      getEnv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099"),
			EmulatorFirestoreHost: getEnv("FIRESTORE_EMULATOR_HOST", "localhost:8080"),
		},
		GitHub: GitHubConfig{
			Organization: getEnv("GITHUB_ORG", "jredh-dev"),
			Token:        getEnv("GITHUB_TOKEN", ""),
		},
		Slack: SlackConfig{
			WebhookURL: getEnv("SLACK_WEBHOOK_URL", ""),
			BotToken:   getEnv("SLACK_BOT_TOKEN", ""),
		},
		Stripe: StripeConfig{
			SecretKey:      getEnv("STRIPE_SECRET_KEY", ""),
			PublishableKey: getEnv("STRIPE_PUBLISHABLE_KEY", ""),
			WebhookSecret:  getEnv("STRIPE_WEBHOOK_SECRET", ""),
		},
		Auth: AuthConfig{
			SessionCookieMaxAge:         getEnvInt("SESSION_COOKIE_MAX_AGE", 3600), // 1 hour
			RememberMeMaxAge:            getEnvInt("REMEMBER_ME_MAX_AGE", 604800),  // 1 week
			RequireEmailVerification:    getEnvBool("REQUIRE_EMAIL_VERIFICATION", true),
			MockVerificationMode:        getEnvBool("MOCK_VERIFICATION_MODE", true),
			InviteExpirationDays:        getEnvInt("INVITE_EXPIRATION_DAYS", 7),
			VerificationExpirationHours: getEnvInt("VERIFICATION_EXPIRATION_HOURS", 24),
			SMSExpirationMinutes:        getEnvInt("SMS_EXPIRATION_MINUTES", 10),
			SMSMaxAttempts:              getEnvInt("SMS_MAX_ATTEMPTS", 3),
		},
		JWT: JWTConfig{
			SigningKey: getEnv("JWT_SIGNING_KEY", ""),
			Issuer:     getEnv("JWT_ISSUER", "portal.jredh.dev"),
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

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		boolVal, err := strconv.ParseBool(value)
		if err == nil {
			return boolVal
		}
	}
	return defaultValue
}
