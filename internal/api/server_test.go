package api

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"shellhaki/envi/internal/announce"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/billing"
	"shellhaki/envi/internal/config"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/kv"
	"shellhaki/envi/internal/otp"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
	"shellhaki/envi/internal/service_token"
)

type fakeMailer struct{ code string }

func (m *fakeMailer) Send(_ string, code string) error { m.code = code; return nil }

// The routes answer and a wrong login code is turned away, all without a database.
func TestHealthAndLoginCodes(t *testing.T) {
	mailer := new(fakeMailer)
	login := auth.LoginSettings{
		Codes:               otp.Service{Store: otp.NewMemory(), TTL: time.Minute},
		Mailer:              mailer,
		FindOrCreateAccount: func(context.Context, string) (string, error) { return "user", nil },
	}
	r := Build(nil, login, project.Service{}, secret.Service{}, kv.Store{}, audit.Service{},
		service_token.Service{}, invitation.Service{}, "", config.Deployment{}, announce.Service{}, billing.Service{})
	send := func(method, path, body string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		return w.Code
	}

	if code := send("GET", "/health", ""); code != 200 {
		t.Fatalf("health: %d", code)
	}
	if code := send("POST", "/auth/request-otp", `{"email":"smoke@example.com"}`); code != 202 {
		t.Fatalf("request code: %d", code)
	}
	if mailer.code == "" {
		t.Fatal("no code was sent")
	}
	if code := send("POST", "/auth/verify-otp", `{"email":"smoke@example.com","code":"wrong!"}`); code != 401 {
		t.Fatalf("wrong code: %d, want 401", code)
	}
	if code := send("POST", "/auth/request-otp", `{"email":"not-an-email"}`); code != 400 {
		t.Fatalf("bad email: %d, want 400", code)
	}
}
