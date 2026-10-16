package api

import (
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/admin"
	"shellhaki/envi/internal/announce"
	"shellhaki/envi/internal/config"
)

/*
Operator access to the whole instance.

Who counts as an operator is read from the ENVI_ADMINS environment variable
rather than from a column on users, deliberately. A database flag can be set by
any bug that can write to users; an environment variable can only be set by
whoever can deploy. For a screen that reads across every organization, that is
the difference worth having.

	ENVI_ADMINS=you@example.com,ops@example.com

Unset means nobody. The route is still registered and still answers 404, so an
instance with no operators configured does not advertise that the endpoint
exists.
*/

// normaliseEmail is how an address is compared, on both sides of the check:
// the list is stored this way and the address read from the database is put
// through it before the lookup. One function so the two can never disagree.
func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// operators returns the configured operator emails, normalised.
func operators() map[string]struct{} {
	raw := strings.Split(config.Setting("ENVI_ADMINS"), ",")
	set := make(map[string]struct{}, len(raw))
	for _, email := range raw {
		if email = normaliseEmail(email); email != "" {
			set[email] = struct{}{}
		}
	}
	return set
}

/*
RequireOperator allows the request through only for a configured operator.

It answers 404 rather than 403 on refusal. A 403 confirms the endpoint is real
and that the caller simply lacks the rank, which tells an attacker where to
spend their time; a 404 says nothing at all. The operator knows the URL exists
because they configured it.

It runs after RequireAuth, so "user_id" is already set. The email is read from
the database rather than from the token, because the token carries an id and
the allowlist is written in addresses — and because a user who changes their
email should be re-checked against the list on the next request, not the next
login.
*/
func RequireOperator(db *pgxpool.Pool) gin.HandlerFunc {
	allowed := operators()
	// The count, not the addresses: enough to tell "the variable never
	// arrived" from "it arrived and my address is not in it".
	log.Printf("ENVI_ADMINS: %d address(es)", len(allowed))
	service := admin.Service{DB: db}
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		if userID == "" {
			c.AbortWithStatus(404)
			return
		}
		var email string
		if err := db.QueryRow(c, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email); err != nil {
			c.AbortWithStatus(404)
			return
		}
		role, err := service.RoleFor(c, email, allowed)
		if err != nil || role == "" {
			c.AbortWithStatus(404)
			return
		}
		c.Set("operator_role", role)
		c.Set("operator_email", normaliseEmail(email))
		c.Next()
	}
}

/*
RequireManage narrows a route to admins.

An announcer is on the operator screen to write announcements, not to read the
instance's figures or change who else may act on it. The check is separate from
RequireOperator so the difference is visible at the route rather than buried in
a handler, and it answers 404 for the same reason RequireOperator does.
*/
func RequireManage() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !admin.MayManage(c.GetString("operator_role")) {
			c.AbortWithStatus(404)
			return
		}
		c.Next()
	}
}

// RequireAnnounce narrows a route to anybody who may send announcements.
func RequireAnnounce() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !admin.MayAnnounce(c.GetString("operator_role")) {
			c.AbortWithStatus(404)
			return
		}
		c.Next()
	}
}

