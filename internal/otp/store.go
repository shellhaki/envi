package otp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Store is where codes and counters live: Redis in production, memory in tests.
type Store interface {
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, key string) error
	Increment(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

type Service struct {
	Store        Store
	TTL          time.Duration // how long a code is valid (default 10 minutes)
	MaxAttempts  int           // wrong guesses allowed per code (default 10)
	RequestLimit int           // codes that may be requested per hour (default 20)
}

// Issue creates a new 6-digit code for the email and stores only its hash.
func (s Service) Issue(ctx context.Context, email string) (string, error) {
	if s.Store == nil {
		return "", errors.New("otp store is required")
	}
	ttl := orDefault(s.TTL, 10*time.Minute)
	limit := orDefault(s.RequestLimit, 20)
	email = normalize(email)

	requests, err := s.Store.Increment(ctx, "otp:req:"+email, time.Hour)
	if err != nil || requests > int64(limit) {
		return "", errors.New("too many OTP requests")
	}
	code, err := randomCode()
	if err != nil {
		return "", err
	}
	if err := s.Store.Set(ctx, "otp:"+email, hash(code), ttl); err != nil {
		return "", err
	}
	return code, nil
}

// Verify checks a code. A wrong guess leaves the code usable; a right one uses it up.
func (s Service) Verify(ctx context.Context, email, code string) error {
	maxAttempts := orDefault(s.MaxAttempts, 10)
	email = normalize(email)

	attempts, err := s.Store.Increment(ctx, "otp:try:"+email, s.TTL)
	if err != nil || attempts > int64(maxAttempts) {
		return errors.New("too many attempts")
	}
	stored, err := s.Store.Get(ctx, "otp:"+email)
	if err != nil || subtle.ConstantTimeCompare([]byte(stored), []byte(hash(strings.TrimSpace(code)))) != 1 {
		return errors.New("invalid or expired OTP")
	}
	_ = s.Store.Del(ctx, "otp:"+email)
	return nil
}

func normalize(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func hash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func orDefault[T int | time.Duration](value, fallback T) T {
	if value <= 0 {
		return fallback
	}
	return value
}
