package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

func selectVersion(tags []string, exact string) (string, error) {
	exact = strings.TrimSpace(exact)
	if exact == "" {
		return nextPatch(tags)
	}
	if !semver.IsValid(exact) || semver.Prerelease(exact) != "" {
		return "", fmt.Errorf("exact release %q is not a stable SemVer tag", exact)
	}
	for _, tag := range tags {
		if strings.TrimSpace(tag) == exact {
			return "", fmt.Errorf("exact release tag %q already exists", exact)
		}
	}
	latest := ""
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if semver.IsValid(tag) && semver.Prerelease(tag) == "" &&
			(latest == "" || semver.Compare(tag, latest) > 0) {
			latest = tag
		}
	}
	if latest != "" && semver.Compare(exact, latest) <= 0 {
		return "", fmt.Errorf("exact release %q must be newer than latest stable tag %q", exact, latest)
	}
	return exact, nil
}

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
	exact := flag.String("exact", "", "required exact stable release for operator-triggered publication")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: release-version [--exact vX.Y.Z]")
		os.Exit(2)
	}
	output, err := exec.Command("git", "tag", "--list", "v*").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	version, err := selectVersion(strings.Fields(string(output)), *exact)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(version)
}
