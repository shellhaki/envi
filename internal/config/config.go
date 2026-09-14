package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL, RedisURL, EncryptionKey, ResendAPIKey, ResendFrom, Address, WebURL, Environment string
}

func Load() Config {
	port := os.Getenv("ENVI_API_PORT")
	if port == "" {
		port = "8080"
	}
	web := os.Getenv("ENVI_WEB_URL")
	if web == "" {
		web = "http://localhost:3000"
	}
	return Config{
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		RedisURL:      os.Getenv("REDIS_URL"),
		EncryptionKey: os.Getenv("ENVI_ENCRYPTION_KEY"),
		ResendAPIKey:  os.Getenv("RESEND_API_KEY"),
		ResendFrom:    os.Getenv("RESEND_FROM"),
		Address:       ":" + port,
		WebURL:        web,
		Environment:   os.Getenv("ENVIRONMENT"),
	}
}

func Read() (Config, error) {
	c := Load()
	if c.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if c.RedisURL == "" {
		return Config{}, errors.New("REDIS_URL is required")
	}
	if c.ResendAPIKey == "" || c.ResendFrom == "" {
		return Config{}, errors.New("RESEND_API_KEY and RESEND_FROM are required")
	}
	if len(c.EncryptionKey) != 32 {
		return Config{}, errors.New("ENVI_ENCRYPTION_KEY must be exactly 32 bytes")
	}
	// Every invitation and device-approval link is built from ENVI_WEB_URL, so
	// a deployed instance left on the localhost default hands real users links
	// that resolve to their own machine. Fail at boot instead.
	//
	// Any named environment counts, not just "production": a beta deployment
	// serves real testers, and gating this on one magic string is how a live
	// instance ends up quietly emailing localhost links.
	if c.IsDeployed() && c.IsLocalWebURL() {
		return Config{}, errors.New("ENVI_WEB_URL must be set to the public dashboard URL when ENVIRONMENT is set (got " + c.WebURL + "); invitation and device-approval links are built from it")
	}
	return c, nil
}

// IsDeployed reports whether this instance serves real users. ENVIRONMENT is
// left unset for local development and named ("production", "beta", a staging
// label) on anything deployed, so an empty value is the only safe default —
// a new environment name inherits the strict behaviour rather than escaping it.
func (c Config) IsDeployed() bool {
	switch strings.ToLower(strings.TrimSpace(c.Environment)) {
	case "", "development", "dev", "local", "test":
		return false
	default:
		return true
	}
}

// IsLocalWebURL reports whether links built from WebURL only work on this
// machine. Callers use it to refuse, or to warn loudly, before sending mail
// that no recipient can act on.
func (c Config) IsLocalWebURL() bool { return isLoopback(c.WebURL) }

// WritesToRemoteDatabaseWithLocalLinks is the specific, easily-missed
// combination where a developer's instance is pointed at the shared database
// but still builds links for their own machine: rows land in production and
// the emails announcing them are unusable. Neither half looks wrong alone,
// which is exactly why it is worth naming.
func (c Config) WritesToRemoteDatabaseWithLocalLinks() bool {
	return c.IsLocalWebURL() && c.DatabaseURL != "" && !isLoopback(c.DatabaseURL)
}

func isLoopback(url string) bool {
	return strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1") || strings.Contains(url, "[::1]")
}
