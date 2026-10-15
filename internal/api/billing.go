package api

import (
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/bachs"
	"shellhaki/envi/internal/billing"
)

/*
Billing endpoints.

Everything here acts on the caller's own organization — the personal one they
own — and only its owner may change the plan. There is no endpoint that takes
an organization id from the request, so there is no way to point one at
somebody else's.

The webhook is the exception: Bachs calls it, with no session, and it is
trusted only because the signature verifies.
*/

// ownedOrg returns the organization the caller owns, and their email for the
// Bachs customer record.
func ownedOrg(c *gin.Context, db *pgxpool.Pool) (orgID, email string, ok bool) {
	userID := c.GetString("user_id")
	if userID == "" {
		// A service token acts for an environment, not a person, and has no
		// business with anybody's payment details.
		c.JSON(403, gin.H{"code": "forbidden", "error": "billing needs a signed-in person, not a service token"})
		return "", "", false
	}
	err := db.QueryRow(c, `
		SELECT o.id, u.email FROM users u
		  JOIN memberships m ON m.user_id = u.id AND m.role = 'owner'
		  JOIN organizations o ON o.id = m.org_id AND o.type = 'personal'
		 WHERE u.id = $1
	`, userID).Scan(&orgID, &email)
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "account unavailable"})
		return "", "", false
	}
	return orgID, email, true
}

// billingError reports a failure from a plan change. Bachs's own reply is not
// passed through: it can name internal ids, and it is not actionable for the
// person looking at the billing page.
func billingError(c *gin.Context, err error) {
	var api *bachs.APIError
	if errors.As(err, &api) {
		// The person sees a generic message; the operator needs the real one.
		// Bachs's reply goes to the log only, where it can say exactly which
		// field was wrong without showing anybody an internal id.
		log.Printf("billing %s: %v", c.FullPath(), err)
		c.JSON(502, gin.H{"code": "payment_provider", "error": "the payment provider refused the request; try again shortly"})
		return
	}
	c.JSON(400, gin.H{"code": "billing", "error": err.Error()})
}

func addBillingRoutes(r *gin.Engine, db *pgxpool.Pool, m gin.HandlerFunc, service billing.Service, webhookSecret string) {
	g := r.Group("/billing", m)

	g.GET("", func(c *gin.Context) {
		org, _, ok := ownedOrg(c, db)
		if !ok {
			return
		}
		overview, err := service.Overview(c, org)
		if err != nil {
			c.JSON(500, gin.H{"code": "internal", "error": "billing unavailable"})
			return
		}
		c.JSON(200, overview)
	})

	g.POST("/payg", func(c *gin.Context) {
		var body struct {
			Currency string `json:"currency"`
		}
		_ = c.ShouldBindJSON(&body)
		org, email, ok := ownedOrg(c, db)
		if !ok {
			return
		}
		url, err := service.StartPAYG(c, org, email, body.Currency)
		if err != nil {
			billingError(c, err)
			return
		}
		// An empty URL means a card was already saved and the switch is done.
		c.JSON(200, gin.H{"url": url})
	})

	g.POST("/team", func(c *gin.Context) {
		org, email, ok := ownedOrg(c, db)
		if !ok {
			return
		}
		url, err := service.StartTeam(c, org, email)
		if err != nil {
			billingError(c, err)
			return
		}
		c.JSON(200, gin.H{"url": url})
	})

	g.POST("/cancel", func(c *gin.Context) {
		org, _, ok := ownedOrg(c, db)
		if !ok {
			return
		}
		if err := service.Cancel(c, org); err != nil {
			billingError(c, err)
			return
		}
		c.Status(204)
	})

	g.POST("/portal", func(c *gin.Context) {
		org, _, ok := ownedOrg(c, db)
		if !ok {
			return
		}
		url, err := service.Portal(c, org)
		if err != nil {
			billingError(c, err)
			return
		}
		c.JSON(200, gin.H{"url": url})
	})

	g.POST("/retry", func(c *gin.Context) {
		org, _, ok := ownedOrg(c, db)
		if !ok {
			return
		}
		if err := service.RetryNow(c, org); err != nil {
			billingError(c, err)
			return
		}
		c.Status(204)
	})

	/*
	   The Bachs webhook. Responses follow Bachs's rules:

	     400  only for a delivery that fails verification, which Bachs does
	          not retry
	     500  when Envi's own write fails, so Bachs retries it
	     200  otherwise, including events billing has no interest in

	   Bachs gives each delivery 10 seconds; applying an event is one short
	   transaction.
	*/
	r.POST("/webhooks/bachs", func(c *gin.Context) {
		if !service.Enabled() {
			c.Status(404)
			return
		}
		// Read raw: the signature is over these exact bytes, and decoding
		// then re-encoding JSON changes them.
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
		if err != nil {
			c.Status(400)
			return
		}
		event, err := bachs.VerifyWebhook(webhookSecret, c.Request.Header, body, time.Now())
		if err != nil {
			c.JSON(400, gin.H{"error": "signature does not verify"})
			return
		}
		if err := service.Apply(c, event); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not apply event"})
			return
		}
		c.Status(200)
	})
}
