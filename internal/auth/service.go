package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"shellhaki/envi/internal/otp"
)

type Service struct {
	OTP                   otp.Service
	Mailer                otp.Mailer
	Provision             func(context.Context, string) (string, error)
	AccessTTL, RefreshTTL time.Duration
}
type TokenStore interface {
	Save(string, string, string) error
	Take(string) (string, string, error)
	Revoke(string) error
	Authenticate(string) (string, error)
}

func (s Service) Refresh(refresh string, store TokenStore) (string, string, error) {
	if store == nil {
		return "", "", errors.New("token store is required")
	}
	userID, access, err := store.Take(refresh)
	if err != nil {
		return "", "", err
	}
	next, err := token()
	if err != nil {
		return "", "", err
	}
	if err := store.Save(userID, next, access); err != nil {
		return "", "", err
	}
	return access, next, nil
}
func (s Service) Logout(refresh string, store TokenStore) error {
	if store == nil {
		return errors.New("token store is required")
	}
	return store.Revoke(refresh)
}

func (s Service) Request(ctx context.Context, email string) error {
	code, err := s.OTP.Issue(ctx, email)
	if err != nil {
		return err
	}
	if s.Mailer == nil {
		return errors.New("mailer is required")
	}
	return s.Mailer.Send(strings.TrimSpace(email), code)
}
func (s Service) Verify(ctx context.Context, email, code string) (string, string, string, error) {
	if err := s.OTP.Verify(ctx, email, code); err != nil {
		return "", "", "", err
	}
	if s.Provision == nil {
		return "", "", "", errors.New("provisioner is required")
	}
	userID, err := s.Provision(ctx, email)
	if err != nil {
		return "", "", "", err
	}
	a, err := token()
	if err != nil {
		return "", "", "", err
	}
	r, err := token()
	return userID, a, r, err
}
func token() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func HashToken(v string) []byte { h := sha256.Sum256([]byte(v)); return h[:] }
