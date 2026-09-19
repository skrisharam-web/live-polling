package config

import (
	"testing"
	"time"
)

func TestLoadDefaultsInDevelopment(t *testing.T) {
	t.Setenv("APP_ENV", "development")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.JWTExpiresIn != 24*time.Hour {
		t.Errorf("JWTExpiresIn = %v, want 24h", cfg.JWTExpiresIn)
	}
	if cfg.CookieSecure {
		t.Error("CookieSecure should default to false outside production")
	}
	if cfg.JWTSecret == "" {
		t.Error("a development fallback secret should be filled in")
	}
}

func TestProductionRequiresStrongSecret(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("FRONTEND_URL", "https://polls.example.com")

	t.Run("missing secret", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "")
		if _, err := Load(); err == nil {
			t.Fatal("expected an error when JWT_SECRET is unset in production")
		}
	})

	t.Run("short secret", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "too-short")
		if _, err := Load(); err == nil {
			t.Fatal("expected an error for a short production JWT_SECRET")
		}
	})

	t.Run("valid", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "a-sufficiently-long-production-secret-value")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() returned an error: %v", err)
		}
		if !cfg.CookieSecure {
			t.Error("CookieSecure must be true in production")
		}
	})
}

func TestProductionRejectsInsecureCookies(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "a-sufficiently-long-production-secret-value")
	t.Setenv("FRONTEND_URL", "https://polls.example.com")
	t.Setenv("COOKIE_SECURE", "false")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error when COOKIE_SECURE is false in production")
	}
}

func TestWildcardOriginRejected(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("FRONTEND_URL", "*")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for a wildcard CORS origin")
	}
}

func TestAllowedOriginsAreSplitAndNormalised(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("FRONTEND_URL", "http://localhost:5173/, https://polls.example.com , ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}
	want := []string{"http://localhost:5173", "https://polls.example.com"}
	if len(cfg.AllowedOrigins) != len(want) {
		t.Fatalf("AllowedOrigins = %v, want %v", cfg.AllowedOrigins, want)
	}
	for i := range want {
		if cfg.AllowedOrigins[i] != want[i] {
			t.Errorf("AllowedOrigins[%d] = %q, want %q", i, cfg.AllowedOrigins[i], want[i])
		}
	}
}

func TestInvalidJWTExpiry(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("JWT_EXPIRES_IN", "twenty-four-hours")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for an unparseable JWT_EXPIRES_IN")
	}
}
