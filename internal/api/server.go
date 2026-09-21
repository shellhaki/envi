package api

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
	"shellhaki/envi/internal/service_token"
)

func New() *gin.Engine {
	r := gin.New()
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	return r
}
func Build(db *pgxpool.Pool, login auth.LoginSettings, p project.Service, s secret.Service, au audit.Service, st service_token.Service, i invitation.Service, webURL string, beta bool) *gin.Engine {
	r := New()
	// Public, unauthenticated: the web app checks this before a user has
	// signed in, to show "private beta" messaging rather than a bare
	// rejected-login error.
	r.GET("/config", func(c *gin.Context) { c.JSON(200, gin.H{"beta": beta}) })
	addAuthRoutes(r, db, login)
	m := RequireAuth(db, st)
	addDeviceRoutes(r, db, webURL, m)
	ProjectHandler{Service: p}.RoutesProtected(r, m)
	SecretHandler{Service: s}.Routes(r, m)
	AuditHandler{Service: au}.Routes(r, m)
	ServiceTokenHandler{Service: st}.Routes(r, m)
	InvitationHandler{Service: i}.Routes(r, m)
	AccountHandler{DB: db}.Routes(r, m)
	return r
}
