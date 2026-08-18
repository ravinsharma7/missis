package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: store-backup <destination>")
		os.Exit(1)
	}
	svc, err := application.Open("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	if err := client.BackupTo(context.Background(), os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
