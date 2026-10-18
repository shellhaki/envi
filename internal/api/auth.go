package api

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/apikey"
	"shellhaki/envi/internal/auth"
)

// POST /auth/request-otp {email}
func (s *server) requestLoginCode(c *gin.Context) {
	var in struct {
		Email string `json:"email" binding:"required,email"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid email required"})
		return
	}
	if err := auth.SendLoginCode(c, s.login, in.Email); err != nil {
		log.Printf("otp delivery failed: %v", err)
		if strings.Contains(err.Error(), "too many OTP requests") {
			c.JSON(http.StatusTooManyRequests, gin.H{"code": "rate_limited", "error": "too many OTP requests"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to send code"})
		return
	}
	c.Status(http.StatusAccepted)
}

// POST /auth/verify-otp {email, code}
func (s *server) verifyLoginCode(c *gin.Context) {
	var in struct {
		Email string `json:"email" binding:"required,email"`
		Code  string `json:"code" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and code required"})
		return
	}
	userID, err := auth.CheckLoginCode(c, s.login, in.Email, in.Code)
	if err != nil {
		log.Printf("otp verify failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired OTP"})
		return
	}
	access, refresh, err := auth.StartSession(s.db, userID)
	if err != nil {
		log.Printf("session persistence failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to create session"})
		return
	}
	c.JSON(http.StatusOK, tokensReply(access, refresh))
}

// POST /auth/refresh {refresh_token}
func (s *server) refreshSession(c *gin.Context) {
	var in struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token required"})
		return
	}
	access, refresh, err := auth.RefreshSession(s.db, in.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}
	c.JSON(http.StatusOK, tokensReply(access, refresh))
}

// POST /auth/logout {refresh_token}
func (s *server) logout(c *gin.Context) {
	var in struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token required"})
		return
	}
	if err := auth.EndSession(s.db, in.RefreshToken); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /auth/api-key {key} trades a personal API key for a session capped at the key's permission.
func (s *server) exchangeAPIKey(c *gin.Context) {
	var in struct {
		Key string `json:"key" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "key required"})
		return
	}
	userID, permission, keyID, err := apikey.Authenticate(c, s.db, in.Key)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "invalid_key", "error": "invalid or expired api key"})
		return
	}
	access, refresh, err := auth.StartLimitedSession(s.db, userID, permission, keyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to create session"})
		return
	}
	c.JSON(http.StatusOK, tokensReply(access, refresh))
}

func tokensReply(access, refresh string) gin.H {
	return gin.H{
		"access_token":  access,
		"refresh_token": refresh,
		"expires_in":    int(auth.AccessTokenLifetime.Seconds()),
	}
}
