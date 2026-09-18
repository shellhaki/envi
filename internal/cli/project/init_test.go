package project

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// multiEnvAPI has several projects *and* several environments, so init has to
// ask two questions in one run.
type multiEnvAPI struct{}

func (multiEnvAPI) Do(_ context.Context, _ string, _ string, _ any, out any) error {
	switch p := out.(type) {
	case *[]Project:
		*p = []Project{{"p1", "alpha"}, {"p2", "beta"}}
	case *[]Environment:
		*p = []Environment{{"e1", "development"}, {"e2", "production"}}
	}
	return nil
}

// A fresh bufio.Reader per prompt reads ahead and swallows the second answer,
// leaving the environment prompt at EOF. It stayed hidden while every project
// had exactly one environment and the second prompt was skipped.
func TestInitReadsBothPromptsFromOneStream(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	// Second project, second environment.
	if err := Init(context.Background(), multiEnvAPI{}, strings.NewReader("2\n2\n"), &out, dir, "", ""); err != nil {
		t.Fatalf("init failed across two prompts: %v\noutput:\n%s", err, out.String())
	}
	ctx, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Project.Name != "beta" || ctx.Environment.Name != "production" {
		t.Fatalf("selected %s/%s, want beta/production", ctx.Project.Name, ctx.Environment.Name)
	}
}
