package api

import (
	"github.com/gin-gonic/gin"
	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/secret"
)

type SecretHandler struct{ Service secret.Service }

func (h SecretHandler) Routes(r *gin.Engine, m gin.HandlerFunc) {
	g := r.Group("/environments/:id", m)
	g.GET("/secrets", h.get)
	g.GET("/secrets/snapshot", h.snapshot)
	g.PUT("/secrets", h.put)
	g.PUT("/secrets/snapshot", h.putSnapshot)
	g.DELETE("/secrets/:key", h.delete)
}
func (h SecretHandler) snapshot(c *gin.Context) {
	if c.GetString("service_id") != "" {
		if c.GetString("service_env") != c.Param("id") {
			c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
			return
		}
		values, e := h.Service.GetService(c, c.GetString("service_id"), c.Param("id"))
		revision, x := h.Service.Revision(c, c.Param("id"))
		if e != nil || x != nil {
			c.JSON(500, gin.H{"code": "internal", "error": "secrets unavailable"})
			return
		}
		c.JSON(200, secret.Snapshot{Values: values, Revision: revision})
		return
	}
	v, e := h.Service.Snapshot(c, c.GetString("user_id"), c.Param("id"))
	if e == access.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	c.JSON(200, v)
}
func (h SecretHandler) putSnapshot(c *gin.Context) {
	var in struct {
		Values           map[string]string `json:"values"`
		ExpectedRevision int64             `json:"expected_revision"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Values == nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "secret snapshot required"})
		return
	}
	var revision int64
	var e error
	if c.GetString("service_id") != "" {
		if c.GetString("service_env") != c.Param("id") || c.GetString("service_permission") == "read" {
			c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
			return
		}
		revision, e = h.Service.PutAllService(c, c.Param("id"), in.Values, in.ExpectedRevision)
	} else {
		revision, e = h.Service.PutAll(c, c.GetString("user_id"), c.Param("id"), in.Values, in.ExpectedRevision)
	}
	if e == secret.ErrConflict {
		c.JSON(409, gin.H{"code": "stale_revision", "error": "remote secrets changed; run envi diff or envi pull"})
		return
	}
	if e == access.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "secret write failed"})
		return
	}
	c.JSON(200, gin.H{"revision": revision})
}
func (h SecretHandler) delete(c *gin.Context) {
	e := h.Service.Delete(c, c.GetString("user_id"), c.Param("id"), c.Param("key"))
	if e == access.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if e != nil {
		c.JSON(404, gin.H{"code": "not_found", "error": "secret not found"})
		return
	}
	c.Status(204)
}
func (h SecretHandler) get(c *gin.Context) {
	if c.GetString("service_id") != "" {
		if c.GetString("service_env") != c.Param("id") {
			c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
			return
		}
		v, e := h.Service.GetService(c, c.GetString("service_id"), c.Param("id"))
		if e != nil {
			c.JSON(500, gin.H{"code": "internal", "error": "secrets unavailable"})
			return
		}
		c.JSON(200, v)
		return
	}
	v, e := h.Service.Get(c, c.GetString("user_id"), c.Param("id"))
	if e == access.ErrForbidden {
		c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	c.JSON(200, v)
}
func (h SecretHandler) put(c *gin.Context) {
	var in map[string]string
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "secret map required"})
		return
	}
	if c.GetString("service_id") != "" {
		if c.GetString("service_env") != c.Param("id") || c.GetString("service_permission") == "read" {
			c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
			return
		}
		for k, v := range in {
			if e := h.Service.PutService(c, c.Param("id"), k, v); e != nil {
				c.JSON(500, gin.H{"code": "internal", "error": "secret write failed"})
				return
			}
		}
		c.Status(204)
		return
	}
	for k, v := range in {
		if e := h.Service.Put(c, c.GetString("user_id"), c.Param("id"), k, v); e != nil {
			if e == access.ErrForbidden {
				c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
			} else {
				c.JSON(500, gin.H{"code": "internal", "error": "secret write failed"})
			}
			return
		}
	}
	c.Status(204)
}
