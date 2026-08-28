package main

import (
	"os"
	"shellhaki/envi/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Version: version}.Run(os.Args[1:]))
}
