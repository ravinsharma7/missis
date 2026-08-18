package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func main() {
	var svc *application.Service
	var err error
	if len(os.Args) > 1 {
		svc, err = application.OpenPath(os.Args[1])
	} else {
		svc, err = application.Open("")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	manifest, err := client.Manifest(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
