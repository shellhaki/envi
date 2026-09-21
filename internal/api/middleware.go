package api

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/service_token"
)

// RequireAuth guards the web addresses that need someone logged in. It runs
// before the real handler, reads the "Authorization: Bearer <token>" header, and
// works out who is calling:
//
//   - a person, via an access token from logging in: stores "user_id"
//   - a machine, via a service token (CI, envi run in Docker): stores
//     "service_id", "service_env" and "service_permission"
//
// If the token is neither, the request stops here with a 401.
func RequireAuth(db *pgxpool.Pool, serviceTokens service_token.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")

		userID, err := auth.UserForAccessToken(db, token)
		if err == nil {
			c.Set("user_id", userID)
			c.Next()
			return
		}

		serviceID, environmentID, permission, err := serviceTokens.Authenticate(c, token)
		if err == nil {
			c.Set("service_id", serviceID)
			c.Set("service_env", environmentID)
			c.Set("service_permission", permission)
			c.Next()
			return
		}

		c.AbortWithStatusJSON(401, gin.H{"code": "unauthenticated", "error": "authentication required"})
	}
}
