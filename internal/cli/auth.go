package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	kr "shellhaki/envi/internal/cli/keyring"
	"strings"
)

type Tokens struct{ Access, Refresh string }
type TokenStore interface {
	Save(Tokens) error
	Load() (Tokens, error)
}
type keyringAdapter struct{ store kr.Store }

func (k keyringAdapter) Save(t Tokens) error {
	return k.store.Save(kr.Tokens{Access: t.Access, Refresh: t.Refresh})
}
func (k keyringAdapter) Load() (Tokens, error) {
	t, e := k.store.Load()
	return Tokens{Access: t.Access, Refresh: t.Refresh}, e
}

func Authenticate(ctx context.Context, c Client, store TokenStore, in io.Reader, out io.Writer, email string) error {
	r := bufio.NewReader(in)
	if email == "" {
		fmt.Fprint(out, "Email: ")
		v, e := r.ReadString('\n')
		if e != nil && !errors.Is(e, io.EOF) {
			return e
		}
		email = strings.TrimSpace(v)
	}
	if email == "" {
		return errors.New("email is required")
	}
	if e := c.Do(ctx, "POST", "/auth/request-otp", map[string]string{"email": email}, nil); e != nil {
		return e
	}
	fmt.Fprint(out, "Code: ")
	code, e := r.ReadString('\n')
	if e != nil && !errors.Is(e, io.EOF) {
		return e
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("OTP code is required")
	}
	var t struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if e = c.Do(ctx, "POST", "/auth/verify-otp", map[string]string{"email": email, "code": code}, &t); e != nil {
		return e
	}
	if t.Access == "" || t.Refresh == "" {
		return errors.New("invalid authentication response")
	}
	if e = store.Save(Tokens{t.Access, t.Refresh}); e != nil {
		return fmt.Errorf("store credentials: %w", e)
	}
	fmt.Fprintln(out, "Authenticated")
	return nil
}
func ResolveToken(store TokenStore) (string, error) {
	if v := LoadConfig().Token; v != "" {
		return v, nil
	}
	t, e := store.Load()
	if e != nil || t.Access == "" {
		return "", errors.New("not authenticated; run envi auth")
	}
	return t.Access, nil
}
