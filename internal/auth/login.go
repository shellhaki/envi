package auth

import (
	"context"
	"errors"
	"strings"

	"shellhaki/envi/internal/otp"
)

// LoginSettings is what email login needs, set up once in cmd/api/main.go.
type LoginSettings struct {
	Codes               otp.Service
	Mailer              otp.Mailer
	FindOrCreateAccount func(ctx context.Context, email string) (userID string, err error)
}

// SendLoginCode emails a fresh 6-digit login code.
func SendLoginCode(ctx context.Context, settings LoginSettings, email string) error {
	if settings.Mailer == nil {
		return errors.New("mailer is required")
	}
	code, err := settings.Codes.Issue(ctx, email)
	if err != nil {
		return err
	}
	return settings.Mailer.Send(strings.TrimSpace(email), code)
}

// CheckLoginCode verifies the code and returns the user ID, creating the account on first login.
func CheckLoginCode(ctx context.Context, settings LoginSettings, email, code string) (string, error) {
	if settings.FindOrCreateAccount == nil {
		return "", errors.New("FindOrCreateAccount is required")
	}
	if err := settings.Codes.Verify(ctx, email, code); err != nil {
		return "", err
	}
	return settings.FindOrCreateAccount(ctx, email)
}
