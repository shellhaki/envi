package api

import (
	"net/http"
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

		userID, ceiling, err := auth.UserForAccessToken(db, token)
		if err == nil {
			if reason := blockedByCeiling(c, ceiling); reason != "" {
				c.AbortWithStatusJSON(403, gin.H{"code": "insufficient_permission", "error": reason})
				return
			}
			c.Set("user_id", userID)
			c.Set("user_permission", ceiling)
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

// blockedByCeiling enforces the limit an API key puts on the session it created.
// It returns why the request is refused, or "" to allow it.
//
// The check is deliberately coarse, by method and address rather than by what
// the handler would go on to do: a ceiling is a blunt "this key may not do
// that", and the per-environment permission check in internal/access still runs
// afterwards either way.
//
// An ordinary login has no ceiling and is never affected.
func blockedByCeiling(c *gin.Context, ceiling string) string {
	switch ceiling {
	case "":
		return ""
	case "read":
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			return "this api key is read-only"
		}
		return ""
	case "write":
		// Writing secrets is fine; handing out access or minting credentials is
		// not, or a write key could quietly promote itself.
		if grantsAccessOrCredentials(c.FullPath()) {
			return "this api key may not manage access or credentials"
		}
		return ""
	default: // "manage"
		return ""
	}
}

func grantsAccessOrCredentials(path string) bool {
	for _, guarded := range []string{"/api-keys", "/invitations", "/service-tokens", "/collaborators"} {
		if strings.Contains(path, guarded) {
			return true
		}
	}
	return false
}
