package auth

import (
	"context"
	"testing"
	"time"

	"shellhaki/envi/internal/otp"
)

type mailer struct{ code string }

func (m *mailer) Send(_ string, code string) error { m.code = code; return nil }
func TestRequestVerify(t *testing.T) {
	m := new(mailer)
	provisioned := false
	s := Service{OTP: otp.Service{Store: otp.NewMemory(), TTL: time.Minute}, Mailer: m, Provision: func(context.Context, string) (string, error) { provisioned = true; return "user", nil }}
	if err := s.Request(context.Background(), "a@b.com"); err != nil {
		t.Fatal(err)
	}
	_, a, r, err := s.Verify(context.Background(), "a@b.com", m.code)
	if err != nil || a == "" || r == "" {
		t.Fatal(err)
	}
	if !provisioned {
		t.Fatal("workspace not provisioned")
	}
}

func TestRefreshRotationAndLogout(t *testing.T) {
	tokens := NewMemoryTokens()
	_ = tokens.Save("user", "old", "access")
	s := Service{}
	access, next, err := s.Refresh("old", tokens)
	if err != nil {
		t.Fatal(err)
	}
	// Refreshing must mint a new access token, not recycle the previous one.
	if access == "access" || access == "" || next == "old" || next == "" {
		t.Fatalf("tokens not rotated: access=%q refresh=%q", access, next)
	}
	if _, err = tokens.Authenticate("access"); err == nil {
		t.Fatal("access token survived rotation")
	}
	if u, err := tokens.Authenticate(access); err != nil || u != "user" {
		t.Fatalf("rotated access token rejected: %q %v", u, err)
	}
	if _, _, err = s.Refresh("old", tokens); err == nil {
		t.Fatal("old refresh token accepted")
	}
	if err = s.Logout(next, tokens); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Refresh(next, tokens); err == nil {
		t.Fatal("revoked refresh token accepted")
	}
	if _, err = tokens.Authenticate(access); err == nil {
		t.Fatal("logout left the access token usable")
	}
}

func TestAccessTokenExpires(t *testing.T) {
	tokens := NewMemoryTokens()
	tokens.AccessTTL = -time.Second
	if err := tokens.Save("user", "refresh", "access"); err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.Authenticate("access"); err == nil {
		t.Fatal("expired access token accepted")
	}
}
