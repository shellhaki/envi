package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	projectctx "shellhaki/envi/internal/cli/project"
)

func Share(ctx context.Context, c Client, dir, email, project, env, permission string, out io.Writer) error {
	x, err := projectctx.Load(dir)
	if err != nil {
		return err
	}
	if project != "" && project != x.Project.Name {
		return errors.New("project does not match envi.toml")
	}
	if env != "" && env != x.Environment.Name {
		return errors.New("environment does not match envi.toml")
	}
	var invite struct{ Token string }
	err = c.Do(ctx, "POST", "/projects/"+x.Project.ID+"/invitations", map[string]any{"email": email, "environment_id": x.Environment.ID, "permission": permission}, &invite)
	if err != nil {
		return err
	}
	if invite.Token == "" {
		return errors.New("invalid invitation response")
	}
	fmt.Fprintln(out, invite.Token)
	return nil
}

func AcceptInvitation(ctx context.Context, c Client, token string) error {
	if token == "" {
		return errors.New("invitation token required")
	}
	return c.Do(ctx, "POST", "/invitations/accept", map[string]string{"token": token}, nil)
}
