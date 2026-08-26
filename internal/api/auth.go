package api

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"shellhaki/envi/internal/auth"
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
		_ = h.Tokens.Save(u, r, a)
	}
	c.JSON(200, gin.H{"access_token": a, "refresh_token": r})
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
	c.JSON(200, gin.H{"access_token": a, "refresh_token": r})
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
