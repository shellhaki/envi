package api

// The web addresses for logging in the CLI through the browser. See
// internal/auth/device.go for how the whole flow works.
//
// /code and /token are called by the CLI, which isn't logged in yet, so they're
// open. /approve and /deny are clicked on the website, so they need a logged-in
// user: that's who the CLI ends up logged in as.

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/auth"
)

func addDeviceRoutes(router *gin.Engine, db *pgxpool.Pool, webURL string, requireLogin gin.HandlerFunc) {
	router.POST("/auth/device/code", func(c *gin.Context) { handleStartDeviceLogin(c, db, webURL) })
	router.POST("/auth/device/token", func(c *gin.Context) { handleFinishDeviceLogin(c, db) })
	// requireLogin runs first and stops the request if nobody is logged in.
	router.POST("/auth/device/approve", requireLogin, func(c *gin.Context) { handleApproveDeviceLogin(c, db) })
	router.POST("/auth/device/deny", requireLogin, func(c *gin.Context) { handleDenyDeviceLogin(c, db) })
}

// POST /auth/device/code   (called by the CLI)
func handleStartDeviceLogin(c *gin.Context, db *pgxpool.Pool, webURL string) {
	deviceCode, userCode, expiresIn, pollEvery, err := auth.StartDeviceLogin(db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "server_error", "error": "unable to start device authorization"})
		return
	}
	approvePage := strings.TrimRight(webURL, "/") + "/device"
	c.JSON(http.StatusOK, gin.H{
		"device_code":               deviceCode,
		"user_code":                 userCode,
		"verification_uri":          approvePage,
		"verification_uri_complete": approvePage + "?code=" + url.QueryEscape(userCode),
		"expires_in":                expiresIn,
		"interval":                  pollEvery,
	})
}

// POST /auth/device/token   {"device_code": "..."}   (the CLI asking "approved yet?")
func handleFinishDeviceLogin(c *gin.Context, db *pgxpool.Pool) {
	var request struct {
		DeviceCode string `json:"device_code" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "device_code required"})
		return
	}

	accessToken, refreshToken, err := auth.FinishDeviceLogin(db, request.DeviceCode)
	if err != nil {
		// errors.As checks whether err is a DevicePending, and if so copies it
		// into `pending` so we can read its Reason.
		var pending auth.DevicePending
		if errors.As(err, &pending) {
			c.JSON(http.StatusBadRequest, gin.H{"code": pending.Reason, "error": deviceReasonMessage(pending.Reason)})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_grant", "error": "invalid device code"})
		return
	}
	c.JSON(http.StatusOK, tokensReply(accessToken, refreshToken))
}

// POST /auth/device/approve   {"user_code": "WXYZ-ABCD"}   (clicked on the website)
func handleApproveDeviceLogin(c *gin.Context, db *pgxpool.Pool) {
	var request struct {
		UserCode string `json:"user_code" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "user_code required"})
		return
	}

	// requireLogin put the logged-in user's ID on the request.
	loggedInUser := c.GetString("user_id")
	if err := auth.ApproveDeviceLogin(db, request.UserCode, loggedInUser); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_grant", "error": "that code is invalid or expired"})
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /auth/device/deny   {"user_code": "WXYZ-ABCD"}   (clicked on the website)
func handleDenyDeviceLogin(c *gin.Context, db *pgxpool.Pool) {
	var request struct {
		UserCode string `json:"user_code" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "user_code required"})
		return
	}

	if err := auth.DenyDeviceLogin(db, request.UserCode); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_grant", "error": "that code is invalid or expired"})
		return
	}
	c.Status(http.StatusNoContent)
}

// deviceReasonMessage turns a DevicePending reason into words for a person.
// The CLI reads the "code" field instead.
func deviceReasonMessage(reason string) string {
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
