package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/service_token"
)

// POST /projects/:id/environments/:env/service-tokens {name, permission, ttl_seconds}
func (s *server) createServiceToken(c *gin.Context) {
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
	ttl := time.Duration(in.TTL) * time.Second
	token, err := s.serviceTokens.Create(c, c.GetString("user_id"), c.Param("id"), c.Param("env"), in.Name, in.Permission, ttl)
	if err == service_token.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(409, gin.H{"code": "conflict", "error": "token could not be created"})
		return
	}
	c.JSON(http.StatusCreated, token)
}

// DELETE /service-tokens with the token to revoke in the X-Service-Token header.
func (s *server) revokeServiceToken(c *gin.Context) {
	token := strings.TrimSpace(c.GetHeader("X-Service-Token"))
	if token == "" {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "service token required"})
		return
	}
	if err := s.serviceTokens.Revoke(c, c.GetString("user_id"), token); err != nil {
		c.JSON(404, gin.H{"code": "not_found", "error": "token not found"})
		return
	}
	c.Status(204)
}
