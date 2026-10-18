package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/kv"
)

// kvTarget is the project a key-value request acts on, and whether the caller may write to it.
type kvTarget struct {
	projectID   string
	projectName string
	canWrite    bool
}

// resolveKVTarget finds the project for this request. On failure it has already replied.
// A service token acts on its environment's project; a person names the project.
func (s *server) resolveKVTarget(c *gin.Context, wantProject string) (kvTarget, bool) {
	if isServiceToken(c) {
		id, name, err := s.projects.ProjectForEnvironment(c, c.GetString("service_env"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "project unavailable"})
			return kvTarget{}, false
		}
		if wantProject != "" && wantProject != name {
			c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "this token belongs to " + name})
			return kvTarget{}, false
		}
		return kvTarget{id, name, c.GetString("service_permission") != "read"}, true
	}

	userID := c.GetString("user_id")
	if wantProject == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "a project is required"})
		return kvTarget{}, false
	}
	found, err := s.projects.FindByName(c, userID, wantProject)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "project_not_found", "error": err.Error()})
		return kvTarget{}, false
	}
	if !s.projects.CanView(c, userID, found.ID) {
		forbidden(c)
		return kvTarget{}, false
	}
	return kvTarget{found.ID, found.Name, s.projects.CanWrite(c, userID, found.ID)}, true
}

// GET /kv?project=
func (s *server) listKV(c *gin.Context) {
	target, ok := s.resolveKVTarget(c, c.Query("project"))
	if !ok {
		return
	}
	values, err := s.kv.All(c, target.projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "values unavailable"})
		return
	}
	if values == nil {
		values = map[string]string{}
	}
	c.JSON(http.StatusOK, gin.H{"project": target.projectName, "values": values})
}

// PUT /kv {project, key, value}
func (s *server) setKV(c *gin.Context) {
	var in struct {
		Project string `json:"project"`
		Key     string `json:"key" binding:"required"`
		Value   string `json:"value"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "key required"})
		return
	}
	target, ok := s.resolveKVTarget(c, in.Project)
	if !ok {
		return
	}
	if !target.canWrite {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "this credential may only read"})
		return
	}
	err := s.kv.Set(c, target.projectID, in.Key, in.Value)
	if err == kv.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": target.projectName, "key": in.Key})
}

// DELETE /kv {project, key}
func (s *server) deleteKV(c *gin.Context) {
	var in struct {
		Project string `json:"project"`
		Key     string `json:"key" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "key required"})
		return
	}
	target, ok := s.resolveKVTarget(c, in.Project)
	if !ok {
		return
	}
	if !target.canWrite {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "this credential may only read"})
		return
	}
	if err := s.kv.Delete(c, target.projectID, in.Key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "unable to delete"})
		return
	}
	c.Status(http.StatusNoContent)
}
