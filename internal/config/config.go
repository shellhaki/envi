package config

import (
	"errors"
	"os"
)

type Config struct {
	DatabaseURL, RedisURL, EncryptionKey, ResendAPIKey, ResendFrom, Address, WebURL string
}

func Load() Config {
	a := os.Getenv("ENVI_ADDRESS")
	if a == "" {
		a = ":8080"
	}
	web := os.Getenv("ENVI_WEB_URL")
	if web == "" {
		web = "http://localhost:3000"
	}
	return Config{os.Getenv("DATABASE_URL"), os.Getenv("REDIS_URL"), os.Getenv("ENVI_ENCRYPTION_KEY"), os.Getenv("RESEND_API_KEY"), os.Getenv("RESEND_FROM"), a, web}
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
	return c, nil
}
