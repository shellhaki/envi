package api

// The login, refresh and logout web addresses. Each handler reads the JSON the
// client sent, calls the matching function in internal/auth, and writes a JSON
// reply. The real work happens in internal/auth; this file only translates
// between HTTP and those functions.

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/auth"
)

// addAuthRoutes connects each web address to its handler.
//
// gin wants every handler to take exactly one thing, the request (c). Ours
// also need the database or the login settings, so each line wraps the call in
// a small function that passes them along.
func addAuthRoutes(router *gin.Engine, db *pgxpool.Pool, login auth.LoginSettings) {
	router.POST("/auth/request-otp", func(c *gin.Context) { handleSendLoginCode(c, login) })
	router.POST("/auth/verify-otp", func(c *gin.Context) { handleCheckLoginCode(c, db, login) })
	router.POST("/auth/refresh", func(c *gin.Context) { handleRefresh(c, db) })
	router.POST("/auth/logout", func(c *gin.Context) { handleLogout(c, db) })
}

// POST /auth/request-otp   {"email": "..."}
func handleSendLoginCode(c *gin.Context, login auth.LoginSettings) {
	var request struct {
		Email string `json:"email" binding:"required,email"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid email required"})
		return
	}

	err := auth.SendLoginCode(c, login, request.Email)
	if err == auth.ErrNotInvited {
		c.JSON(http.StatusForbidden, gin.H{"code": "not_invited", "error": "this email isn't on the beta list yet"})
		return
	}
	if err != nil {
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

// POST /auth/verify-otp   {"email": "...", "code": "123456"}
func handleCheckLoginCode(c *gin.Context, db *pgxpool.Pool, login auth.LoginSettings) {
	var request struct {
		Email string `json:"email" binding:"required,email"`
		Code  string `json:"code" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and code required"})
		return
	}

	userID, err := auth.CheckLoginCode(c, login, request.Email, request.Code)
	if err == auth.ErrNotInvited {
		c.JSON(http.StatusForbidden, gin.H{"code": "not_invited", "error": "this email isn't on the beta list yet"})
		return
	}
	if err != nil {
		log.Printf("otp verify failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired OTP"})
		return
	}

	accessToken, refreshToken, err := auth.StartSession(db, userID)
	if err != nil {
		log.Printf("session persistence failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to create session"})
		return
	}
	c.JSON(http.StatusOK, tokensReply(accessToken, refreshToken))
}

// POST /auth/refresh   {"refresh_token": "..."}
func handleRefresh(c *gin.Context, db *pgxpool.Pool) {
	var request struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token required"})
		return
	}

	accessToken, refreshToken, err := auth.RefreshSession(db, request.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}
	c.JSON(http.StatusOK, tokensReply(accessToken, refreshToken))
}

// POST /auth/logout   {"refresh_token": "..."}
func handleLogout(c *gin.Context, db *pgxpool.Pool) {
	var request struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token required"})
		return
	}

	err := auth.EndSession(db, request.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}
	c.Status(http.StatusNoContent)
}

// tokensReply is the JSON a client gets after logging in or refreshing.
// expires_in tells it how many seconds until it should refresh, so it can do
// that before a request fails instead of after.
func tokensReply(accessToken string, refreshToken string) gin.H {
	return gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    int(auth.AccessTokenLifetime.Seconds()),
	}
}
