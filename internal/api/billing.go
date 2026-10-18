package api

import (
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/bachs"
)

// Billing routes act only on the caller's own personal organization, never one named in the request.

// ownedOrg finds the caller's personal organization and email. On failure it has already replied.
func (s *server) ownedOrg(c *gin.Context) (orgID, email string, ok bool) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(403, gin.H{"code": "forbidden", "error": "billing needs a signed-in person, not a service token"})
		return "", "", false
	}
	err := s.db.QueryRow(c, `
		SELECT o.id, u.email FROM users u
		  JOIN memberships m ON m.user_id = u.id AND m.role = 'owner'
		  JOIN organizations o ON o.id = m.org_id AND o.type = 'personal'
		 WHERE u.id = $1`, userID).Scan(&orgID, &email)
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "account unavailable"})
		return "", "", false
	}
	return orgID, email, true
}

// billingError hides the payment provider's reply from the user and logs it instead.
func billingError(c *gin.Context, err error) {
	var providerErr *bachs.APIError
	if errors.As(err, &providerErr) {
		log.Printf("billing %s: %v", c.FullPath(), err)
		c.JSON(502, gin.H{"code": "payment_provider", "error": "the payment provider refused the request; try again shortly"})
		return
	}
	c.JSON(400, gin.H{"code": "billing", "error": err.Error()})
}

// GET /billing
func (s *server) billingOverview(c *gin.Context) {
	org, _, ok := s.ownedOrg(c)
	if !ok {
		return
	}
	overview, err := s.billing.Overview(c, org)
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "billing unavailable"})
		return
	}
	c.JSON(200, overview)
}

// POST /billing/payg {currency}. An empty url means a card was already saved and the switch is done.
func (s *server) startPAYG(c *gin.Context) {
	var in struct {
		Currency string `json:"currency"`
	}
	_ = c.ShouldBindJSON(&in)
	org, email, ok := s.ownedOrg(c)
	if !ok {
		return
	}
	url, err := s.billing.StartPAYG(c, org, email, in.Currency)
	if err != nil {
		billingError(c, err)
		return
	}
	c.JSON(200, gin.H{"url": url})
}

// POST /billing/team
func (s *server) startTeam(c *gin.Context) {
	org, email, ok := s.ownedOrg(c)
	if !ok {
		return
	}
	url, err := s.billing.StartTeam(c, org, email)
	if err != nil {
		billingError(c, err)
		return
	}
	c.JSON(200, gin.H{"url": url})
}

// POST /billing/cancel
func (s *server) cancelPlan(c *gin.Context) {
	org, _, ok := s.ownedOrg(c)
	if !ok {
		return
	}
	if err := s.billing.Cancel(c, org); err != nil {
		billingError(c, err)
		return
	}
	c.Status(204)
}

// POST /billing/portal
func (s *server) billingPortal(c *gin.Context) {
	org, _, ok := s.ownedOrg(c)
	if !ok {
		return
	}
	url, err := s.billing.Portal(c, org)
	if err != nil {
		billingError(c, err)
		return
	}
	c.JSON(200, gin.H{"url": url})
}

// POST /billing/retry
func (s *server) retryCharge(c *gin.Context) {
	org, _, ok := s.ownedOrg(c)
	if !ok {
		return
	}
	if err := s.billing.RetryNow(c, org); err != nil {
		billingError(c, err)
		return
	}
	c.Status(204)
}

// POST /webhooks/bachs. Bachs retries on 500 but not on 400, so 400 is only for a bad signature.
func (s *server) bachsWebhook(c *gin.Context) {
	if !s.billing.Enabled() {
		c.Status(404)
		return
	}
	// The signature covers these exact bytes, so read them raw rather than decoding JSON.
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.Status(400)
		return
	}
	event, err := bachs.VerifyWebhook(s.billing.WebhookSecret, c.Request.Header, body, time.Now())
	if err != nil {
		c.JSON(400, gin.H{"error": "signature does not verify"})
		return
	}
	if err := s.billing.Apply(c, event); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not apply event"})
		return
	}
	c.Status(200)
}
