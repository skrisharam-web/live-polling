// Package config loads and validates every externally supplied setting once, at
// startup. Nothing else in the codebase reads os.Getenv, so there is exactly one
// place to look for "where does this value come from" and one place that decides
// whether the process is safe to start.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment names the deployment mode. Production tightens several defaults.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
	EnvProduction  Environment = "production"
)

// minJWTSecretLength is the shortest secret accepted in production. HS256 keys
// shorter than this are brute-forceable offline once a token leaks.
const minJWTSecretLength = 32

// Config is the fully resolved application configuration.
type Config struct {
	AppEnv Environment
	Port   string

	MongoURI      string
	MongoDatabase string

	RedisURL string

	JWTSecret    string
	JWTExpiresIn time.Duration

	// AllowedOrigins is the CORS allow-list, derived from FRONTEND_URL
	// (comma-separated). Credentialed requests mean a wildcard is never an option.
	AllowedOrigins []string

	CookieSecure bool
	CookieDomain string

	// MaxRequestBodyBytes caps any JSON payload the API will read.
	MaxRequestBodyBytes int64

	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration
}

// IsProduction reports whether production hardening applies.
func (c *Config) IsProduction() bool { return c.AppEnv == EnvProduction }

// Load reads the environment, applies development-friendly defaults and then
// validates. It returns an error rather than exiting so tests can call it.
func Load() (*Config, error) {
	cfg := &Config{
		AppEnv:              Environment(getEnv("APP_ENV", string(EnvDevelopment))),
		Port:                getEnv("PORT", "8080"),
		MongoURI:            getEnv("MONGODB_URI", "mongodb://localhost:27017"),
		MongoDatabase:       getEnv("MONGODB_DATABASE", "livepolling"),
		RedisURL:            getEnv("REDIS_URL", "redis://localhost:6379/0"),
		JWTSecret:           os.Getenv("JWT_SECRET"),
		CookieDomain:        getEnv("COOKIE_DOMAIN", ""),
		MaxRequestBodyBytes: 64 * 1024,
		ShutdownTimeout:     15 * time.Second,
	}

	expiry, err := time.ParseDuration(getEnv("JWT_EXPIRES_IN", "24h"))
	if err != nil {
		return nil, fmt.Errorf("JWT_EXPIRES_IN is not a valid duration (e.g. 24h): %w", err)
	}
	cfg.JWTExpiresIn = expiry

	cfg.AllowedOrigins = splitOrigins(getEnv("FRONTEND_URL", "http://localhost:5173"))

	// Cookies must be Secure in production; in development the frontend is served
	// over plain HTTP, where a Secure cookie would simply never be sent.
	cfg.CookieSecure = getEnvBool("COOKIE_SECURE", cfg.AppEnv == EnvProduction)

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	switch c.AppEnv {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		return fmt.Errorf("APP_ENV must be development, test or production (got %q)", c.AppEnv)
	}

	if c.MongoURI == "" {
		return fmt.Errorf("MONGODB_URI is required")
	}
	if c.MongoDatabase == "" {
		return fmt.Errorf("MONGODB_DATABASE is required")
	}
	if c.RedisURL == "" {
		return fmt.Errorf("REDIS_URL is required")
	}
	if len(c.AllowedOrigins) == 0 {
		return fmt.Errorf("FRONTEND_URL is required (comma-separated list of allowed origins)")
	}
	for _, origin := range c.AllowedOrigins {
		if origin == "*" {
			return fmt.Errorf("FRONTEND_URL must not be '*': the API uses credentialed requests")
		}
	}

	if c.JWTSecret == "" {
		if c.IsProduction() {
			return fmt.Errorf("JWT_SECRET is required in production")
		}
		// A per-process random secret would invalidate sessions on every restart,
		// which is painful in development; a fixed, obviously-fake value is clearer.
		c.JWTSecret = "development-only-insecure-jwt-secret-change-me"
	}
	if c.IsProduction() {
		if len(c.JWTSecret) < minJWTSecretLength {
			return fmt.Errorf("JWT_SECRET must be at least %d characters in production", minJWTSecretLength)
		}
		if !c.CookieSecure {
			return fmt.Errorf("COOKIE_SECURE must be true in production")
		}
	}
	return nil
}

func splitOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimRight(strings.TrimSpace(p), "/"); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}
