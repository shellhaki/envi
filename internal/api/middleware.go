package api

import (
	"github.com/gin-gonic/gin"
	"shellhaki/envi/internal/auth"
	"strings"
)

func RequireAuth(tokens auth.TokenStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		v := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		u, e := tokens.Authenticate(v)
		if e != nil {
			c.AbortWithStatusJSON(401, gin.H{"code": "unauthenticated", "error": "authentication required"})
			return
		}
		c.Set("user_id", u)
		c.Next()
	}
}
