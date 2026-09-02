package auth

import (
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"time"
)

// DeviceStore persists device authorizations for the device-code login flow.
// Redeem must be single-use: only the first caller to observe an approved
// authorization receives the user; every later poll sees it already redeemed.
type DeviceStore interface {
	Create(deviceHash []byte, userCode string, expiresAt time.Time) error
	Approve(userCode, userID string) error
	Deny(userCode string) error
	Redeem(deviceHash []byte) (userID string, err error)
}

// DevicePending is a non-fatal poll outcome. Reason carries an RFC 8628 error
// code: authorization_pending and slow_down mean "keep polling", access_denied
// and expired_token mean "stop". The CLI branches on the reason.
type DevicePending struct{ Reason string }

func (e DevicePending) Error() string { return e.Reason }

// ErrDeviceNotFound means a device code matched no authorization at all.
var ErrDeviceNotFound = errors.New("device code not found")

// ErrDeviceCode means a user code could not be approved or denied: unknown,
// already handled, or expired.
var ErrDeviceCode = errors.New("invalid or expired code")

// DeviceService runs the device authorization grant. Sessions minted on redeem
// go through the same TokenStore the OTP path uses, so refresh behaves identically.
type DeviceService struct {
	Store         DeviceStore
	Tokens        TokenStore
	TTL, Interval time.Duration
}

// userCodeAlphabet omits I, O, 0, and 1 so a code copied off a screen is
// unambiguous when typed back. Its length divides 256, keeping newUserCode's
// byte-to-index mapping unbiased.
const userCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Start creates a pending authorization and returns the device code (polled by
// the CLI), the display user code (typed by the human), and the poll parameters.
func (s DeviceService) Start() (deviceCode, userCode string, expiresIn, interval int, err error) {
	if s.Store == nil {
		return "", "", 0, 0, errors.New("device store is required")
	}
	ttl := s.TTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	poll := s.Interval
	if poll <= 0 {
		poll = 5 * time.Second
	}
	deviceCode, err = token()
	if err != nil {
		return "", "", 0, 0, err
	}
	// The stored user_code is UNIQUE among live rows; retry on the rare clash.
	for attempt := 0; attempt < 5; attempt++ {
		var raw string
		if raw, err = newUserCode(); err != nil {
			return "", "", 0, 0, err
		}
		if err = s.Store.Create(HashToken(deviceCode), raw, time.Now().Add(ttl)); err == nil {
			return deviceCode, formatUserCode(raw), int(ttl.Seconds()), int(poll.Seconds()), nil
		}
	}
	return "", "", 0, 0, err
}

// Approve binds an authenticated user to a pending user code.
func (s DeviceService) Approve(userCode, userID string) error {
	if s.Store == nil {
		return errors.New("device store is required")
	}
	if userID == "" {
		return errors.New("user is required")
	}
	return s.Store.Approve(NormalizeUserCode(userCode), userID)
}

// Deny rejects a pending user code so the CLI stops polling with access_denied.
func (s DeviceService) Deny(userCode string) error {
	if s.Store == nil {
		return errors.New("device store is required")
	}
	return s.Store.Deny(NormalizeUserCode(userCode))
}

// Redeem is the CLI poll step: it returns a fresh session once the authorization
// is approved, or a DevicePending explaining why it is not ready.
func (s DeviceService) Redeem(deviceCode string) (access, refresh string, err error) {
	if s.Store == nil {
		return "", "", errors.New("device store is required")
	}
	if s.Tokens == nil {
		return "", "", errors.New("token store is required")
	}
	userID, err := s.Store.Redeem(HashToken(deviceCode))
	if err != nil {
		return "", "", err
	}
	if access, err = token(); err != nil {
		return "", "", err
	}
	if refresh, err = token(); err != nil {
		return "", "", err
	}
	if err = s.Tokens.Save(userID, refresh, access); err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// newUserCode returns eight raw alphabet characters (no separator). len(alphabet)
// is 32 and 256%32==0, so mapping each random byte modulo the length is unbiased.
func newUserCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = userCodeAlphabet[int(v)%len(userCodeAlphabet)]
	}
	return string(out), nil
}

// formatUserCode renders the stored eight-character code for display as XXXX-XXXX.
func formatUserCode(raw string) string {
	if len(raw) != 8 {
		return raw
	}
	return raw[:4] + "-" + raw[4:]
}

// NormalizeUserCode upper-cases input and drops anything outside the alphabet, so
// "wxyz-abcd", "WXYZ ABCD", and "WXYZABCD" all resolve to the stored form.
func NormalizeUserCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if strings.ContainsRune(userCodeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MemoryDeviceStore is an in-process DeviceStore for tests, mirroring
// PostgresDeviceStore's state machine and single-use redeem.
type MemoryDeviceStore struct {
	mu     sync.Mutex
	byHash map[string]*memoryDevice
	byCode map[string]*memoryDevice
}
type memoryDevice struct {
	userID    string
	status    string
	expiresAt time.Time
}

func NewMemoryDeviceStore() *MemoryDeviceStore {
	return &MemoryDeviceStore{byHash: map[string]*memoryDevice{}, byCode: map[string]*memoryDevice{}}
}
func (m *MemoryDeviceStore) Create(deviceHash []byte, userCode string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byCode[userCode]; ok {
		return errors.New("user code already exists")
	}
	d := &memoryDevice{status: "pending", expiresAt: expiresAt}
	m.byHash[string(deviceHash)] = d
	m.byCode[userCode] = d
	return nil
}
func (m *MemoryDeviceStore) Approve(userCode, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.byCode[userCode]
	if !ok || d.status != "pending" || time.Now().After(d.expiresAt) {
		return ErrDeviceCode
	}
	d.status, d.userID = "approved", userID
	return nil
}
func (m *MemoryDeviceStore) Deny(userCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.byCode[userCode]
	if !ok || d.status != "pending" {
		return ErrDeviceCode
	}
	d.status = "denied"
	return nil
}
func (m *MemoryDeviceStore) Redeem(deviceHash []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.byHash[string(deviceHash)]
	if !ok {
		return "", ErrDeviceNotFound
	}
	if time.Now().After(d.expiresAt) {
		return "", DevicePending{"expired_token"}
	}
	switch d.status {
	case "pending":
		return "", DevicePending{"authorization_pending"}
	case "denied":
		return "", DevicePending{"access_denied"}
	case "approved":
		d.status = "redeemed"
		return d.userID, nil
	default: // redeemed or anything unexpected
		return "", DevicePending{"expired_token"}
	}
}
