package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	projectctx "shellhaki/envi/internal/cli/project"
)

func CreateServiceToken(ctx context.Context, c Client, dir, name, permission string, ttl int, out io.Writer) error {
	if name == "" {
		return errors.New("token name is required")
	}
	x, e := projectctx.Load(dir)
	if e != nil {
		return e
	}
	var t struct {
		Value     string `json:"Value"`
		ExpiresAt any    `json:"ExpiresAt"`
	}
	if e = c.Do(ctx, "POST", "/projects/"+x.Project.ID+"/environments/"+x.Environment.ID+"/service-tokens", map[string]any{"name": name, "permission": permission, "ttl_seconds": ttl}, &t); e != nil {
		return e
	}
	if t.Value == "" {
		return errors.New("invalid token response")
	}
	fmt.Fprintln(out, t.Value)
	return nil
}
