package otp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

type Store interface {
	Set(context.Context, string, string, time.Duration) error
	GetDel(context.Context, string) (string, error)
	Increment(context.Context, string, time.Duration) (int64, error)
}

type Service struct {
	Store       Store
	TTL         time.Duration
	MaxAttempts int
}

func (s Service) Issue(ctx context.Context, email string) (string, error) {
	if s.Store == nil {
		return "", errors.New("otp store is required")
	}
	if s.TTL <= 0 {
		s.TTL = 10 * time.Minute
	}
	if s.MaxAttempts <= 0 {
		s.MaxAttempts = 5
	}
	if n, err := s.Store.Increment(ctx, "otp:req:"+normalize(email), time.Hour); err != nil || n > 5 {
		return "", errors.New("too many OTP requests")
	}
	code, err := randomCode()
	if err != nil {
		return "", err
	}
	key := "otp:" + strings.ToLower(strings.TrimSpace(email))
	if err := s.Store.Set(ctx, key, hash(code), s.TTL); err != nil {
		return "", err
	}
	return code, nil
}

func (s Service) Verify(ctx context.Context, email, code string) error {
	if s.MaxAttempts <= 0 {
		s.MaxAttempts = 5
	}
	if n, err := s.Store.Increment(ctx, "otp:try:"+normalize(email), s.TTL); err != nil || n > int64(s.MaxAttempts) {
		return errors.New("too many attempts")
	}
	v, err := s.Store.GetDel(ctx, "otp:"+strings.ToLower(strings.TrimSpace(email)))
	if err != nil || v != hash(code) {
		return errors.New("invalid or expired OTP")
	}
	return nil
}

func normalize(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

func hash(v string) string { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }
func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

type Memory struct {
	mu     sync.Mutex
	values map[string]entry
}
type entry struct {
	value   string
	expires time.Time
}

func NewMemory() *Memory { return &Memory{values: map[string]entry{}} }
func (m *Memory) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = entry{value, time.Now().Add(ttl)}
	return nil
}
func (m *Memory) GetDel(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.values[key]
	delete(m.values, key)
	if !ok || time.Now().After(e.expires) {
		return "", errors.New("missing OTP")
	}
	return e.value, nil
}
func (m *Memory) Increment(_ context.Context, key string, ttl time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.values[key]
	n := int64(0)
	if time.Now().Before(e.expires) {
		fmt.Sscan(e.value, &n)
	}
	n++
	m.values[key] = entry{fmt.Sprint(n), time.Now().Add(ttl)}
	return n, nil
}
