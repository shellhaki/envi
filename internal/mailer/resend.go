package mailer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Resend sends mail through the Resend API (https://resend.com).
type Resend struct {
	APIKey string
	From   string
}

// Send tries up to three times, since a dropped connection or a 5xx is usually brief.
func (r Resend) Send(m Message) error {
	if r.APIKey == "" {
		return fmt.Errorf("RESEND_API_KEY is required")
	}
	if r.From == "" {
		return fmt.Errorf("RESEND_FROM is required")
	}
	var err error
	for _, wait := range []time.Duration{0, time.Second, 3 * time.Second} {
		time.Sleep(wait)
		if err = r.send(m); err == nil {
			return nil
		}
	}
	return err
}

func (r Resend) send(m Message) error {
	body := map[string]any{
		"from":    r.From,
		"to":      []string{m.To},
		"subject": m.Subject,
		"text":    m.Text,
		"html":    m.HTML,
	}
	if len(m.Headers) > 0 {
		body["headers"] = m.Headers
	}
	payload, err := json.Marshal(body)
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
