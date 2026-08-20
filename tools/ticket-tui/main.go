package main

import (
	"os"

	"github.com/ravinsharma7/missis/internal/tui"
)

func main() {
	os.Exit(tui.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
