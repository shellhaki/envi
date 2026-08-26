package api

import (
	"github.com/gin-gonic/gin"
	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/secret"
)

type SecretHandler struct{ Service secret.Service }

func (h SecretHandler) Routes(r *gin.Engine, m gin.HandlerFunc) {
	g := r.Group("/environments/:id", m)
	g.GET("/secrets", h.get)
	g.PUT("/secrets", h.put)
	g.DELETE("/secrets/:key", h.delete)
}
func (h SecretHandler) delete(c *gin.Context) {
	e := h.Service.Delete(c, c.GetString("user_id"), c.Param("id"), c.Param("key"))
	if e == access.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if e != nil {
		c.JSON(404, gin.H{"code": "not_found", "error": "secret not found"})
		return
	}
	c.Status(204)
}
func (h SecretHandler) get(c *gin.Context) {
	v, e := h.Service.Get(c, c.GetString("user_id"), c.Param("id"))
	if e == access.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	c.JSON(200, v)
}
func (h SecretHandler) put(c *gin.Context) {
	var in map[string]string
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "secret map required"})
		return
	}
	for k, v := range in {
		if e := h.Service.Put(c, c.GetString("user_id"), c.Param("id"), k, v); e != nil {
			if e == access.ErrForbidden {
				c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
			} else {
				c.JSON(500, gin.H{"code": "internal", "error": "secret write failed"})
			}
			return
		}
	}
	c.Status(204)
}
