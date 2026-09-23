package api

// GET /values — one request that turns names into values.
//
// Not /config: that address is already taken by the public beta-mode flag the
// dashboard reads before anyone has signed in.
//
// Every other secrets address is keyed by environment UUID, which is fine for
// the CLI (it keeps one in envi.toml) and useless for an SDK, where nobody wants
// a UUID in their source. This resolves "project acme-api, environment
// production" and returns the values, in a single round trip, because on a
// serverless platform every extra call is added to a cold start.
//
// It accepts either credential:
//
//   - a service token, which already names one environment and may only ever
//     read that one;
//   - a user session, from a browser login or an exchanged API key, which names
//     the project and environment it wants.

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
)

func addConfigRoutes(router *gin.Engine, projects project.Service, secrets secret.Service, requireLogin gin.HandlerFunc) {
	router.GET("/values", requireLogin, func(c *gin.Context) { handleConfig(c, projects, secrets) })
}

func handleConfig(c *gin.Context, projects project.Service, secrets secret.Service) {
	wantProject := c.Query("project")
	wantEnvironment := c.Query("environment")

	if serviceID := c.GetString("service_id"); serviceID != "" {
		configForServiceToken(c, projects, secrets, serviceID, wantProject, wantEnvironment)
		return
	}
	configForUser(c, projects, secrets, c.GetString("user_id"), wantProject, wantEnvironment)
}

// A service token is bound to one environment, so there is nothing to resolve.
// Names in the query are treated as an assertion about which environment the
// caller believes it is reading, and a wrong one is refused rather than quietly
// served: a deploy pointed at the wrong environment should fail loudly.
func configForServiceToken(c *gin.Context, projects project.Service, secrets secret.Service, serviceID, wantProject, wantEnvironment string) {
	environmentID := c.GetString("service_env")

	environmentName, projectName, err := projects.DescribeEnvironment(c, environmentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "environment unavailable"})
		return
	}
	if (wantProject != "" && wantProject != projectName) || (wantEnvironment != "" && wantEnvironment != environmentName) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":  "forbidden",
			"error": "this token reads " + projectName + "/" + environmentName + " and nothing else",
		})
		return
	}

	values, err := secrets.GetService(c, serviceID, environmentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	revision, err := secrets.Revision(c, environmentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	c.JSON(http.StatusOK, configReply(projectName, environmentName, values, revision))
}

func configForUser(c *gin.Context, projects project.Service, secrets secret.Service, userID, wantProject, wantEnvironment string) {
	if wantProject == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "a project is required"})
		return
	}

	found, err := projects.FindByName(c, userID, wantProject)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "project_not_found", "error": err.Error()})
		return
	}
	environment, err := projects.FindEnvironmentByName(c, userID, found.ID, wantEnvironment)
	if err == project.ErrForbidden {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "environment_not_found", "error": err.Error()})
		return
	}

	// Snapshot runs the access check and writes the audit event, so reading
	// through here is exactly as restricted as reading anywhere else.
	snapshot, err := secrets.Snapshot(c, userID, environment.ID)
	if err == access.ErrForbidden {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	c.JSON(http.StatusOK, configReply(found.Name, environment.Name, snapshot.Values, snapshot.Revision))
}

func configReply(projectName, environmentName string, values map[string]string, revision int64) gin.H {
	if values == nil {
		values = map[string]string{}
	}
	return gin.H{
		"project":     projectName,
		"environment": environmentName,
		"values":      values,
		"revision":    revision,
	}
}
