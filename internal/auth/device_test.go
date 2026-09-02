package auth

import (
	"errors"
	"regexp"
	"testing"
	"time"
)

func newDeviceService() DeviceService {
	return DeviceService{Store: NewMemoryDeviceStore(), Tokens: NewMemoryTokens(), TTL: 10 * time.Minute, Interval: 5 * time.Second}
}

func TestDeviceFlowApproveAndRedeem(t *testing.T) {
	s := newDeviceService()
	deviceCode, userCode, expiresIn, interval, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if expiresIn != 600 || interval != 5 {
		t.Fatalf("expiresIn=%d interval=%d, want 600/5", expiresIn, interval)
	}
	if !regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`).MatchString(userCode) {
		t.Fatalf("user code %q not in XXXX-XXXX form", userCode)
	}

	// Before approval the CLI must keep polling.
	if _, _, err = s.Redeem(deviceCode); !isPending(err, "authorization_pending") {
		t.Fatalf("pre-approval redeem = %v, want authorization_pending", err)
	}

	// The web sends the displayed code (with the dash); Approve normalises it.
	if err = s.Approve(userCode, "user-1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	access, refresh, err := s.Redeem(deviceCode)
	if err != nil {
		t.Fatalf("Redeem after approve: %v", err)
	}
	if access == "" || refresh == "" {
		t.Fatal("Redeem returned empty tokens")
	}
	// The minted session is a normal session usable for authentication.
	if u, e := s.Tokens.Authenticate(access); e != nil || u != "user-1" {
		t.Fatalf("Authenticate(access) = %q,%v; want user-1,nil", u, e)
	}
	// A device code is single-use: a second poll must not mint again.
	if _, _, err = s.Redeem(deviceCode); !isPending(err, "expired_token") {
		t.Fatalf("second redeem = %v, want expired_token", err)
	}
}

func TestDeviceFlowDenied(t *testing.T) {
	s := newDeviceService()
	deviceCode, userCode, _, _, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Deny(userCode); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if _, _, err = s.Redeem(deviceCode); !isPending(err, "access_denied") {
		t.Fatalf("redeem after deny = %v, want access_denied", err)
	}
}

func TestDeviceApproveUnknownCode(t *testing.T) {
	s := newDeviceService()
	if err := s.Approve("ZZZZ-ZZZZ", "user-1"); !errors.Is(err, ErrDeviceCode) {
		t.Fatalf("Approve unknown = %v, want ErrDeviceCode", err)
	}
}

func TestDeviceRedeemUnknownCode(t *testing.T) {
	s := newDeviceService()
	if _, _, err := s.Redeem("not-a-real-device-code"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("Redeem unknown = %v, want ErrDeviceNotFound", err)
	}
}

func TestDeviceExpiry(t *testing.T) {
	store := NewMemoryDeviceStore()
	s := DeviceService{Store: store, Tokens: NewMemoryTokens(), TTL: 10 * time.Minute, Interval: 5 * time.Second}
	// Seed a row that has already expired, bypassing Start's TTL floor.
	if err := store.Create(HashToken("dc"), "EXPIREDX", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve("EXPIREDX", "user-1"); !errors.Is(err, ErrDeviceCode) {
		t.Fatalf("Approve expired = %v, want ErrDeviceCode", err)
	}
	if _, _, err := s.Redeem("dc"); !isPending(err, "expired_token") {
		t.Fatalf("Redeem expired = %v, want expired_token", err)
	}
}

func TestNormalizeUserCode(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"wxyz-abcd", "WXYZABCD"},
		{"WXYZ ABCD", "WXYZABCD"},
		{"WXYZABCD", "WXYZABCD"},
		{"  wx yz-ab cd  ", "WXYZABCD"},
	} {
		if got := NormalizeUserCode(tc.in); got != tc.want {
			t.Errorf("NormalizeUserCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func isPending(err error, reason string) bool {
	var p DevicePending
	return errors.As(err, &p) && p.Reason == reason
}
