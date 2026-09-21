package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"shellhaki/envi/internal/otp"
)

// fakeMailer remembers the last code instead of emailing it.
type fakeMailer struct{ lastCode string }

func (m *fakeMailer) Send(_ string, code string) error {
	m.lastCode = code
	return nil
}

func testLoginSettings(mailer *fakeMailer, created *bool) LoginSettings {
	return LoginSettings{
		Codes:  otp.Service{Store: otp.NewMemory(), TTL: time.Minute},
		Mailer: mailer,
		FindOrCreateAccount: func(context.Context, string) (string, error) {
			*created = true
			return "user-1", nil
		},
	}
}

func TestLoginWithEmailedCode(t *testing.T) {
	mailer := &fakeMailer{}
	created := false
	settings := testLoginSettings(mailer, &created)

	if err := SendLoginCode(context.Background(), settings, "a@b.com"); err != nil {
		t.Fatal(err)
	}
	userID, err := CheckLoginCode(context.Background(), settings, "a@b.com", mailer.lastCode)
	if err != nil || userID != "user-1" {
		t.Fatalf("got %q, %v", userID, err)
	}
	if !created {
		t.Fatal("the account was never looked up or created")
	}
}

func TestWrongCodeIsRejected(t *testing.T) {
	mailer := &fakeMailer{}
	created := false
	settings := testLoginSettings(mailer, &created)

	if err := SendLoginCode(context.Background(), settings, "a@b.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckLoginCode(context.Background(), settings, "a@b.com", "000000x"); err == nil {
		t.Fatal("a wrong code was accepted")
	}
	if created {
		t.Fatal("an account was created for a wrong code")
	}
}

func TestBetaListBlocksOtherEmails(t *testing.T) {
	mailer := &fakeMailer{}
	created := false
	settings := testLoginSettings(mailer, &created)
	settings.AllowEmail = func(email string) bool { return email == "invited@b.com" }

	if err := SendLoginCode(context.Background(), settings, "stranger@b.com"); !errors.Is(err, ErrNotInvited) {
		t.Fatalf("got %v, want ErrNotInvited", err)
	}
	if mailer.lastCode != "" {
		t.Fatal("a code was emailed to someone not on the list")
	}
	if err := SendLoginCode(context.Background(), settings, "invited@b.com"); err != nil {
		t.Fatal(err)
	}
}
