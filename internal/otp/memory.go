package otp

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"
)

// Memory is an in-memory Store for tests.
type Memory struct {
	mu     sync.Mutex
	values map[string]memoryEntry
}

type memoryEntry struct {
	value   string
	expires time.Time
}

func NewMemory() *Memory { return &Memory{values: map[string]memoryEntry{}} }

func (m *Memory) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = memoryEntry{value, time.Now().Add(ttl)}
	return nil
}

func (m *Memory) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.values[key]
	if !ok || time.Now().After(entry.expires) {
		delete(m.values, key)
		return "", errors.New("missing OTP")
	}
	return entry.value, nil
}

func (m *Memory) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, key)
	return nil
}

func (m *Memory) Increment(_ context.Context, key string, ttl time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	if entry := m.values[key]; time.Now().Before(entry.expires) {
		n, _ = strconv.ParseInt(entry.value, 10, 64)
	}
	n++
	m.values[key] = memoryEntry{strconv.FormatInt(n, 10), time.Now().Add(ttl)}
	return n, nil
}
