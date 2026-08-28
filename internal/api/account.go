package api

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AccountHandler struct{ DB *pgxpool.Pool }

func (h AccountHandler) Routes(r *gin.Engine, m gin.HandlerFunc) {
	r.GET("/me", m, h.get)
}
func (h AccountHandler) get(c *gin.Context) {
	var out struct {
		ID, Email, OrganizationID string
	}
	e := h.DB.QueryRow(c, `SELECT u.id,u.email,o.id FROM users u JOIN memberships m ON m.user_id=u.id JOIN organizations o ON o.id=m.org_id AND o.type='personal' WHERE u.id=$1`, c.GetString("user_id")).Scan(&out.ID, &out.Email, &out.OrganizationID)
	if e != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "account unavailable"})
		return
	}
	c.JSON(200, out)
}
