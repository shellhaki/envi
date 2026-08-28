package project

import (
	"errors"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"strings"
)

const Filename = "envi.toml"

type Context struct {
	Version     int      `toml:"version"`
	Project     Resource `toml:"project"`
	Environment Resource `toml:"environment"`
}
type Resource struct {
	ID       string `toml:"id"`
	Name     string `toml:"name"`
	Revision int64  `toml:"revision,omitempty"`
}

func (c Context) Validate() error {
	if c.Version != 1 {
		return errors.New("unsupported envi.toml version")
	}
	if strings.TrimSpace(c.Project.ID) == "" || strings.TrimSpace(c.Project.Name) == "" {
		return errors.New("envi.toml project metadata is incomplete")
	}
	if strings.TrimSpace(c.Environment.ID) == "" || strings.TrimSpace(c.Environment.Name) == "" {
		return errors.New("envi.toml environment metadata is incomplete")
	}
	return nil
}
func Write(d string, c Context) error {
	if e := c.Validate(); e != nil {
		return e
	}
	b, e := toml.Marshal(c)
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(d, Filename), b, 0644)
}
func Load(d string) (Context, error) {
	b, e := os.ReadFile(filepath.Join(d, Filename))
	if os.IsNotExist(e) {
		return Context{}, errors.New("not initialized; run envi init")
	}
	if e != nil {
		return Context{}, e
	}
	var c Context
	if e = toml.Unmarshal(b, &c); e != nil {
		return Context{}, errors.New("malformed envi.toml")
	}
	return c, c.Validate()
}
