package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/admin"
	"shellhaki/envi/internal/announce"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/bachs"
	"shellhaki/envi/internal/billing"
	"shellhaki/envi/internal/config"
	crypt "shellhaki/envi/internal/crypto"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/kv"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
	"shellhaki/envi/internal/service_token"
	"shellhaki/envi/internal/workspace"
)

type noBachs struct{}

func (noBachs) CreateCustomer(context.Context, string, string, map[string]string) (string, error) {
	return "cust_test", nil
}
func (noBachs) SaveCardCheckout(context.Context, string, string, string, map[string]string) (bachs.Checkout, error) {
	return bachs.Checkout{URL: "https://pay.test/card"}, nil
}
func (noBachs) SubscriptionCheckout(context.Context, string, string, string, string, map[string]string) (bachs.Checkout, error) {
	return bachs.Checkout{URL: "https://pay.test/team"}, nil
}
func (noBachs) CancelSubscription(context.Context, string) error { return nil }
func (noBachs) ChargeSavedCard(context.Context, string, int64, string, string, string) (bachs.Charge, error) {
	return bachs.Charge{ID: "ch_1", Status: "processing"}, nil
}
func (noBachs) PortalURL(context.Context, string) (string, error) {
	return "https://pay.test/portal", nil
}

const testWebhookSecret = "whsec_test"

func billingRouter(t *testing.T, enabled bool) (*gin.Engine, *pgxpool.Pool, string, string) {
	t.Helper()
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	w, err := workspace.Service{DB: db}.Provision(t.Context(), fmt.Sprintf("billapi-%d@example.test", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, w.OrganizationID)
		db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, w.UserID)
	})
	token, _, err := auth.StartSession(db, w.UserID)
	if err != nil {
		t.Fatal(err)
	}

	bill := billing.Service{DB: db, TeamProductID: "prod_team", WebURL: "https://envi.test"}
	if enabled {
		bill.Bachs = noBachs{}
	}
	cipher, _ := crypt.New([]byte("01234567890123456789012345678901"))
	router := Build(db, auth.LoginSettings{}, project.Service{DB: db},
		secret.Service{DB: db, Access: access.Service{DB: db}, Cipher: cipher},
		kv.Store{DB: db, Cipher: cipher}, audit.Service{DB: db}, service_token.Service{DB: db},
		invitation.Service{DB: db, Billing: bill}, "https://envi.test", false, config.DefaultDeployment(),
		announce.Service{Admin: admin.Service{DB: db}}, bill, testWebhookSecret)
	return router, db, w.OrganizationID, token
}

func send(router *gin.Engine, method, path, bearer string, body []byte, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range header {
		req.Header[k] = v
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestBillingWebhookVerifiesBeforeActing(t *testing.T) {
	router, db, org, _ := billingRouter(t, true)

	body, _ := json.Marshal(map[string]any{
		"id": "evt_api_" + org[:8], "type": "customer.subscription.created",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]any{"subscription_id": "sub_api", "product_id": "prod_team", "status": "active",
			"metadata": map[string]string{"org_id": org}},
	})
	t.Cleanup(func() { db.Exec(context.Background(), `DELETE FROM bachs_events WHERE id = $1`, "evt_api_"+org[:8]) })

	forged := http.Header{}
	forged.Set("X-Bachs-Signature-V2", bachs.Sign("whsec_wrong", body, time.Now()))
	if got := send(router, "POST", "/webhooks/bachs", "", body, forged); got.Code != 400 {
		t.Fatalf("forged webhook got %d, want 400", got.Code)
	}
	var plan string
	db.QueryRow(t.Context(), `SELECT coalesce((SELECT plan FROM billing_accounts WHERE org_id = $1), 'free')`, org).Scan(&plan)
	if plan != "free" {
		t.Fatalf("a forged webhook changed the plan to %s", plan)
	}

	signed := http.Header{}
	signed.Set("X-Bachs-Signature-V2", bachs.Sign(testWebhookSecret, body, time.Now()))
	if got := send(router, "POST", "/webhooks/bachs", "", body, signed); got.Code != 200 {
		t.Fatalf("signed webhook got %d: %s", got.Code, got.Body)
	}
	db.QueryRow(t.Context(), `SELECT plan FROM billing_accounts WHERE org_id = $1`, org).Scan(&plan)
	if plan != "team" {
		t.Fatalf("after a signed subscription event the plan is %s", plan)
	}
}

func TestBillingDisabledInstance(t *testing.T) {
	router, _, _, token := billingRouter(t, false)
	if got := send(router, "POST", "/webhooks/bachs", "", []byte(`{}`), nil); got.Code != 404 {
		t.Fatalf("webhook on a non-billing instance got %d, want 404", got.Code)
	}
	got := send(router, "GET", "/billing", token, nil, nil)
	if got.Code != 200 {
		t.Fatalf("GET /billing got %d", got.Code)
	}
	var o struct {
		Enabled bool `json:"enabled"`
	}
	json.Unmarshal(got.Body.Bytes(), &o)
	if o.Enabled {
		t.Fatal("a self-hosted instance reported billing as enabled")
	}
}

func TestBillingPageAndUpgrade(t *testing.T) {
	router, _, _, token := billingRouter(t, true)

	if got := send(router, "GET", "/billing", "", nil, nil); got.Code != 401 {
		t.Fatalf("anonymous GET /billing got %d, want 401", got.Code)
	}

	got := send(router, "POST", "/billing/payg", token, []byte(`{"currency":"NGN"}`), http.Header{"Content-Type": {"application/json"}})
	if got.Code != 200 {
		t.Fatalf("POST /billing/payg got %d: %s", got.Code, got.Body)
	}
	var reply struct {
		URL string `json:"url"`
	}
	json.Unmarshal(got.Body.Bytes(), &reply)
	if reply.URL != "https://pay.test/card" {
		t.Fatalf("payg with no card should send to checkout, got %q", reply.URL)
	}

	bad := send(router, "POST", "/billing/payg", token, []byte(`{"currency":"GBP"}`), http.Header{"Content-Type": {"application/json"}})
	if bad.Code != 400 {
		t.Fatalf("unsupported currency got %d", bad.Code)
	}
}
