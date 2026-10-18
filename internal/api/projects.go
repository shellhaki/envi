package api

import (
	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/project"
)

// POST /projects {org_id, name}
func (s *server) createProject(c *gin.Context) {
	var in struct {
		OrgID string `json:"org_id" binding:"required"`
		Name  string `json:"name" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "org_id and name required"})
		return
	}
	p, err := s.projects.Create(c, c.GetString("user_id"), in.OrgID, in.Name)
	if err == project.ErrForbidden {
		c.JSON(403, gin.H{"error": "forbidden"})
		return
	}
	if err != nil {
		c.JSON(409, gin.H{"error": "project could not be created"})
		return
	}
	c.JSON(201, p)
}

// GET /projects
func (s *server) listProjects(c *gin.Context) {
	list, err := s.projects.List(c, c.GetString("user_id"))
	if err != nil {
		c.JSON(500, gin.H{"error": "projects unavailable"})
		return
	}
	c.JSON(200, list)
}

// DELETE /projects/:id removes the project and everything in it.
func (s *server) deleteProject(c *gin.Context) {
	err := s.projects.Delete(c, c.GetString("user_id"), c.Param("id"))
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

// POST /projects/:id/environments {name, is_production}
func (s *server) createEnvironment(c *gin.Context) {
	var in struct {
		Name       string `json:"name" binding:"required"`
		Production bool   `json:"is_production"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"error": "name required"})
		return
	}
	env, err := s.projects.CreateEnvironment(c, c.GetString("user_id"), c.Param("id"), in.Name, in.Production)
	if err == project.ErrForbidden {
		c.JSON(403, gin.H{"error": "forbidden"})
		return
	}
	if err != nil {
		c.JSON(409, gin.H{"error": "environment could not be created"})
		return
	}
	c.JSON(201, env)
}

// GET /projects/:id/environments
func (s *server) listEnvironments(c *gin.Context) {
	list, err := s.projects.ListEnvironments(c, c.GetString("user_id"), c.Param("id"))
	if err == project.ErrForbidden {
		c.JSON(403, gin.H{"error": "forbidden"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "environments unavailable"})
		return
	}
	c.JSON(200, list)
}

// PATCH /environments/:id {name, is_production}
func (s *server) updateEnvironment(c *gin.Context) {
	var in struct {
		Name       string `json:"name" binding:"required"`
		Production bool   `json:"is_production"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "name required"})
		return
	}
	env, err := s.projects.UpdateEnvironment(c, c.GetString("user_id"), c.Param("id"), in.Name, in.Production)
	if err == project.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "only an owner or admin can change this environment"})
		return
	}
	if err != nil {
		c.JSON(409, gin.H{"code": "conflict", "error": "environment could not be updated"})
		return
	}
	c.JSON(200, env)
}

// DELETE /environments/:id removes the environment and its secrets.
func (s *server) deleteEnvironment(c *gin.Context) {
	err := s.projects.DeleteEnvironment(c, c.GetString("user_id"), c.Param("id"))
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
