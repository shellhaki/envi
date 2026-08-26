package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("REDIS_URL", "redis://localhost")
	t.Setenv("SMTP_EMAIL", "a@gmail.com")
	t.Setenv("SMTP_PASSWORD", "app-password")
	t.Setenv("ENVI_ENCRYPTION_KEY", "test-key")
	c := Load()
	if c.DatabaseURL != "postgres://test" || c.RedisURL != "redis://localhost" || c.EncryptionKey != "test-key" || c.SMTPEmail != "a@gmail.com" {
		t.Fatalf("unexpected config: %#v", c)
	}
}

func TestRead(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("REDIS_URL", "redis://localhost")
	t.Setenv("SMTP_EMAIL", "a@gmail.com")
	t.Setenv("SMTP_PASSWORD", "app-password")
	t.Setenv("ENVI_ENCRYPTION_KEY", "01234567890123456789012345678901")
	if _, err := Read(); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("DATABASE_URL")
	if _, err := Read(); err == nil {
		t.Fatal("missing database URL accepted")
	}
}
