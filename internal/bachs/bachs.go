// Package bachs is the client for Bachs (https://docs.bachs.io), Envi's payment provider.
package bachs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	SandboxURL = "https://sandbox-api.bachs.io"
	LiveURL    = "https://api.bachs.io"
)

// Client talks to Bachs with one secret key.
type Client struct {
	Key  string
	Base string
	HTTP *http.Client
}

// New builds a client for key, choosing the base URL from the key itself.
func New(key string) (Client, error) {
	key = strings.TrimSpace(key)
	var base string
	switch {
	case strings.HasPrefix(key, "sk_sandbox_"):
		base = SandboxURL
	case strings.HasPrefix(key, "sk_live_"):
		base = LiveURL
	case key == "":
		return Client{}, errors.New("no Bachs key")
	default:
		return Client{}, errors.New("unrecognised Bachs key: expected sk_sandbox_ or sk_live_")
	}
	return Client{Key: key, Base: base, HTTP: &http.Client{Timeout: 20 * time.Second}}, nil
}

// Live reports whether this client moves real money.
func (c Client) Live() bool { return c.Base == LiveURL }

// APIError is a non-2xx reply from Bachs.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("bachs: %d: %s", e.Status, e.Body) }

func (c Client) do(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("bachs: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return &APIError{Status: res.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Money converts minor units to the decimal string Bachs wants.
func Money(minor int64) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	return fmt.Sprintf("%s%d.%02d", sign, minor/100, minor%100)
}

// MinorUnits parses a Bachs amount back into minor units.
func MinorUnits(amount string) (int64, error) {
	amount = strings.TrimSpace(amount)
	whole, fraction, _ := strings.Cut(amount, ".")
	if len(fraction) > 2 {
		fraction = fraction[:2]
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad amount %q", amount)
	}
	f, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad amount %q", amount)
	}
	if strings.HasPrefix(whole, "-") {
		return w*100 - f, nil
	}
	return w*100 + f, nil
}

// --- customers ----------------------------------------------------------

// CreateCustomer creates the Bachs customer for an organization. metadata holds Envi's IDs, to match webhooks back.
func (c Client) CreateCustomer(ctx context.Context, email, name string, metadata map[string]string) (string, error) {
	var out struct {
		CustomerID string `json:"customer_id"`
		ID         string `json:"id"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/customers", map[string]any{
		"email": email, "name": name, "metadata": metadata,
	}, "", &out)
	if err != nil {
		return "", err
	}
	if out.CustomerID != "" {
		return out.CustomerID, nil
	}
	if out.ID != "" {
		return out.ID, nil
	}
	return "", errors.New("bachs: customer created with no id in the reply")
}

// PortalURL returns a link to the Bachs portal, where a customer updates their card or sees invoices.
func (c Client) PortalURL(ctx context.Context, customerID string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/customers/"+customerID+"/portal-sessions", map[string]any{}, "", &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// --- checkout -------------------------------------------------------------

// Checkout is what a customer is sent to.
type Checkout struct {
	ID  string `json:"checkout_id"`
	URL string `json:"checkout_url"`
}

// SaveCardCheckout starts a checkout that saves a card without charging it.
func (c Client) SaveCardCheckout(ctx context.Context, customerID, successURL, cancelURL string, metadata map[string]string) (Checkout, error) {
	var out Checkout
	err := c.do(ctx, http.MethodPost, "/v1/checkout-sessions", map[string]any{
		"customer":            map[string]string{"customer_id": customerID},
		"save_payment_method": true,
		"success_url":         successURL,
		"cancel_url":          cancelURL,
		"metadata":            metadata,
	}, "", &out)
	return out, err
}

// SubscriptionCheckout starts a checkout for a recurring product.
func (c Client) SubscriptionCheckout(ctx context.Context, customerID, productID, successURL, cancelURL string, metadata map[string]string) (Checkout, error) {
	var out Checkout
	err := c.do(ctx, http.MethodPost, "/v1/checkout-sessions", map[string]any{
		"product_cart":     []map[string]any{{"product_id": productID, "quantity": 1}},
		"customer":         map[string]string{"customer_id": customerID},
		"billing_currency": "USD",
		"success_url":      successURL,
		"cancel_url":       cancelURL,
		"metadata":         metadata,
	}, "", &out)
	return out, err
}

// CancelSubscription cancels at the end of the paid period, not immediately.
func (c Client) CancelSubscription(ctx context.Context, subscriptionID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/subscriptions/"+subscriptionID,
		map[string]any{"cancel_at_period_end": true, "reason": "Cancelled from the Envi dashboard"}, "", nil)
}

// --- charges ----------------------------------------------------------------

// Charge is a charge against a saved card.
type Charge struct {
	ID     string `json:"charge_id"`
	Status string `json:"status"`
}

// ChargeSavedCard charges the customer's default saved card, off-session.
func (c Client) ChargeSavedCard(ctx context.Context, customerID string, minor int64, currency, description, key string) (Charge, error) {
	var out struct {
		Charge
		AltID string `json:"id"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/charges", map[string]any{
		"customer":    customerID,
		"amount":      Money(minor),
		"currency":    currency,
		"description": description,
		"reference":   key,
	}, key, &out)
	if out.Charge.ID == "" {
		out.Charge.ID = out.AltID
	}
	return out.Charge, err
}

