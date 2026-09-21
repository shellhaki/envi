package api

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/otp"
)

type smokeMailer struct{ code string }

func (m *smokeMailer) Send(_ string, code string) error { m.code = code; return nil }

// The routes answer and the login-code steps reject bad input, without a
// database: a wrong code is turned away before a session is ever saved. The
// successful login, which does write a session, is covered end to end in
// internal/e2e.
func TestSmoke(t *testing.T) {
	r := New()
	m := new(smokeMailer)
	login := auth.LoginSettings{
		Codes:               otp.Service{Store: otp.NewMemory(), TTL: time.Minute},
		Mailer:              m,
		FindOrCreateAccount: func(context.Context, string) (string, error) { return "user", nil },
	}
	addAuthRoutes(r, nil, login)
	req := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		return w
	}
	if w := req("GET", "/health", ""); w.Code != 200 {
		t.Fatalf("health: %d", w.Code)
	}
	if w := req("POST", "/auth/request-otp", `{"email":"smoke@example.com"}`); w.Code != 202 {
		t.Fatalf("request OTP: %d", w.Code)
	}
	if m.code == "" {
		t.Fatal("no code was sent")
	}
	if w := req("POST", "/auth/verify-otp", `{"email":"smoke@example.com","code":"wrong!"}`); w.Code != 401 {
		t.Fatalf("verify with a wrong code: %d, want 401", w.Code)
	}
	if w := req("POST", "/auth/request-otp", `{"email":"not-an-email"}`); w.Code != 400 {
		t.Fatalf("request OTP with a bad email: %d, want 400", w.Code)
	}
}
