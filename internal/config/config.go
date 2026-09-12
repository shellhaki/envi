package config

import (
	"errors"
	"os"
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
	return c, nil
}
