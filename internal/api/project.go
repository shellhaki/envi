package api

import (
	"shellhaki/envi/internal/project"

	"github.com/gin-gonic/gin"
)

type ProjectHandler struct{ Service project.Service }

func (h ProjectHandler) Routes(r *gin.Engine) {
	r.POST("/projects", h.create)
	r.GET("/projects", h.list)
	r.POST("/projects/:id/environments", h.createEnv)
	r.GET("/projects/:id/environments", h.listEnv)
	r.DELETE("/projects/:id", h.delete)
	r.PATCH("/environments/:id", h.updateEnv)
	r.DELETE("/environments/:id", h.deleteEnv)
}
func (h ProjectHandler) RoutesProtected(r *gin.Engine, m gin.HandlerFunc) {
	g := r.Group("/", m)
	g.POST("/projects", h.create)
	g.GET("/projects", h.list)
	g.POST("/projects/:id/environments", h.createEnv)
	g.GET("/projects/:id/environments", h.listEnv)
	g.DELETE("/projects/:id", h.delete)
	g.PATCH("/environments/:id", h.updateEnv)
	g.DELETE("/environments/:id", h.deleteEnv)
}
func (h ProjectHandler) user(c *gin.Context) string { return c.GetString("user_id") }
func (h ProjectHandler) create(c *gin.Context) {
	var in struct {
		OrgID string `json:"org_id" binding:"required"`
		Name  string `json:"name" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "org_id and name required"})
		return
	}
	p, e := h.Service.Create(c, h.user(c), in.OrgID, in.Name)
	if e == project.ErrForbidden {
		c.JSON(403, gin.H{"error": "forbidden"})
		return
	}
	if e != nil {
		c.JSON(409, gin.H{"error": "project could not be created"})
		return
	}
	c.JSON(201, p)
}
func (h ProjectHandler) list(c *gin.Context) {
	p, e := h.Service.List(c, h.user(c))
	if e != nil {
		c.JSON(500, gin.H{"error": "projects unavailable"})
		return
	}
	c.JSON(200, p)
}
func (h ProjectHandler) createEnv(c *gin.Context) {
	var in struct {
		Name       string `json:"name" binding:"required"`
		Production bool   `json:"is_production"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "name required"})
		return
	}
	e, err := h.Service.CreateEnvironment(c, h.user(c), c.Param("id"), in.Name, in.Production)
	if err == project.ErrForbidden {
		c.JSON(403, gin.H{"error": "forbidden"})
		return
	}
	if err != nil {
		c.JSON(409, gin.H{"error": "environment could not be created"})
		return
	}
	c.JSON(201, e)
}
func (h ProjectHandler) listEnv(c *gin.Context) {
	e, err := h.Service.ListEnvironments(c, h.user(c), c.Param("id"))
	if err == project.ErrForbidden {
		c.JSON(403, gin.H{"error": "forbidden"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "environments unavailable"})
		return
	}
	c.JSON(200, e)
}

// delete removes the project and, through ON DELETE CASCADE, every
// environment, secret, version, grant, invitation and service token under it.
// Only an owner or admin of the project's org may do it.
func (h ProjectHandler) delete(c *gin.Context) {
	err := h.Service.Delete(c, h.user(c), c.Param("id"))
	if err == project.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "only an owner or admin can delete this project"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "project could not be deleted"})
		return
	}
	c.Status(204)
}

// updateEnv renames an environment or flips its production flag. Production is
// not cosmetic: it stops org membership alone from granting access, so the
// caller must be an owner or admin.
func (h ProjectHandler) updateEnv(c *gin.Context) {
	var in struct {
		Name       string `json:"name" binding:"required"`
		Production bool   `json:"is_production"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "name required"})
		return
	}
	e, err := h.Service.UpdateEnvironment(c, h.user(c), c.Param("id"), in.Name, in.Production)
	if err == project.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "only an owner or admin can change this environment"})
		return
	}
	if err != nil {
		c.JSON(409, gin.H{"code": "conflict", "error": "environment could not be updated"})
		return
	}
	c.JSON(200, e)
}

// deleteEnv removes the environment and, by cascade, every secret and version
// inside it.
func (h ProjectHandler) deleteEnv(c *gin.Context) {
	err := h.Service.DeleteEnvironment(c, h.user(c), c.Param("id"))
	if err == project.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "only an owner or admin can delete this environment"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "environment could not be deleted"})
		return
	}
	c.Status(204)
}
