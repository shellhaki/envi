package api

import (
	"github.com/gin-gonic/gin"

	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/secret"
)

// Every secrets route works for a signed-in person or for a service token.
// A service token may only touch its own environment, and a read token may not write.

func isServiceToken(c *gin.Context) bool { return c.GetString("service_id") != "" }

func serviceTokenMayRead(c *gin.Context) bool {
	return c.GetString("service_env") == c.Param("id")
}

func serviceTokenMayWrite(c *gin.Context) bool {
	return serviceTokenMayRead(c) && c.GetString("service_permission") != "read"
}

func forbidden(c *gin.Context) {
	c.JSON(403, gin.H{"code": "forbidden", "error": "access denied"})
}

// GET /environments/:id/secrets
func (s *server) getSecrets(c *gin.Context) {
	var values map[string]string
	var err error
	if isServiceToken(c) {
		if !serviceTokenMayRead(c) {
			forbidden(c)
			return
		}
		values, err = s.secrets.GetService(c, c.GetString("service_id"), c.Param("id"))
	} else {
		values, err = s.secrets.Get(c, c.GetString("user_id"), c.Param("id"))
	}
	if err == access.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	c.JSON(200, values)
}

// GET /environments/:id/secrets/snapshot returns every value plus the environment's revision.
func (s *server) getSnapshot(c *gin.Context) {
	if isServiceToken(c) {
		if !serviceTokenMayRead(c) {
			forbidden(c)
			return
		}
		values, err := s.secrets.GetService(c, c.GetString("service_id"), c.Param("id"))
		revision, revErr := s.secrets.Revision(c, c.Param("id"))
		if err != nil || revErr != nil {
			c.JSON(500, gin.H{"code": "internal", "error": "secrets unavailable"})
			return
		}
		c.JSON(200, secret.Snapshot{Values: values, Revision: revision})
		return
	}
	snapshot, err := s.secrets.Snapshot(c, c.GetString("user_id"), c.Param("id"))
	if err == access.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "secrets unavailable"})
		return
	}
	c.JSON(200, snapshot)
}

// PUT /environments/:id/secrets {KEY: value, ...} sets the given keys and leaves the rest.
func (s *server) putSecrets(c *gin.Context) {
	var in map[string]string
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "secret map required"})
		return
	}
	if isServiceToken(c) && !serviceTokenMayWrite(c) {
		forbidden(c)
		return
	}
	for key, value := range in {
		var err error
		if isServiceToken(c) {
			err = s.secrets.PutService(c, c.Param("id"), key, value)
		} else {
			err = s.secrets.Put(c, c.GetString("user_id"), c.Param("id"), key, value)
		}
		if err == access.ErrForbidden {
			forbidden(c)
			return
		}
		if err != nil {
			c.JSON(500, gin.H{"code": "internal", "error": "secret write failed"})
			return
		}
	}
	c.Status(204)
}

// PUT /environments/:id/secrets/snapshot {values, expected_revision} replaces the whole environment.
// It answers 409 when someone else wrote since expected_revision.
func (s *server) putSnapshot(c *gin.Context) {
	var in struct {
		Values           map[string]string `json:"values"`
		ExpectedRevision int64             `json:"expected_revision"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Values == nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": "secret snapshot required"})
		return
	}
	var revision int64
	var err error
	if isServiceToken(c) {
		if !serviceTokenMayWrite(c) {
			forbidden(c)
			return
		}
		revision, err = s.secrets.PutAllService(c, c.Param("id"), in.Values, in.ExpectedRevision)
	} else {
		revision, err = s.secrets.PutAll(c, c.GetString("user_id"), c.Param("id"), in.Values, in.ExpectedRevision)
	}
	if err == secret.ErrConflict {
		c.JSON(409, gin.H{"code": "stale_revision", "error": "remote secrets changed; run envi diff or envi pull"})
		return
	}
	if err == access.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": "internal", "error": "secret write failed"})
		return
	}
	c.JSON(200, gin.H{"revision": revision})
}

// DELETE /environments/:id/secrets/:key
func (s *server) deleteSecret(c *gin.Context) {
	err := s.secrets.Delete(c, c.GetString("user_id"), c.Param("id"), c.Param("key"))
	if err == access.ErrForbidden {
		forbidden(c)
		return
	}
	if err != nil {
		c.JSON(404, gin.H{"code": "not_found", "error": "secret not found"})
		return
	}
	c.Status(204)
}
