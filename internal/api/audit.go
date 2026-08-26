package api

import (
	"github.com/gin-gonic/gin"
	"shellhaki/envi/internal/audit"
)

type AuditHandler struct{ Service audit.Service }

func (h AuditHandler) Routes(r *gin.Engine, m gin.HandlerFunc) {
	r.GET("/orgs/:id/audit-events", m, h.list)
}
func (h AuditHandler) list(c *gin.Context) {
	v, e := h.Service.List(c, c.GetString("user_id"), c.Param("id"))
	if e == audit.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "audit events unavailable"})
		return
	}
	c.JSON(200, v)
}
