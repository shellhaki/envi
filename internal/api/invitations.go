package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/billing"
	"shellhaki/envi/internal/invitation"
)

// GET /invitations/:token is public, so the accept page can show what the link is for before sign-in.
func (s *server) previewInvitation(c *gin.Context) {
	preview, err := s.invitations.Preview(c, c.Param("token"))
	if err == invitation.ErrForbidden {
		c.JSON(404, gin.H{"code": "not_found", "error": "invitation not found or expired"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "unable to load invitation"})
		return
	}
	c.JSON(200, preview)
}

// POST /projects/:id/invitations {email, environment_id, permission, ttl_seconds}
func (s *server) createInvitation(c *gin.Context) {
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
	ttl := time.Duration(in.TTL) * time.Second
	created, err := s.invitations.Create(c, c.GetString("user_id"), c.Param("id"), in.EnvironmentID, in.Email, in.Permission, ttl)
	if err == invitation.ErrForbidden {
		forbidden(c)
		return
	}
	if refusal, ok := billing.IsRefusal(err); ok {
		c.JSON(http.StatusPaymentRequired, refusal)
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
	c.JSON(http.StatusCreated, created)
}

// POST /invitations/accept {token}
func (s *server) acceptInvitation(c *gin.Context) {
	var in struct {
		Token string `json:"token"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Token == "" {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "token required"})
		return
	}
	err := s.invitations.Accept(c, c.GetString("user_id"), in.Token)
	if refusal, ok := billing.IsRefusal(err); ok {
		refusal.Message = "this invitation can't be accepted yet: the organization that sent it needs to update its plan"
		c.JSON(http.StatusPaymentRequired, refusal)
		return
	}
	if err != nil {
		c.JSON(403, gin.H{"code": "forbidden", "error": "invitation invalid or expired"})
		return
	}
	c.Status(204)
}

// GET /projects/:id/collaborators
func (s *server) listCollaborators(c *gin.Context) {
	list, err := s.invitations.ListCollaborators(c, c.GetString("user_id"), c.Param("id"))
	if err == invitation.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "unable to load collaborators"})
		return
	}
	c.JSON(200, list)
}

// DELETE /projects/:id/invitations/:invitationID
func (s *server) revokeInvitation(c *gin.Context) {
	err := s.invitations.RevokeInvitation(c, c.GetString("user_id"), c.Param("id"), c.Param("invitationID"))
	if err == invitation.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "unable to revoke invitation"})
		return
	}
	c.Status(204)
}

// DELETE /projects/:id/collaborators/:grantID
func (s *server) removeCollaborator(c *gin.Context) {
	err := s.invitations.RevokeGrant(c, c.GetString("user_id"), c.Param("id"), c.Param("grantID"))
	if err == invitation.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "unable to remove collaborator"})
		return
	}
	c.Status(204)
}
