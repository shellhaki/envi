package api

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/admin"
	"shellhaki/envi/internal/announce"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/billing"
	"shellhaki/envi/internal/config"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/kv"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
	"shellhaki/envi/internal/service_token"
)

// server holds everything the handlers need. Every handler is a method on it.
type server struct {
	db            *pgxpool.Pool
	login         auth.LoginSettings
	projects      project.Service
	secrets       secret.Service
	kv            kv.Store
	audit         audit.Service
	serviceTokens service_token.Service
	invitations   invitation.Service
	webURL        string
	deployment    config.Deployment
	announcer     announce.Service
	billing       billing.Service
	admin         admin.Service
}

// Build returns the HTTP router with every route registered.
func Build(db *pgxpool.Pool, login auth.LoginSettings, projects project.Service, secrets secret.Service, values kv.Store, auditLog audit.Service, serviceTokens service_token.Service, invitations invitation.Service, webURL string, deployment config.Deployment, announcer announce.Service, bill billing.Service) *gin.Engine {
	s := &server{
		db: db, login: login, projects: projects, secrets: secrets, kv: values,
		audit: auditLog, serviceTokens: serviceTokens, invitations: invitations,
		webURL: webURL, deployment: deployment, announcer: announcer, billing: bill,
		admin: admin.Service{DB: db},
	}

	r := gin.New()
	signedIn := RequireAuth(db, serviceTokens)

	r.GET("/health", s.health)

	// Sign-in
	r.POST("/auth/request-otp", s.requestLoginCode)
	r.POST("/auth/verify-otp", s.verifyLoginCode)
	r.POST("/auth/refresh", s.refreshSession)
	r.POST("/auth/logout", s.logout)
	r.POST("/auth/api-key", s.exchangeAPIKey)

	// CLI sign-in through the browser
	r.POST("/auth/device/code", s.startDeviceLogin)
	r.POST("/auth/device/token", s.finishDeviceLogin)
	r.POST("/auth/device/approve", signedIn, s.approveDeviceLogin)
	r.POST("/auth/device/deny", signedIn, s.denyDeviceLogin)

	// Account and API keys
	r.GET("/me", signedIn, s.me)
	r.POST("/me/api-keys", signedIn, s.createAPIKey)
	r.GET("/me/api-keys", signedIn, s.listAPIKeys)
	r.DELETE("/me/api-keys/:id", signedIn, s.revokeAPIKey)

	// Projects and environments
	r.POST("/projects", signedIn, s.createProject)
	r.GET("/projects", signedIn, s.listProjects)
	r.DELETE("/projects/:id", signedIn, s.deleteProject)
	r.POST("/projects/:id/environments", signedIn, s.createEnvironment)
	r.GET("/projects/:id/environments", signedIn, s.listEnvironments)
	r.PATCH("/environments/:id", signedIn, s.updateEnvironment)
	r.DELETE("/environments/:id", signedIn, s.deleteEnvironment)

	// Secrets
	r.GET("/environments/:id/secrets", signedIn, s.getSecrets)
	r.GET("/environments/:id/secrets/snapshot", signedIn, s.getSnapshot)
	r.PUT("/environments/:id/secrets", signedIn, s.putSecrets)
	r.PUT("/environments/:id/secrets/snapshot", signedIn, s.putSnapshot)
	r.DELETE("/environments/:id/secrets/:key", signedIn, s.deleteSecret)
	r.GET("/values", signedIn, s.getValues)

	// Key-value store
	r.GET("/kv", signedIn, s.listKV)
	r.PUT("/kv", signedIn, s.setKV)
	r.DELETE("/kv", signedIn, s.deleteKV)

	// Service tokens
	r.POST("/projects/:id/environments/:env/service-tokens", signedIn, s.createServiceToken)
	r.DELETE("/service-tokens", signedIn, s.revokeServiceToken)

	// Invitations and collaborators
	r.GET("/invitations/:token", s.previewInvitation)
	r.POST("/projects/:id/invitations", signedIn, s.createInvitation)
	r.POST("/invitations/accept", signedIn, s.acceptInvitation)
	r.GET("/projects/:id/collaborators", signedIn, s.listCollaborators)
	r.DELETE("/projects/:id/invitations/:invitationID", signedIn, s.revokeInvitation)
	r.DELETE("/projects/:id/collaborators/:grantID", signedIn, s.removeCollaborator)

	// Audit log
	r.GET("/orgs/:id/audit-events", signedIn, s.listAuditEvents)

	// Billing (the caller's own organization)
	r.GET("/billing", signedIn, s.billingOverview)
	r.POST("/billing/payg", signedIn, s.startPAYG)
	r.POST("/billing/team", signedIn, s.startTeam)
	r.POST("/billing/cancel", signedIn, s.cancelPlan)
	r.POST("/billing/portal", signedIn, s.billingPortal)
	r.POST("/billing/retry", signedIn, s.retryCharge)
	r.POST("/webhooks/bachs", s.bachsWebhook)

	// Operators (answers 404 to everyone else)
	operator := r.Group("/admin", signedIn, RequireOperator(db))
	operator.GET("/whoami", s.whoami)
	operator.GET("/stats", RequireManage(), s.adminStats)
	operator.POST("/expenses", RequireManage(), s.addExpense)
	operator.DELETE("/expenses/:id", RequireManage(), s.deleteExpense)
	operator.GET("/operators", RequireManage(), s.listOperators)
	operator.POST("/operators", RequireManage(), s.addOperator)
	operator.DELETE("/operators/:id", RequireManage(), s.removeOperator)
	operator.GET("/announcements", RequireAnnounce(), s.listAnnouncements)
	operator.POST("/announcements", RequireAnnounce(), s.sendAnnouncement)

	return r
}

func (s *server) health(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}
