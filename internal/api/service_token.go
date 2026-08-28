package api

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"shellhaki/envi/internal/service_token"
	"strings"
	"time"
)

type ServiceTokenHandler struct{ Service service_token.Service }

func (h ServiceTokenHandler) Routes(r *gin.Engine, m gin.HandlerFunc) {
	r.POST("/projects/:project/environments/:env/service-tokens", m, h.create)
	r.DELETE("/service-tokens", m, h.revoke)
}
func (h ServiceTokenHandler) create(c *gin.Context) {
	var in struct {
		Name       string `json:"name"`
		Permission string `json:"permission"`
		TTL        int    `json:"ttl_seconds"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Name == "" {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "name required"})
		return
	}
	if in.Permission == "" {
		in.Permission = "read"
	}
	t, e := h.Service.Create(c, c.GetString("user_id"), c.Param("project"), c.Param("env"), in.Name, in.Permission, time.Duration(in.TTL)*time.Second)
	if e == service_token.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if e != nil {
		c.JSON(409, gin.H{"code": "conflict", "error": "token could not be created"})
		return
	}
	c.JSON(http.StatusCreated, t)
}
func (h ServiceTokenHandler) revoke(c *gin.Context) {
	v := strings.TrimSpace(c.GetHeader("X-Service-Token"))
	if v == "" {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "service token required"})
		return
	}
	if e := h.Service.Revoke(c, c.GetString("user_id"), v); e != nil {
		c.JSON(404, gin.H{"code": "not_found", "error": "token not found"})
		return
	}
	c.Status(204)
}
