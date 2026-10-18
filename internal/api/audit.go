package api

import (
	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/audit"
)

// GET /orgs/:id/audit-events
func (s *server) listAuditEvents(c *gin.Context) {
	events, err := s.audit.List(c, c.GetString("user_id"), c.Param("id"))
	if err == audit.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "audit events unavailable"})
		return
	}
	c.JSON(200, events)
}
