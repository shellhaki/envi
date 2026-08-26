package api

import (
	"github.com/gin-gonic/gin"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
)

func New() *gin.Engine {
	r := gin.New()
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	return r
}
func Build(a auth.Service, t auth.TokenStore, p project.Service, s secret.Service, au audit.Service) *gin.Engine {
	r := New()
	AuthHandler{Service: a, Tokens: t}.Routes(r)
	m := RequireAuth(t)
	ProjectHandler{Service: p}.RoutesProtected(r, m)
	SecretHandler{Service: s}.Routes(r, m)
	AuditHandler{Service: au}.Routes(r, m)
	return r
}