/*
addAdminRoutes registers the operator-only endpoints.

Reads are unrestricted across the instance; writes are confined to expenses and
nothing else. An operator screen that can change the product is an operator
screen that can be tricked into changing the product, so the write surface here
is deliberately one table wide.
*/
func addAdminRoutes(r *gin.Engine, db *pgxpool.Pool, m gin.HandlerFunc, deployment config.Deployment, announcer announce.Service) {
	service := admin.Service{DB: db}
	g := r.Group("/admin", m, RequireOperator(db))

	// Whether the caller is an operator at all. The dashboard asks this before
	// showing an admin link, so a normal user is never shown a door they
	// cannot open.
	g.GET("/whoami", func(c *gin.Context) {
		role := c.GetString("operator_role")
		c.JSON(200, gin.H{
			"operator": true,
			"role":     role,
			"email":    c.GetString("operator_email"),
			"announce": admin.MayAnnounce(role),
			"manage":   admin.MayManage(role),
		})
	})

	g.GET("/stats", RequireManage(), func(c *gin.Context) {
		snapshot, err := service.Snapshot(c, string(deployment.Mode), deployment.Currency)
		if err != nil {
			c.JSON(500, gin.H{"code": "admin_stats_failed", "error": err.Error()})
			return
		}
		c.JSON(200, snapshot)
	})

	g.POST("/expenses", RequireManage(), func(c *gin.Context) {
		var body struct {
			// Minor units — kobo or cents. The client sends the integer it
			// computed from what was typed, so no float crosses the wire.
			AmountMinor int64  `json:"amount_minor"`
			Currency    string `json:"currency"`
			Category    string `json:"category"`
			Description string `json:"description"`
			SpentOn     string `json:"spent_on"`
			Recurring   string `json:"recurring"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(400, gin.H{"code": "invalid_body", "error": err.Error()})
			return
		}
		expense := admin.Expense{
			AmountMinor: body.AmountMinor,
			Currency:    body.Currency,
			Category:    body.Category,
			Description: body.Description,
			Recurring:   body.Recurring,
		}
		if body.SpentOn != "" {
			when, err := time.Parse("2006-01-02", body.SpentOn)
			if err != nil {
				c.JSON(400, gin.H{"code": "invalid_date", "error": "spent_on must be YYYY-MM-DD"})
				return
			}
			expense.SpentOn = when
		}
		saved, err := service.AddExpense(c, c.GetString("user_id"), expense)
		if err != nil {
			c.JSON(400, gin.H{"code": "invalid_expense", "error": err.Error()})
			return
		}
		c.JSON(201, saved)
	})

	g.DELETE("/expenses/:id", RequireManage(), func(c *gin.Context) {
		if err := service.DeleteExpense(c, c.Param("id")); err != nil {
			c.JSON(500, gin.H{"code": "delete_failed", "error": err.Error()})
			return
		}
		c.Status(204)
	})

	// --- operators: admins only -------------------------------------------

	g.GET("/operators", RequireManage(), func(c *gin.Context) {
		list, err := service.Operators(c, operators())
		if err != nil {
			c.JSON(500, gin.H{"code": "operators_failed", "error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"operators": list})
	})

	g.POST("/operators", RequireManage(), func(c *gin.Context) {
		var body struct{ Email, Name, Role string }
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(400, gin.H{"code": "invalid_body", "error": err.Error()})
			return
		}
		added, err := service.AddOperator(c, c.GetString("user_id"), body.Email, body.Name, body.Role)
		if err != nil {
			c.JSON(400, gin.H{"code": "invalid_operator", "error": err.Error()})
			return
		}
		c.JSON(201, added)
	})

	g.DELETE("/operators/:id", RequireManage(), func(c *gin.Context) {
		if err := service.RemoveOperator(c, c.Param("id")); err != nil {
			c.JSON(500, gin.H{"code": "delete_failed", "error": err.Error()})
			return
		}
		c.Status(204)
	})

	// --- announcements: admins and announcers -----------------------------

	g.GET("/announcements", RequireAnnounce(), func(c *gin.Context) {
		sent, err := service.Announcements(c, 25)
		if err != nil {
			c.JSON(500, gin.H{"code": "announcements_failed", "error": err.Error()})
			return
		}
		count, err := service.RecipientCount(c)
		if err != nil {
			c.JSON(500, gin.H{"code": "recipients_failed", "error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"announcements": sent, "recipients": count})
	})

	g.POST("/announcements", RequireAnnounce(), func(c *gin.Context) {
		var body struct {
			Subject string `json:"subject"`
			Body    string `json:"body"`
			// Confirm has to be sent explicitly. This route emails every
			// subscribed person on the instance, and a mis-click should not
			// be able to do that.
			Confirm bool `json:"confirm"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(400, gin.H{"code": "invalid_body", "error": err.Error()})
			return
		}
		if !body.Confirm {
			c.JSON(400, gin.H{"code": "not_confirmed", "error": "sending to everyone has to be confirmed"})
			return
		}
		sent, err := announcer.Send(c, c.GetString("user_id"), c.GetString("operator_email"), body.Subject, body.Body)
		if err != nil {
			c.JSON(400, gin.H{"code": "announce_failed", "error": err.Error()})
			return
		}
		c.JSON(201, sent)
	})
}
