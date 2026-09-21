package api

// The web addresses for personal API keys.
//
// POST /auth/api-key is open, because a key is exactly the credential you use
// when you don't have a session yet. The rest need you to be logged in.

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/apikey"
	"shellhaki/envi/internal/auth"
)

func addAPIKeyRoutes(router *gin.Engine, db *pgxpool.Pool, requireLogin gin.HandlerFunc) {
	router.POST("/auth/api-key", func(c *gin.Context) { handleExchangeAPIKey(c, db) })
	router.POST("/me/api-keys", requireLogin, func(c *gin.Context) { handleCreateAPIKey(c, db) })
	router.GET("/me/api-keys", requireLogin, func(c *gin.Context) { handleListAPIKeys(c, db) })
	router.DELETE("/me/api-keys/:id", requireLogin, func(c *gin.Context) { handleRevokeAPIKey(c, db) })
}

// POST /auth/api-key   {"key": "envi_..."}
//
// Trades a key for an ordinary session. The session carries the key's ceiling,
// so a read-only key can never produce a session that writes.
func handleExchangeAPIKey(c *gin.Context, db *pgxpool.Pool) {
	var request struct {
		Key string `json:"key" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "key required"})
		return
	}

	userID, permission, keyID, err := apikey.Authenticate(c, db, request.Key)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "invalid_key", "error": "invalid or expired api key"})
		return
	}

	accessToken, refreshToken, err := auth.StartLimitedSession(db, userID, permission, keyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to create session"})
		return
	}
	c.JSON(http.StatusOK, tokensReply(accessToken, refreshToken))
}

// POST /me/api-keys   {"name": "...", "permission": "read", "ttl_seconds": 0}
func handleCreateAPIKey(c *gin.Context, db *pgxpool.Pool) {
	var request struct {
		Name       string `json:"name" binding:"required"`
		Permission string `json:"permission"`
		// Seconds until the key expires. Omitted means the default; 0 asks for
		// a key that never expires, so the field has to be a pointer to tell
		// "not given" apart from "zero".
		TTLSeconds *int64 `json:"ttl_seconds"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "name required"})
		return
	}
	if request.Permission == "" {
		request.Permission = "read"
	}

	lifetime := apikey.DefaultLifetime
	if request.TTLSeconds != nil {
		lifetime = time.Duration(*request.TTLSeconds) * time.Second
	}

	key, err := apikey.Create(c, db, c.GetString("user_id"), request.Name, request.Permission, lifetime)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": err.Error()})
		return
	}
	// The only time the secret is ever returned.
	c.JSON(http.StatusCreated, key)
}

// GET /me/api-keys
func handleListAPIKeys(c *gin.Context, db *pgxpool.Pool) {
	keys, err := apikey.List(c, db, c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to list api keys"})
		return
	}
	c.JSON(http.StatusOK, keys)
}

// DELETE /me/api-keys/:id
func handleRevokeAPIKey(c *gin.Context, db *pgxpool.Pool) {
	err := apikey.Revoke(c, db, c.GetString("user_id"), c.Param("id"))
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
