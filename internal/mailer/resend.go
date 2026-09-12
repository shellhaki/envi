package mailer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Resend sends mail through the Resend HTTP API (https://resend.com) —
// no SMTP, no app passwords, just an API key and a from-address.
//
// Without a verified sending domain on the Resend account, "From" is
// restricted to their sandbox address (onboarding@resend.dev) and mail can
// only be delivered to the address the Resend account itself was signed up
// with — verify a domain in the Resend dashboard to send to arbitrary
// recipients (invitations, OTP codes for any user) in production.
type Resend struct {
	APIKey string
	From   string
}

func (r Resend) Send(m Message) error {
	if r.APIKey == "" {
		return fmt.Errorf("RESEND_API_KEY is required")
	}
	if r.From == "" {
		return fmt.Errorf("RESEND_FROM is required")
	}
	return withRetry(func() error { return r.send(m) })
}

func (r Resend) send(m Message) error {
	payload, err := json.Marshal(map[string]any{
		"from":    r.From,
		"to":      []string{m.To},
		"subject": m.Subject,
		"text":    m.Text,
		"html":    m.HTML,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Resend: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
		return fmt.Errorf("Resend API error (%d): %s", res.StatusCode, detail)
	}
	return nil
}
