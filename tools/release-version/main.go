package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

func nextPatch(tags []string) (string, error) {
	valid := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if semver.IsValid(tag) && semver.Prerelease(tag) == "" {
			valid = append(valid, tag)
		}
	}
	if len(valid) == 0 {
		return "v0.0.1", nil
	}
	sort.Slice(valid, func(i, j int) bool { return semver.Compare(valid[i], valid[j]) < 0 })
	latest := valid[len(valid)-1]
	parts := strings.Split(strings.TrimPrefix(latest, "v"), ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("latest stable tag %q is not three-part SemVer", latest)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v%s.%s.%d", parts[0], parts[1], patch+1), nil
}

func main() {
	output, err := exec.Command("git", "tag", "--list", "v*").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	version, err := nextPatch(strings.Fields(string(output)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(version)
}