// --- webhooks ---------------------------------------------------------------

// Event is the envelope every webhook arrives in.
type Event struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Data      json.RawMessage `json:"data"`
}

// SignatureTolerance is how old a signed delivery may be.
const SignatureTolerance = 5 * time.Minute

var ErrBadSignature = errors.New("bachs: webhook signature does not verify")

// VerifyWebhook checks a delivery against the signing secret and decodes it.
func VerifyWebhook(secret string, header http.Header, body []byte, now time.Time) (Event, error) {
	if secret == "" {
		return Event{}, errors.New("bachs: no webhook signing secret configured")
	}

	timestamp, signatures := parseSignatureHeader(header.Get("X-Bachs-Signature-V2"))
	if timestamp == "" {
		timestamp = header.Get("X-Bachs-Timestamp")
		if v1 := header.Get("X-Bachs-Signature"); v1 != "" {
			signatures = append(signatures, v1)
		}
	}
	if timestamp == "" || len(signatures) == 0 {
		return Event{}, ErrBadSignature
	}

	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return Event{}, ErrBadSignature
	}
	age := now.Sub(time.Unix(seconds, 0))
	if age > SignatureTolerance || age < -SignatureTolerance {
		return Event{}, ErrBadSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + string(body)))
	want := mac.Sum(nil)

	matched := false
	for _, signature := range signatures {
		got, err := hex.DecodeString(strings.TrimSpace(signature))
		// hmac.Equal takes the same time however much matches, so it leaks nothing about the signature.
		if err == nil && hmac.Equal(got, want) {
			matched = true
		}
	}
	if !matched {
		return Event{}, ErrBadSignature
	}

	var event Event
	if err := json.Unmarshal(body, &event); err != nil {
		return Event{}, fmt.Errorf("bachs: webhook body: %w", err)
	}
	if event.ID == "" || event.Type == "" {
		return Event{}, errors.New("bachs: webhook has no id or type")
	}
	return event, nil
}

// parseSignatureHeader reads "t=1700000000,v1=abc,v1=def".
func parseSignatureHeader(value string) (timestamp string, signatures []string) {
	for _, part := range strings.Split(value, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			timestamp = val
		case "v1":
			signatures = append(signatures, val)
		}
	}
	return timestamp, signatures
}

// Sign produces a V2 header for body, the way Bachs does.
func Sign(secret string, body []byte, at time.Time) string {
	timestamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + string(body)))
	return "t=" + timestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
