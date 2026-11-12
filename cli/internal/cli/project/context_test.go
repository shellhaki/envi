package project

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fakeAPI struct{}

func (fakeAPI) Do(_ context.Context, _ string, path string, _ any, out any) error {
	switch p := out.(type) {
	case *[]Project:
		*p = []Project{{"p1", "one"}, {"p2", "two"}}
	case *[]Environment:
		if path == "/projects/p2/environments" {
			*p = []Environment{{"e2", "production"}}
		} else {
			*p = []Environment{{"e1", "development"}}
		}
	}
	return nil
}
func TestInitAndLoad(t *testing.T) {
	d := t.TempDir()
	var out bytes.Buffer
	if e := Init(context.Background(), fakeAPI{}, bytes.NewBufferString("2\n"), &out, d, "", ""); e != nil {
		t.Fatal(e)
	}
	c, e := Load(d)
	if e != nil || c.Project.ID != "p2" || c.Environment.ID != "e2" {
		t.Fatal(c, e)
	}
}
func TestNamedProject(t *testing.T) {
	d := t.TempDir()
	if e := Init(context.Background(), fakeAPI{}, bytes.NewBuffer(nil), new(bytes.Buffer), d, "one", "development"); e != nil {
		t.Fatal(e)
	}
	c, _ := Load(d)
	if c.Project.Name != "one" {
		t.Fatal(c)
	}
}
func TestLoadErrors(t *testing.T) {
	d := t.TempDir()
	if _, e := Load(d); e == nil {
		t.Fatal("missing accepted")
	}
	if e := os.WriteFile(filepath.Join(d, Filename), []byte("[bad"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := Load(d); e == nil {
		t.Fatal("malformed accepted")
	}
}
