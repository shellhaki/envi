package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL, RedisURL, EncryptionKey, ResendAPIKey, ResendFrom, Address, WebURL, Environment string
	Bachs                                                                                        Bachs
}

// Bachs holds the payment provider's settings.
type Bachs struct {
	SecretKey, WebhookSecret, TeamProductID, ReturnURL string
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
		Bachs: Bachs{
			SecretKey:     Setting("BACHS_SECRET_KEY"),
			WebhookSecret: Setting("BACHS_WEBHOOK_SECRET"),
			TeamProductID: Setting("BACHS_TEAM_PRODUCT_ID"),
			ReturnURL:     Setting("BACHS_RETURN_URL"),
		},
	}
}

func Read() (Config, error) {
	c := Load()
	if c.DatabaseURL == "" {
		return Config{}, errors.New("r")
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
	// Emailed links are built from ENVI_WEB_URL, so a deployed server must not point it at localhost.
	if c.IsDeployed() && c.IsLocalWebURL() {
		return Config{}, errors.New("ENVI_WEB_URL must be set to the public dashboard URL when ENVIRONMENT is set (got " + c.WebURL + "); invitation and device-approval links are built from it")
	}
	return c, nil
}

// IsDeployed reports whether this instance serves real users.
func (c Config) IsDeployed() bool {
	switch strings.ToLower(strings.TrimSpace(c.Environment)) {
	case "", "development", "dev", "local", "test":
		return false
	default:
		return true
	}
}

// IsLocalWebURL reports whether links built from WebURL only work on this machine.
func (c Config) IsLocalWebURL() bool { return IsLocalURL(c.WebURL) }

// WritesToRemoteDatabaseWithLocalLinks catches a laptop writing to a shared database while emailing localhost links.
func (c Config) WritesToRemoteDatabaseWithLocalLinks() bool {
	return c.IsLocalWebURL() && c.DatabaseURL != "" && !IsLocalURL(c.DatabaseURL)
}

// IsLocalURL reports an address only this machine can reach.
func IsLocalURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		// A Postgres URL with no host uses the local Unix socket.
		return parsed.Scheme == "postgres" || parsed.Scheme == "postgresql"
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// DocsURL is where the documentation lives, for links in outgoing email.
func (c Config) DocsURL() string {
	if v := strings.TrimSpace(os.Getenv("ENVI_DOCS_URL")); v != "" {
		return v
	}
	if strings.Contains(c.WebURL, "envisecrets.com") {
		return "https://docs.envisecrets.com"
	}
	return strings.TrimRight(c.WebURL, "/") + "/docs"
}
