package api

import (
	"github.com/gin-gonic/gin"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/service_token"
	"strings"
)

func RequireAuth(tokens auth.TokenStore, services service_token.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		v := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		u, e := tokens.Authenticate(v)
		if e != nil {
			id, env, permission, x := services.Authenticate(c, v)
			if x != nil {
				c.AbortWithStatusJSON(401, gin.H{"code": "unauthenticated", "error": "authentication required"})
				return
			}
			c.Set("service_id", id)
			c.Set("service_env", env)
			c.Set("service_permission", permission)
			c.Next()
			return
		}
		c.Set("user_id", u)
		c.Next()
	}
}
