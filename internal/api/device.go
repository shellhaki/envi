package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/auth"
)

// POST /auth/device/code, called by the CLI to start a browser login.
func (s *server) startDeviceLogin(c *gin.Context) {
	deviceCode, userCode, expiresIn, interval, err := auth.StartDeviceLogin(s.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "server_error", "error": "unable to start device authorization"})
		return
	}
	page := strings.TrimRight(s.webURL, "/") + "/device"
	c.JSON(http.StatusOK, gin.H{
		"device_code":               deviceCode,
		"user_code":                 userCode,
		"verification_uri":          page,
		"verification_uri_complete": page + "?code=" + url.QueryEscape(userCode),
		"expires_in":                expiresIn,
		"interval":                  interval,
	})
}

// POST /auth/device/token {device_code}, polled by the CLI until the browser approves.
func (s *server) finishDeviceLogin(c *gin.Context) {
	var in struct {
		DeviceCode string `json:"device_code" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "device_code required"})
		return
	}
	access, refresh, err := auth.FinishDeviceLogin(s.db, in.DeviceCode)
	var pending auth.DevicePending
	if errors.As(err, &pending) {
		c.JSON(http.StatusBadRequest, gin.H{"code": pending.Reason, "error": deviceMessage(pending.Reason)})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_grant", "error": "invalid device code"})
		return
	}
	c.JSON(http.StatusOK, tokensReply(access, refresh))
}

// POST /auth/device/approve {user_code}, clicked on the website.
func (s *server) approveDeviceLogin(c *gin.Context) {
	var in struct {
		UserCode string `json:"user_code" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "user_code required"})
		return
	}
	if err := auth.ApproveDeviceLogin(s.db, in.UserCode, c.GetString("user_id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_grant", "error": "that code is invalid or expired"})
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /auth/device/deny {user_code}, clicked on the website.
func (s *server) denyDeviceLogin(c *gin.Context) {
	var in struct {
		UserCode string `json:"user_code" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "user_code required"})
		return
	}
	if err := auth.DenyDeviceLogin(s.db, in.UserCode); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_grant", "error": "that code is invalid or expired"})
		return
	}
	c.Status(http.StatusNoContent)
}

func deviceMessage(reason string) string {
	switch reason {
	case "authorization_pending":
		return "waiting for you to approve the code in your browser"
	case "slow_down":
		return "polling too fast"
	case "access_denied":
		return "the request was denied"
	case "expired_token":
		return "the code expired"
	default:
		return "device authorization not ready"
	}
}
