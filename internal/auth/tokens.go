package auth

import (
	"errors"
	"sync"
)

type session struct{ user, access string }
type MemoryTokens struct {
	mu      sync.Mutex
	refresh map[string]session
	access  map[string]string
	revoked map[string]bool
}

func NewMemoryTokens() *MemoryTokens {
	return &MemoryTokens{refresh: map[string]session{}, access: map[string]string{}, revoked: map[string]bool{}}
}
func (m *MemoryTokens) Save(user, refresh, access string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refresh[refresh] = session{user, access}
	m.access[access] = user
	return nil
}
func (m *MemoryTokens) Take(refresh string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.revoked[refresh] {
		return "", "", errors.New("revoked token")
	}
	s, ok := m.refresh[refresh]
	if !ok {
		return "", "", errors.New("invalid refresh token")
	}
	delete(m.refresh, refresh)
	return s.user, s.access, nil
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
	u, ok := m.access[access]
	if !ok {
		return "", errors.New("invalid access token")
	}
	return u, nil
}
