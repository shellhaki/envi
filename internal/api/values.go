package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/project"
)

// GET /values?project=&environment= reads an environment by name instead of by ID.
// A service token always gets its own environment, and is refused if the names say otherwise.
func (s *server) getValues(c *gin.Context) {
	wantProject, wantEnvironment := c.Query("project"), c.Query("environment")
	if isServiceToken(c) {
		s.valuesForServiceToken(c, wantProject, wantEnvironment)
		return
	}
	s.valuesForUser(c, wantProject, wantEnvironment)
}

func (s *server) valuesForServiceToken(c *gin.Context, wantProject, wantEnvironment string) {
	envID := c.GetString("service_env")
	envName, projectName, err := s.projects.DescribeEnvironment(c, envID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "environment unavailable"})
		return
	}
	if (wantProject != "" && wantProject != projectName) || (wantEnvironment != "" && wantEnvironment != envName) {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "this token reads " + projectName + "/" + envName + " and nothing else"})
		return
	}
	values, err := s.secrets.GetService(c, c.GetString("service_id"), envID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	revision, err := s.secrets.Revision(c, envID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	c.JSON(http.StatusOK, valuesReply(projectName, envName, values, revision))
}

func (s *server) valuesForUser(c *gin.Context, wantProject, wantEnvironment string) {
	userID := c.GetString("user_id")
	if wantProject == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "a project is required"})
		return
	}
	found, err := s.projects.FindByName(c, userID, wantProject)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "project_not_found", "error": err.Error()})
		return
	}
	env, err := s.projects.FindEnvironmentByName(c, userID, found.ID, wantEnvironment)
	if err == project.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "environment_not_found", "error": err.Error()})
		return
	}
	snapshot, err := s.secrets.Snapshot(c, userID, env.ID)
	if err == access.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	c.JSON(http.StatusOK, valuesReply(found.Name, env.Name, snapshot.Values, snapshot.Revision))
}

func valuesReply(projectName, envName string, values map[string]string, revision int64) gin.H {
	if values == nil {
		values = map[string]string{}
	}
	return gin.H{"project": projectName, "environment": envName, "values": values, "revision": revision}
}
