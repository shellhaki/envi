package api

import (
	"time"

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
func Build(a auth.Service, t auth.TokenStore, p project.Service, s secret.Service, au audit.Service, st service_token.Service, i invitation.Service, db *pgxpool.Pool, dev auth.DeviceService, webURL string, accessTTL time.Duration) *gin.Engine {
	r := New()
	AuthHandler{Service: a, Tokens: t}.Routes(r)
	m := RequireAuth(t, st)
	DeviceHandler{Service: dev, WebURL: webURL, AccessTTL: accessTTL}.Routes(r, m)
	ProjectHandler{Service: p}.RoutesProtected(r, m)
	SecretHandler{Service: s}.Routes(r, m)
	AuditHandler{Service: au}.Routes(r, m)
	ServiceTokenHandler{Service: st}.Routes(r, m)
	InvitationHandler{Service: i}.Routes(r, m)
	AccountHandler{DB: db}.Routes(r, m)
	return r
}
