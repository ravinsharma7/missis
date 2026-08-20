package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func RunManifest(args []string, stdout, stderr io.Writer) int {
	stdout, stderr = commandWriters(stdout, stderr)
	var svc *application.Service
	var err error
	if len(args) > 0 {
		svc, err = application.OpenPath(args[0])
	} else {
		svc, err = application.Open("")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	client := missis.NewClient(svc)
	defer client.Close()
	manifest, err := client.Manifest(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
