package project

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type API interface {
	Do(context.Context, string, string, any, any) error
}
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Environment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func Init(ctx context.Context, a API, in io.Reader, out io.Writer, dir, name, envName string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("working directory is required")
	}
	var ps []Project
	if e := a.Do(ctx, "GET", "/projects", nil, &ps); e != nil {
		return e
	}
	p, e := chooseProject(ps, name, in, out)
	if e != nil {
		return e
	}
	var es []Environment
	if e = a.Do(ctx, "GET", "/projects/"+p.ID+"/environments", nil, &es); e != nil {
		return e
	}
	v, e := chooseEnv(es, envName, in, out)
	if e != nil {
		return e
	}
	if e = Write(dir, Context{Version: 1, Project: Resource{ID: p.ID, Name: p.Name}, Environment: Resource{ID: v.ID, Name: v.Name}}); e != nil {
		return e
	}
	fmt.Fprintf(out, "Initialized %s (%s)\n", p.Name, v.Name)
	return nil
}
func chooseProject(v []Project, n string, in io.Reader, out io.Writer) (Project, error) {
	if n != "" {
		for _, p := range v {
			if p.Name == n {
				return p, nil
			}
		}
		return Project{}, fmt.Errorf("project %q not found", n)
	}
	if len(v) == 0 {
		return Project{}, errors.New("no projects available")
	}
	i, e := selectIndex("project", len(v), func(i int) string { return v[i].Name }, in, out)
	return v[i], e
}
func chooseEnv(v []Environment, n string, in io.Reader, out io.Writer) (Environment, error) {
	if n != "" {
		for _, x := range v {
			if x.Name == n {
				return x, nil
			}
		}
		return Environment{}, fmt.Errorf("environment %q not found", n)
	}
	if len(v) == 0 {
		return Environment{}, errors.New("project has no environments")
	}
	if len(v) == 1 {
		return v[0], nil
	}
	i, e := selectIndex("environment", len(v), func(i int) string { return v[i].Name }, in, out)
	return v[i], e
}
func selectIndex(l string, n int, name func(int) string, in io.Reader, out io.Writer) (int, error) {
	for i := 0; i < n; i++ {
		fmt.Fprintf(out, "%d) %s\n", i+1, name(i))
	}
	fmt.Fprintf(out, "Select %s: ", l)
	s, e := bufio.NewReader(in).ReadString('\n')
	if e != nil && !errors.Is(e, io.EOF) {
		return 0, e
	}
	i, e := strconv.Atoi(strings.TrimSpace(s))
	if e != nil || i < 1 || i > n {
		return 0, fmt.Errorf("invalid %s selection", l)
	}
	return i - 1, nil
}
