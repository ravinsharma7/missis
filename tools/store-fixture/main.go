package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ravinsharma7/missis/internal/compatfixture"
)

func main() {
	output := flag.String("output", "", "new fixture directory")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "--output is required")
		os.Exit(2)
	}
	manifest, err := compatfixture.Build(context.Background(), *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("created store fixture revision=%d events=%d head=%s\n", manifest.StoreFormatRevision, manifest.EventCount, manifest.HeadHash)
}
