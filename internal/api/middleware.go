package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/service_token"
)

// RequireAuth accepts a user's access token or a service token, and answers 401 to anything else.
// A user sets "user_id"; a service token sets "service_id", "service_env" and "service_permission".
func RequireAuth(db *pgxpool.Pool, serviceTokens service_token.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")

		if userID, ceiling, err := auth.UserForAccessToken(db, token); err == nil {
			if reason := blockedByCeiling(c, ceiling); reason != "" {
				c.AbortWithStatusJSON(403, gin.H{"code": "insufficient_permission", "error": reason})
				return
			}
			c.Set("user_id", userID)
			c.Set("user_permission", ceiling)
			c.Next()
			return
		}

		if serviceID, envID, permission, err := serviceTokens.Authenticate(c, token); err == nil {
			c.Set("service_id", serviceID)
			c.Set("service_env", envID)
			c.Set("service_permission", permission)
			c.Next()
			return
		}

		c.AbortWithStatusJSON(401, gin.H{"code": "unauthenticated", "error": "authentication required"})
	}
}

// blockedByCeiling enforces the limit an API key puts on its session, returning why it refuses or "".
// A read key may only read; a write key may not hand out access or create credentials.
func blockedByCeiling(c *gin.Context, ceiling string) string {
	switch ceiling {
	case "read":
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			return "this api key is read-only"
		}
	case "write":
		for _, path := range []string{"/api-keys", "/invitations", "/service-tokens", "/collaborators"} {
			if strings.Contains(c.FullPath(), path) {
				return "this api key may not manage access or credentials"
			}
		}
	}
	return ""
}
