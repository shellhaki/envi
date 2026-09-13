package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"shellhaki/envi/internal/invitation"
)

type InvitationHandler struct{ Service invitation.Service }

func (h InvitationHandler) Routes(r *gin.Engine, m gin.HandlerFunc) {
	// Preview is deliberately public: a visitor needs to see what an
	// invitation link is for before they've signed in (or before they even
	// have an account) so the accept page can walk them through login/signup.
	// The token itself is an unguessable 32-byte value, so this doesn't expose
	// invitations to enumeration.
	r.GET("/invitations/:token", h.preview)
	r.POST("/projects/:id/invitations", m, h.create)
	r.POST("/invitations/accept", m, h.accept)
	r.GET("/projects/:id/collaborators", m, h.list)
	r.DELETE("/projects/:id/invitations/:invitationID", m, h.revokeInvitation)
	r.DELETE("/projects/:id/collaborators/:grantID", m, h.revokeGrant)
}
func (h InvitationHandler) preview(c *gin.Context) {
	p, err := h.Service.Preview(c, c.Param("token"))
	if err == invitation.ErrForbidden {
		c.JSON(404, gin.H{"code": "not_found", "error": "invitation not found or expired"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "unable to load invitation"})
		return
	}
	c.JSON(200, p)
}
func (h InvitationHandler) create(c *gin.Context) {
	var in struct {
		Email         string `json:"email"`
		EnvironmentID string `json:"environment_id"`
		Permission    string `json:"permission"`
		TTL           int    `json:"ttl_seconds"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Email == "" {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "email required"})
		return
	}
	if in.Permission == "" {
		in.Permission = "read"
	}
	i, err := h.Service.Create(c, c.GetString("user_id"), c.Param("id"), in.EnvironmentID, in.Email, in.Permission, time.Duration(in.TTL)*time.Second)
	if err == invitation.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if err == invitation.ErrRateLimited {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": "rate_limited", "error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, i)
}
func (h InvitationHandler) accept(c *gin.Context) {
	var in struct {
		Token string `json:"token"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Token == "" {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "token required"})
		return
	}
	if err := h.Service.Accept(c, c.GetString("user_id"), in.Token); err != nil {
		c.JSON(403, gin.H{"code": "forbidden", "error": "invitation invalid or expired"})
		return
	}
	c.Status(204)
}
func (h InvitationHandler) list(c *gin.Context) {
	cs, err := h.Service.ListCollaborators(c, c.GetString("user_id"), c.Param("id"))
	if err == invitation.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "unable to load collaborators"})
		return
	}
	c.JSON(200, cs)
}
func (h InvitationHandler) revokeInvitation(c *gin.Context) {
	err := h.Service.RevokeInvitation(c, c.GetString("user_id"), c.Param("id"), c.Param("invitationID"))
	if err == invitation.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "unable to revoke invitation"})
		return
	}
	c.Status(204)
}
func (h InvitationHandler) revokeGrant(c *gin.Context) {
	err := h.Service.RevokeGrant(c, c.GetString("user_id"), c.Param("id"), c.Param("grantID"))
	if err == invitation.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "unable to remove collaborator"})
		return
	}
	c.Status(204)
}
