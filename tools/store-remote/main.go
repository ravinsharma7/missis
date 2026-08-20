package main

import (
	"os"

	"github.com/ravinsharma7/missis/internal/tooling"
)

func main() {
	os.Exit(tooling.RunRemote(os.Args[1:], os.Stdout, os.Stderr))
}
