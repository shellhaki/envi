package auth

// This file is logging in with an emailed code.
//
// There are no passwords. Logging in takes two requests:
//
//  1. SendLoginCode: the user types their email, and we email them a
//     6-digit code.
//  2. CheckLoginCode: they type the code back. If it matches, we look up
//     their account (creating one on their first visit) and they're in.
//
// After step 2 the caller runs StartSession (in sessions.go) to hand out
// tokens.

import (
	"context"
	"errors"
	"strings"

	"shellhaki/envi/internal/otp"
)

// ErrNotInvited means the email isn't allowed in, because the server only lets
// a private beta list sign in. That's different from a wrong code, so the
// website can show a different message.
var ErrNotInvited = errors.New("not on the beta access list")

// LoginSettings is everything logging in needs, set up once when the server
// starts (see cmd/api/main.go) and passed to each function below.
type LoginSettings struct {
	// Codes makes, stores, and checks the 6-digit codes.
	Codes otp.Service

	// Mailer sends the code by email.
	Mailer otp.Mailer

	// FindOrCreateAccount returns the user ID for an email, creating the
	// account and its workspace the first time that email logs in.
	FindOrCreateAccount func(ctx context.Context, email string) (userID string, err error)

	// AllowEmail, if set, decides who may log in: the server uses it for the
	// private beta list. If it's nil, anyone can log in.
	AllowEmail func(email string) bool
}

// SendLoginCode emails a fresh login code to the given address.
func SendLoginCode(ctx context.Context, settings LoginSettings, email string) error {
	if settings.AllowEmail != nil && !settings.AllowEmail(email) {
		return ErrNotInvited
	}
	if settings.Mailer == nil {
		return errors.New("mailer is required")
	}

	code, err := settings.Codes.Issue(ctx, email)
	if err != nil {
		return err
	}
	return settings.Mailer.Send(strings.TrimSpace(email), code)
}

// CheckLoginCode checks the code the user typed. If it's right, it returns
// their user ID, creating their account if this is their first login.
func CheckLoginCode(ctx context.Context, settings LoginSettings, email string, code string) (userID string, err error) {
	if settings.AllowEmail != nil && !settings.AllowEmail(email) {
		return "", ErrNotInvited
	}
	if settings.FindOrCreateAccount == nil {
		return "", errors.New("FindOrCreateAccount is required")
	}

	err = settings.Codes.Verify(ctx, email, code)
	if err != nil {
		return "", err
	}
	return settings.FindOrCreateAccount(ctx, email)
}
