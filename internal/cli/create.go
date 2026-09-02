package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	projectctx "shellhaki/envi/internal/cli/project"
)

// CreateProject creates a project in the caller's personal organization.
func CreateProject(ctx context.Context, c Client, name string, out io.Writer) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("project name required")
	}
	var me struct{ OrganizationID string }
	if e := c.Do(ctx, "GET", "/me", nil, &me); e != nil {
		return e
	}
	if me.OrganizationID == "" {
		return errors.New("no organization found for this account")
	}
	var p struct{ ID, Name string }
	if e := c.Do(ctx, "POST", "/projects", map[string]string{"org_id": me.OrganizationID, "name": name}, &p); e != nil {
		return e
	}
	fmt.Fprintf(out, "Created project %s (%s)\n", p.Name, p.ID)
	return nil
}

// CreateEnv adds an environment to a project. The project is taken from
// --project when given, otherwise from envi.toml in dir.
func CreateEnv(ctx context.Context, c Client, dir, projectName, envName string, production bool, out io.Writer) error {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return errors.New("environment name required")
	}
	id, label, e := resolveProject(ctx, c, dir, projectName)
	if e != nil {
		return e
	}
	var v struct{ ID, Name string }
	if e = c.Do(ctx, "POST", "/projects/"+id+"/environments", map[string]any{"name": envName, "is_production": production}, &v); e != nil {
		return e
	}
	tag := ""
	if production {
		tag = " [production]"
	}
	fmt.Fprintf(out, "Created environment %s (%s) in %s%s\n", v.Name, v.ID, label, tag)
	return nil
}

// resolveProject picks a project by explicit name, falling back to the
// project recorded in envi.toml when no name is given.
func resolveProject(ctx context.Context, c Client, dir, name string) (id, label string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		x, e := projectctx.Load(dir)
		if e != nil {
			return "", "", errors.New("no project given; pass --project or run inside an initialized directory")
		}
		return x.Project.ID, x.Project.Name, nil
	}
	var ps []struct{ ID, Name string }
	if e := c.Do(ctx, "GET", "/projects", nil, &ps); e != nil {
		return "", "", e
	}
	for _, p := range ps {
		if p.Name == name {
			return p.ID, p.Name, nil
		}
	}
	return "", "", fmt.Errorf("project %q not found", name)
}
