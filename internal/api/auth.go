package api

import (
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"shellhaki/envi/internal/auth"
	"strings"
)

type AuthHandler struct {
	Service auth.Service
	Tokens  auth.TokenStore
}

func (h AuthHandler) Routes(r *gin.Engine) {
	r.POST("/auth/request-otp", h.request)
	r.POST("/auth/verify-otp", h.verify)
	r.POST("/auth/refresh", h.refresh)
	r.POST("/auth/logout", h.logout)
}
func (h AuthHandler) request(c *gin.Context) {
	var in struct {
		Email string `json:"email" binding:"required,email"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid email required"})
		return
	}
	if err := h.Service.Request(c, in.Email); err != nil {
		log.Printf("otp delivery failed: %v", err)
		if strings.Contains(err.Error(), "too many OTP requests") {
			c.JSON(http.StatusTooManyRequests, gin.H{"code": "rate_limited", "error": "too many OTP requests"})
			return
		}
		c.JSON(500, gin.H{"error": "unable to send code"})
		return
	}
	c.Status(http.StatusAccepted)
}
func (h AuthHandler) verify(c *gin.Context) {
	var in struct {
		Email string `json:"email" binding:"required,email"`
		Code  string `json:"code" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "email and code required"})
		return
	}
	u, a, r, err := h.Service.Verify(c, in.Email, in.Code)
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid or expired OTP"})
		return
	}
	if h.Tokens != nil {
		if err = h.Tokens.Save(u, r, a); err != nil {
			log.Printf("session persistence failed: %v", err)
			c.JSON(500, gin.H{"error": "unable to create session"})
			return
		}
	}
	c.JSON(200, h.session(a, r))
}
func (h AuthHandler) refresh(c *gin.Context) {
	var in struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "refresh_token required"})
		return
	}
	a, r, err := h.Service.Refresh(in.RefreshToken, h.Tokens)
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid refresh token"})
		return
	}
	c.JSON(200, h.session(a, r))
}

// session reports the access token lifetime alongside the pair so clients can
// refresh before expiry instead of discovering it through a failed request.
func (h AuthHandler) session(access, refresh string) gin.H {
	return gin.H{"access_token": access, "refresh_token": refresh, "expires_in": int(h.Service.AccessTTL.Seconds())}
}
func (h AuthHandler) logout(c *gin.Context) {
	var in struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "refresh_token required"})
		return
	}
	if err := h.Service.Logout(in.RefreshToken, h.Tokens); err != nil {
		c.JSON(401, gin.H{"error": "invalid refresh token"})
		return
	}
	c.Status(204)
}
