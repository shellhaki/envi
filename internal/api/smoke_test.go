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

func TestSmoke(t *testing.T) {
	r := New()
	m := new(smokeMailer)
	tokens := auth.NewMemoryTokens()
	h := AuthHandler{Service: auth.Service{OTP: otp.Service{Store: otp.NewMemory(), TTL: time.Minute}, Mailer: m, Provision: func(context.Context, string) (string, error) { return "user", nil }}, Tokens: tokens}
	h.Routes(r)
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
	if w := req("POST", "/auth/verify-otp", `{"email":"smoke@example.com","code":"`+m.code+`"}`); w.Code != 200 {
		t.Fatalf("verify OTP: %d", w.Code)
	}
}
