package api

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"shellhaki/envi/internal/auth"
)

// DeviceHandler serves the device authorization grant. code and token are public
// (the CLI has no session yet); approve and deny require an authenticated web
// session so the approving user's identity is bound to the code.
type DeviceHandler struct {
	Service   auth.DeviceService
	WebURL    string
	AccessTTL time.Duration
}

func (h DeviceHandler) Routes(r *gin.Engine, m gin.HandlerFunc) {
	r.POST("/auth/device/code", h.start)
	r.POST("/auth/device/token", h.token)
	r.POST("/auth/device/approve", m, h.approve)
	r.POST("/auth/device/deny", m, h.deny)
}

func (h DeviceHandler) start(c *gin.Context) {
	deviceCode, userCode, expiresIn, interval, err := h.Service.Start()
	if err != nil {
		c.JSON(500, gin.H{"code": "server_error", "error": "unable to start device authorization"})
		return
	}
	verify := strings.TrimRight(h.WebURL, "/") + "/device"
	c.JSON(200, gin.H{
		"device_code":               deviceCode,
		"user_code":                 userCode,
		"verification_uri":          verify,
		"verification_uri_complete": verify + "?code=" + url.QueryEscape(userCode),
		"expires_in":                expiresIn,
		"interval":                  interval,
	})
}

func (h DeviceHandler) token(c *gin.Context) {
	var in struct {
		DeviceCode string `json:"device_code" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "device_code required"})
		return
	}
	access, refresh, err := h.Service.Redeem(in.DeviceCode)
	if err != nil {
		var pend auth.DevicePending
		if errors.As(err, &pend) {
			c.JSON(400, gin.H{"code": pend.Reason, "error": deviceReason(pend.Reason)})
			return
		}
		c.JSON(400, gin.H{"code": "invalid_grant", "error": "invalid device code"})
		return
	}
	c.JSON(200, gin.H{"access_token": access, "refresh_token": refresh, "expires_in": int(h.AccessTTL.Seconds())})
}

func (h DeviceHandler) approve(c *gin.Context) {
	var in struct {
		UserCode string `json:"user_code" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "user_code required"})
		return
	}
	if err := h.Service.Approve(in.UserCode, c.GetString("user_id")); err != nil {
		c.JSON(400, gin.H{"code": "invalid_grant", "error": "that code is invalid or expired"})
		return
	}
	c.Status(204)
}

func (h DeviceHandler) deny(c *gin.Context) {
	var in struct {
		UserCode string `json:"user_code" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "user_code required"})
		return
	}
	if err := h.Service.Deny(in.UserCode); err != nil {
		c.JSON(400, gin.H{"code": "invalid_grant", "error": "that code is invalid or expired"})
		return
	}
	c.Status(204)
}

// deviceReason gives each RFC 8628 poll code a human message for the "error"
// field; the CLI branches on the machine-readable "code" instead.
func deviceReason(code string) string {
	switch code {
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
