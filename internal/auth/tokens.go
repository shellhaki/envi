package auth

import (
	"errors"
	"sync"
	"time"
)

type memorySession struct{ user, access string }
type memoryAccess struct {
	user    string
	expires time.Time
}

// MemoryTokens is an in-process TokenStore for tests. It mirrors
// PostgresTokens: taking or revoking a refresh token also invalidates the access
// token issued alongside it.
type MemoryTokens struct {
	mu        sync.Mutex
	AccessTTL time.Duration
	refresh   map[string]memorySession
	access    map[string]memoryAccess
	revoked   map[string]bool
}

func NewMemoryTokens() *MemoryTokens {
	return &MemoryTokens{AccessTTL: 15 * time.Minute, refresh: map[string]memorySession{}, access: map[string]memoryAccess{}, revoked: map[string]bool{}}
}
func (m *MemoryTokens) Save(user, refresh, access string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refresh[refresh] = memorySession{user, access}
	m.access[access] = memoryAccess{user, time.Now().Add(m.AccessTTL)}
	return nil
}
func (m *MemoryTokens) Take(refresh string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.revoked[refresh] {
		return "", errors.New("revoked token")
	}
	s, ok := m.refresh[refresh]
	if !ok {
		return "", errors.New("invalid refresh token")
	}
	m.revoked[refresh] = true
	delete(m.refresh, refresh)
	delete(m.access, s.access)
	return s.user, nil
}
func (m *MemoryTokens) Revoke(refresh string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.refresh[refresh]; ok {
		delete(m.access, s.access)
	}
	m.revoked[refresh] = true
	delete(m.refresh, refresh)
	return nil
}
func (m *MemoryTokens) Authenticate(access string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.access[access]
	if !ok {
		return "", errors.New("invalid access token")
	}
	if time.Now().After(a.expires) {
		return "", errors.New("expired access token")
	}
	return a.user, nil
}
