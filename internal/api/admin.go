package api

import (
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/admin"
	"shellhaki/envi/internal/config"
)

// Operators are the emails in ENVI_ADMINS plus anyone an admin adds on the operator screen.
// Every refusal is a 404, so the admin area does not reveal that it exists.

func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// operators returns the addresses listed in ENVI_ADMINS.
func operators() map[string]struct{} {
	set := map[string]struct{}{}
	for _, email := range strings.Split(config.Setting("ENVI_ADMINS"), ",") {
		if email = normaliseEmail(email); email != "" {
			set[email] = struct{}{}
		}
	}
	return set
}

// RequireOperator lets through only operators, and records their role.
func RequireOperator(db *pgxpool.Pool) gin.HandlerFunc {
	fromEnv := operators()
	log.Printf("ENVI_ADMINS: %d address(es)", len(fromEnv))
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
		role, err := service.RoleFor(c, email, fromEnv)
		if err != nil || role == "" {
			c.AbortWithStatus(404)
			return
		}
		c.Set("operator_role", role)
		c.Set("operator_email", normaliseEmail(email))
		c.Next()
	}
}

// RequireManage lets through only admins (not announcers).
func RequireManage() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !admin.MayManage(c.GetString("operator_role")) {
			c.AbortWithStatus(404)
			return
		}
		c.Next()
	}
}

// RequireAnnounce lets through admins and announcers.
func RequireAnnounce() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !admin.MayAnnounce(c.GetString("operator_role")) {
			c.AbortWithStatus(404)
			return
		}
		c.Next()
	}
}

// GET /admin/whoami tells the dashboard whether to show the admin link.
func (s *server) whoami(c *gin.Context) {
	role := c.GetString("operator_role")
	c.JSON(200, gin.H{
		"operator": true,
		"role":     role,
		"email":    c.GetString("operator_email"),
		"announce": admin.MayAnnounce(role),
		"manage":   admin.MayManage(role),
	})
}

// GET /admin/stats
func (s *server) adminStats(c *gin.Context) {
	snapshot, err := s.admin.Snapshot(c, string(s.deployment.Mode), s.deployment.Currency)
	if err != nil {
		c.JSON(500, gin.H{"code": "admin_stats_failed", "error": err.Error()})
		return
	}
	c.JSON(200, snapshot)
}

// POST /admin/expenses {amount_minor, currency, category, description, spent_on, recurring}
func (s *server) addExpense(c *gin.Context) {
	var in struct {
		AmountMinor int64  `json:"amount_minor"` // kobo or cents
		Currency    string `json:"currency"`
		Category    string `json:"category"`
		Description string `json:"description"`
		SpentOn     string `json:"spent_on"` // YYYY-MM-DD
		Recurring   string `json:"recurring"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"code": "invalid_body", "error": err.Error()})
		return
	}
	expense := admin.Expense{
		AmountMinor: in.AmountMinor,
		Currency:    in.Currency,
		Category:    in.Category,
		Description: in.Description,
		Recurring:   in.Recurring,
	}
	if in.SpentOn != "" {
		day, err := time.Parse("2006-01-02", in.SpentOn)
		if err != nil {
			c.JSON(400, gin.H{"code": "invalid_date", "error": "spent_on must be YYYY-MM-DD"})
			return
		}
		expense.SpentOn = day
	}
	saved, err := s.admin.AddExpense(c, c.GetString("user_id"), expense)
	if err != nil {
		c.JSON(400, gin.H{"code": "invalid_expense", "error": err.Error()})
		return
	}
	c.JSON(201, saved)
}

// DELETE /admin/expenses/:id
func (s *server) deleteExpense(c *gin.Context) {
	if err := s.admin.DeleteExpense(c, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"code": "delete_failed", "error": err.Error()})
		return
	}
	c.Status(204)
}

// GET /admin/operators
func (s *server) listOperators(c *gin.Context) {
	list, err := s.admin.Operators(c, operators())
	if err != nil {
		c.JSON(500, gin.H{"code": "operators_failed", "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"operators": list})
}

// POST /admin/operators {email, name, role}
func (s *server) addOperator(c *gin.Context) {
	var in struct{ Email, Name, Role string }
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"code": "invalid_body", "error": err.Error()})
		return
	}
	added, err := s.admin.AddOperator(c, c.GetString("user_id"), in.Email, in.Name, in.Role)
	if err != nil {
		c.JSON(400, gin.H{"code": "invalid_operator", "error": err.Error()})
		return
	}
	c.JSON(201, added)
}

// DELETE /admin/operators/:id
func (s *server) removeOperator(c *gin.Context) {
	if err := s.admin.RemoveOperator(c, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"code": "delete_failed", "error": err.Error()})
		return
	}
	c.Status(204)
}

// GET /admin/announcements
func (s *server) listAnnouncements(c *gin.Context) {
	sent, err := s.admin.Announcements(c, 25)
	if err != nil {
		c.JSON(500, gin.H{"code": "announcements_failed", "error": err.Error()})
		return
	}
	count, err := s.admin.RecipientCount(c)
	if err != nil {
		c.JSON(500, gin.H{"code": "recipients_failed", "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"announcements": sent, "recipients": count})
}

// POST /admin/announcements {subject, body, confirm: true} emails every subscribed user.
func (s *server) sendAnnouncement(c *gin.Context) {
	var in struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
		Confirm bool   `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"code": "invalid_body", "error": err.Error()})
		return
	}
	if !in.Confirm {
		c.JSON(400, gin.H{"code": "not_confirmed", "error": "sending to everyone has to be confirmed"})
		return
	}
	sent, err := s.announcer.Send(c, c.GetString("user_id"), c.GetString("operator_email"), in.Subject, in.Body)
	if err != nil {
		c.JSON(400, gin.H{"code": "announce_failed", "error": err.Error()})
		return
	}
	c.JSON(201, sent)
}
