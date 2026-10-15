// Command envi is the CLI people install: it logs in, pulls and pushes
// secrets, and runs programs with them. The commands live in internal/cli.
package main

import (
	"os"
	"shellhaki/envi/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Version: version}.Run(os.Args[1:]))
}
