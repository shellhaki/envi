package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/apikey"
)

// GET /me returns the signed-in user and their personal organization.
func (s *server) me(c *gin.Context) {
	var out struct {
		ID, Email, OrganizationID string
	}
	err := s.db.QueryRow(c, `
		SELECT u.id, u.email, o.id
		  FROM users u
		  JOIN memberships m ON m.user_id = u.id
		  JOIN organizations o ON o.id = m.org_id AND o.type = 'personal'
		 WHERE u.id = $1`, c.GetString("user_id")).Scan(&out.ID, &out.Email, &out.OrganizationID)
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "account unavailable"})
		return
	}
	c.JSON(200, out)
}

// POST /me/api-keys {name, permission, ttl_seconds}. The key's secret is only ever returned here.
func (s *server) createAPIKey(c *gin.Context) {
	var in struct {
		Name       string `json:"name" binding:"required"`
		Permission string `json:"permission"`
		TTLSeconds *int64 `json:"ttl_seconds"` // missing means the default; 0 means never expires
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "name required"})
		return
	}
	if in.Permission == "" {
		in.Permission = "read"
	}
	lifetime := apikey.DefaultLifetime
	if in.TTLSeconds != nil {
		lifetime = time.Duration(*in.TTLSeconds) * time.Second
	}
	key, err := apikey.Create(c, s.db, c.GetString("user_id"), in.Name, in.Permission, lifetime)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, key)
}

// GET /me/api-keys
func (s *server) listAPIKeys(c *gin.Context) {
	keys, err := apikey.List(c, s.db, c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to list api keys"})
		return
	}
	c.JSON(http.StatusOK, keys)
}

// DELETE /me/api-keys/:id
func (s *server) revokeAPIKey(c *gin.Context) {
	err := apikey.Revoke(c, s.db, c.GetString("user_id"), c.Param("id"))
	if err == apikey.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "error": "api key not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to revoke api key"})
		return
	}
	c.Status(http.StatusNoContent)
}
