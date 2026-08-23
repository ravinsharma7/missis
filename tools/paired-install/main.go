package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"

	"golang.org/x/mod/semver"

	"github.com/ravinsharma7/missis/internal/buildinfo"
	"github.com/ravinsharma7/missis/internal/update"
)

func main() {
	ref := flag.String("ref", "latest", "stable release tag or latest")
	binDir := flag.String("bin-dir", "", "destination directory for both binaries")
	manifestURL := flag.String("manifest-url", "", "release manifest URL (tests only)")
	flag.Parse()
	if *binDir == "" {
		fmt.Fprintln(os.Stderr, "paired-install: --bin-dir is required")
		os.Exit(2)
	}
	if *ref != "latest" && (!semver.IsValid(*ref) || semver.Prerelease(*ref) != "") {
		fmt.Fprintf(os.Stderr, "paired-install: ref %q is not a stable release tag\n", *ref)
		os.Exit(2)
	}
	url := *manifestURL
	if url == "" {
		if *ref == "latest" {
			url = update.DefaultManifestURL
		} else {
			url = "https://github.com/ravinsharma7/missis/releases/download/" + *ref + "/release-manifest.json"
		}
	}
	client := update.DefaultClient()
	client.ManifestURL = url
	client.GOOS = runtime.GOOS
	client.GOARCH = runtime.GOARCH
	manifest, err := client.Install(context.Background(), *ref, *binDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "paired-install: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("installed %s commit=%s store_format=%d in %s\n", buildinfo.ReleaseDisplay(manifest.Version, manifest.Commit), manifest.Commit, manifest.StoreFormatRevision, *binDir)
}
