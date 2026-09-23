package api

// The key-value store, addressed by project name so nothing has to know a UUID.
//
//	GET    /kv?project=acme-api          every value in the project
//	PUT    /kv   {project, key, value}   write one
//	DELETE /kv   {project, key}          remove one
//
// Both credentials work. A service token is bound to one environment, and the
// store is project-wide, so the token acts on the project that environment
// belongs to — its permission still decides whether it may write.

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/kv"
	"shellhaki/envi/internal/project"
)

func addKVRoutes(router *gin.Engine, projects project.Service, store kv.Store, requireLogin gin.HandlerFunc) {
	group := router.Group("/kv", requireLogin)
	group.GET("", func(c *gin.Context) { handleKVList(c, projects, store) })
	group.PUT("", func(c *gin.Context) { handleKVSet(c, projects, store) })
	group.DELETE("", func(c *gin.Context) { handleKVDelete(c, projects, store) })
}

// kvCaller works out which project this request may act on, and whether it may
// write to it. It answers once for both credential types so the handlers below
// don't each have to branch.
type kvCaller struct {
	projectID   string
	projectName string
	canWrite    bool
}

func resolveKVCaller(c *gin.Context, projects project.Service, wantProject string) (kvCaller, bool) {
	if serviceID := c.GetString("service_id"); serviceID != "" {
		id, name, err := projects.ProjectForEnvironment(c, c.GetString("service_env"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "project unavailable"})
			return kvCaller{}, false
		}
		// A named project that isn't the token's own is refused rather than
		// silently redirected, so a misconfigured deploy fails loudly.
		if wantProject != "" && wantProject != name {
			c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "this token belongs to " + name})
			return kvCaller{}, false
		}
		return kvCaller{projectID: id, projectName: name, canWrite: c.GetString("service_permission") != "read"}, true
	}

	userID := c.GetString("user_id")
	if wantProject == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "a project is required"})
		return kvCaller{}, false
	}
	found, err := projects.FindByName(c, userID, wantProject)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "project_not_found", "error": err.Error()})
		return kvCaller{}, false
	}
	if !projects.CanView(c, userID, found.ID) {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "access denied"})
		return kvCaller{}, false
	}
	return kvCaller{projectID: found.ID, projectName: found.Name, canWrite: projects.CanWrite(c, userID, found.ID)}, true
}

func handleKVList(c *gin.Context, projects project.Service, store kv.Store) {
	caller, ok := resolveKVCaller(c, projects, c.Query("project"))
	if !ok {
		return
	}
	values, err := store.All(c, caller.projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "values unavailable"})
		return
	}
	if values == nil {
		values = map[string]string{}
	}
	c.JSON(http.StatusOK, gin.H{"project": caller.projectName, "values": values})
}

func handleKVSet(c *gin.Context, projects project.Service, store kv.Store) {
	var request struct {
		Project string `json:"project"`
		Key     string `json:"key" binding:"required"`
		Value   string `json:"value"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "key required"})
		return
	}
	caller, ok := resolveKVCaller(c, projects, request.Project)
	if !ok {
		return
	}
	if !caller.canWrite {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "this credential may only read"})
		return
	}
	if err := store.Set(c, caller.projectID, request.Key, request.Value); err != nil {
		if err == kv.ErrForbidden {
			c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "access denied"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": caller.projectName, "key": request.Key})
}

func handleKVDelete(c *gin.Context, projects project.Service, store kv.Store) {
	var request struct {
		Project string `json:"project"`
		Key     string `json:"key" binding:"required"`
	}
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": "key required"})
		return
	}
	caller, ok := resolveKVCaller(c, projects, request.Project)
	if !ok {
		return
	}
	if !caller.canWrite {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "error": "this credential may only read"})
		return
	}
	if err := store.Delete(c, caller.projectID, request.Key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal", "error": "unable to delete"})
		return
	}
	c.Status(http.StatusNoContent)
}
