package bachs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The key decides the server. A sandbox key sent to live, or the reverse, is
// the mistake this rules out.
func TestNewPicksTheServerFromTheKey(t *testing.T) {
	sandbox, err := New("sk_sandbox_abc")
	if err != nil || sandbox.Base != SandboxURL || sandbox.Live() {
		t.Fatalf("sandbox key: %+v %v", sandbox, err)
	}
	live, err := New("sk_live_abc")
	if err != nil || live.Base != LiveURL || !live.Live() {
		t.Fatalf("live key: %+v %v", live, err)
	}
	for _, bad := range []string{"", "pk_live_abc", "sk_test_abc", "abc"} {
		if _, err := New(bad); err == nil {
			t.Errorf("accepted key %q", bad)
		}
	}
}

func TestMoneyRoundTrips(t *testing.T) {
	for minor, text := range map[int64]string{0: "0.00", 40: "0.40", 400: "4.00", 60000: "600.00", 7: "0.07", 123456: "1234.56"} {
		if got := Money(minor); got != text {
			t.Errorf("Money(%d) = %q, want %q", minor, got, text)
		}
		back, err := MinorUnits(text)
		if err != nil || back != minor {
			t.Errorf("MinorUnits(%q) = %d, %v; want %d", text, back, err, minor)
		}
	}
	// Bachs sometimes writes "0" or a single decimal; both must parse.
	for text, want := range map[string]int64{"0": 0, "4.5": 450, "75000.00": 7500000} {
		if got, err := MinorUnits(text); err != nil || got != want {
			t.Errorf("MinorUnits(%q) = %d, %v; want %d", text, got, err, want)
		}
	}
}

func event(id string) []byte {
	return []byte(`{"id":"` + id + `","type":"collection.succeeded","created_at":"2026-10-15T10:00:00Z","data":{"charge_id":"ch_1"}}`)
}

func headerWith(name, value string) http.Header {
	h := http.Header{}
	h.Set(name, value)
	return h
}

func TestVerifyWebhookAcceptsAGoodSignature(t *testing.T) {
	now := time.Now()
	body := event("evt_1")
	got, err := VerifyWebhook("whsec_x", headerWith("X-Bachs-Signature-V2", Sign("whsec_x", body, now)), body, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "evt_1" || got.Type != "collection.succeeded" {
		t.Fatalf("decoded %+v", got)
	}
}

// Every way a delivery can be forged or replayed must be refused.
func TestVerifyWebhookRefuses(t *testing.T) {
	now := time.Now()
	body := event("evt_1")
	good := Sign("whsec_x", body, now)

	cases := map[string]struct {
		header http.Header
		body   []byte
		at     time.Time
	}{
		"wrong secret":    {headerWith("X-Bachs-Signature-V2", Sign("whsec_other", body, now)), body, now},
		"body changed":    {headerWith("X-Bachs-Signature-V2", good), []byte(strings.Replace(string(body), "ch_1", "ch_2", 1)), now},
		"replayed later":  {headerWith("X-Bachs-Signature-V2", good), body, now.Add(6 * time.Minute)},
		"from the future": {headerWith("X-Bachs-Signature-V2", Sign("whsec_x", body, now.Add(10*time.Minute))), body, now},
		"no header":       {http.Header{}, body, now},
		"garbage header":  {headerWith("X-Bachs-Signature-V2", "nonsense"), body, now},
		"signature only":  {headerWith("X-Bachs-Signature-V2", "v1=abcd"), body, now},
	}
	for name, tc := range cases {
		if _, err := VerifyWebhook("whsec_x", tc.header, tc.body, tc.at); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := VerifyWebhook("", headerWith("X-Bachs-Signature-V2", good), body, now); err == nil {
		t.Error("verified with no secret configured")
	}
}

// During a rotation Bachs signs with both secrets; either matching is enough.
func TestVerifyWebhookDuringRotation(t *testing.T) {
	now := time.Now()
	body := event("evt_1")
	oldSig := Sign("whsec_old", body, now)
	newSig := Sign("whsec_new", body, now)
	both := oldSig + "," + newSig[strings.Index(newSig, "v1="):]

	for _, secret := range []string{"whsec_old", "whsec_new"} {
		if _, err := VerifyWebhook(secret, headerWith("X-Bachs-Signature-V2", both), body, now); err != nil {
			t.Errorf("secret %s refused during rotation: %v", secret, err)
		}
	}
}

// The older two-header form still verifies.
func TestVerifyWebhookV1Fallback(t *testing.T) {
	now := time.Now()
	body := event("evt_1")
	v2 := Sign("whsec_x", body, now)
	ts := strings.TrimPrefix(strings.Split(v2, ",")[0], "t=")
	sig := strings.TrimPrefix(strings.Split(v2, ",")[1], "v1=")

	h := http.Header{}
	h.Set("X-Bachs-Timestamp", ts)
	h.Set("X-Bachs-Signature", sig)
	if _, err := VerifyWebhook("whsec_x", h, body, now); err != nil {
		t.Fatal(err)
	}
}

// The requests that move money carry the key, the idempotency key, and the
// amount as a decimal string.
func TestChargeRequestShape(t *testing.T) {
	var got struct {
		auth, idem string
		body       map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/charges" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		got.auth = r.Header.Get("Authorization")
		got.idem = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)
		w.WriteHeader(201)
		w.Write([]byte(`{"charge_id":"ch_9","status":"processing"}`))
	}))
	defer srv.Close()

	c := Client{Key: "sk_sandbox_k", Base: srv.URL, HTTP: srv.Client()}
	charge, err := c.ChargeSavedCard(context.Background(), "cust_1", 1200, "USD", "Envi", "payg:org:2026-09:1")
	if err != nil {
		t.Fatal(err)
	}
	if charge.ID != "ch_9" || charge.Status != "processing" {
		t.Errorf("charge = %+v", charge)
	}
	if got.auth != "Bearer sk_sandbox_k" {
		t.Errorf("auth = %q", got.auth)
	}
	if got.idem != "payg:org:2026-09:1" {
		t.Errorf("idempotency key = %q", got.idem)
	}
	if got.body["amount"] != "12.00" || got.body["currency"] != "USD" || got.body["customer"] != "cust_1" {
		t.Errorf("body = %v", got.body)
	}
	if got.body["reference"] != "payg:org:2026-09:1" {
		t.Errorf("reference = %v", got.body["reference"])
	}
}

func TestAPIErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		w.Write([]byte(`{"error":"card declined"}`))
	}))
	defer srv.Close()

	c := Client{Key: "sk_sandbox_k", Base: srv.URL, HTTP: srv.Client()}
	_, err := c.ChargeSavedCard(context.Background(), "cust_1", 100, "USD", "x", "k")
	var api *APIError
	if err == nil || !errorsAs(err, &api) || api.Status != 402 {
		t.Fatalf("err = %v", err)
	}
}

func errorsAs(err error, target **APIError) bool {
	for err != nil {
		if e, ok := err.(*APIError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
