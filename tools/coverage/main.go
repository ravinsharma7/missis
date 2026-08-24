package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type RegistryRequirement struct {
	ID       string   `json:"id"`
	Strength string   `json:"strength,omitempty"`
	Status   string   `json:"status,omitempty"`
	Tests    []string `json:"tests"`
}

type RegistryDecision struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Target   string `json:"target"`
}

type Registry struct {
	Requirements          []RegistryRequirement `json:"requirements"`
	Norms                 []RegistryRequirement `json:"norms"`
	Phase1ShouldDecisions []RegistryDecision    `json:"phase1_should_decisions"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "coverage: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("coverage", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	registryPath := flags.String("registry", "", "path to specs/requirements-registry.v3.json; enables two-way verification")
	if err := flags.Parse(args); err != nil {
		return err
	}

	lines, err := scanAll(".")
	if err != nil {
		return err
	}
	if *registryPath == "" {
		for _, line := range lines {
			fmt.Println(line)
		}
		return nil
	}
	return verifyRegistry(*registryPath, lines)
}

func scanAll(root string) ([]string, error) {
	var lines []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".missis-store", ".missis-backups", "temp", "vendor", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		displayPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fileLines, err := scanFile(path, filepath.ToSlash(displayPath))
		if err != nil {
			return err
		}
		lines = append(lines, fileLines...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(lines)
	return lines, nil
}

func scanFile(path, displayPath string) (out []string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	scanner := bufio.NewScanner(file)
	var (
		currentTest string
		currentLine int
		ids         []string
		lineNo      int
	)
	flush := func() {
		if currentTest != "" && len(ids) > 0 {
			out = append(out, fmt.Sprintf("%s:%d:%s:%s", displayPath, currentLine, currentTest, strings.Join(ids, " ")))
		}
		currentTest = ""
		currentLine = 0
		ids = nil
	}

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func Test") {
			flush()
			currentTest = testName(trimmed)
			currentLine = lineNo
			continue
		}
		if currentTest != "" && strings.Contains(line, "// covers") {
			for _, token := range strings.Fields(line) {
				token = strings.Trim(token, ",:")
				if strings.HasPrefix(token, "PH1-") || strings.HasPrefix(token, "N") {
					ids = append(ids, token)
				}
			}
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func testName(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	name := fields[1]
	if idx := strings.Index(name, "("); idx >= 0 {
		name = name[:idx]
	}
	return name
}

func verifyRegistry(path string, lines []string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var reg Registry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return fmt.Errorf("registry %s: %w", path, err)
	}

	known := map[string]bool{}
	norms := map[string]RegistryRequirement{}
	for _, r := range append(append([]RegistryRequirement{}, reg.Requirements...), reg.Norms...) {
		known[r.ID] = true
	}
	for _, n := range reg.Norms {
		norms[n.ID] = n
	}

	referenced := map[string]map[string]bool{}
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 4)
		if len(parts) != 4 {
			continue
		}
		test := parts[2]
		for _, id := range strings.Fields(parts[3]) {
			if referenced[id] == nil {
				referenced[id] = map[string]bool{}
			}
			referenced[id][test] = true
		}
	}

	var violations []string
	decisions := map[string]RegistryDecision{}
	for _, decision := range reg.Phase1ShouldDecisions {
		norm, ok := norms[decision.ID]
		if !ok {
			violations = append(violations, fmt.Sprintf("SHOULD decision references unknown norm %s", decision.ID))
			continue
		}
		if _, duplicate := decisions[decision.ID]; duplicate {
			violations = append(violations, fmt.Sprintf("duplicate SHOULD decision for %s", decision.ID))
			continue
		}
		decisions[decision.ID] = decision
		if decision.Reason == "" || decision.Target == "" {
			violations = append(violations, fmt.Sprintf("SHOULD decision %s requires reason and target", decision.ID))
		}
		switch decision.Decision {
		case "adopt":
			if norm.Status != "phase-1-should" && norm.Status != "phase-1" {
				violations = append(violations, fmt.Sprintf("adopted SHOULD %s has incompatible status %s", decision.ID, norm.Status))
			}
		case "defer":
			if !strings.HasPrefix(norm.Status, "deferred-") {
				violations = append(violations, fmt.Sprintf("deferred SHOULD %s has incompatible status %s", decision.ID, norm.Status))
			}
		case "reject":
			if norm.Status != "rejected" {
				violations = append(violations, fmt.Sprintf("rejected SHOULD %s has incompatible status %s", decision.ID, norm.Status))
			}
		default:
			violations = append(violations, fmt.Sprintf("SHOULD decision %s has invalid value %q", decision.ID, decision.Decision))
		}
	}
	for _, norm := range reg.Norms {
		if norm.Status == "phase-1-should" {
			decision, ok := decisions[norm.ID]
			if !ok || decision.Decision != "adopt" {
				violations = append(violations, fmt.Sprintf("Phase 1 SHOULD %s lacks an adopt decision", norm.ID))
			}
		}
	}
	var unknownIDs []string
	for id := range referenced {
		if !known[id] {
			unknownIDs = append(unknownIDs, id)
		}
	}
	sort.Strings(unknownIDs)
	for _, id := range unknownIDs {
		violations = append(violations, fmt.Sprintf("test references unknown requirement %s", id))
	}

	var untested []string
	for _, r := range reg.Requirements {
		if len(referenced[r.ID]) == 0 {
			untested = append(untested, r.ID)
		}
	}
	sort.Strings(untested)
	for _, id := range untested {
		violations = append(violations, fmt.Sprintf("requirement %s has no test reference", id))
	}

	var untestedNorms []string
	for _, n := range reg.Norms {
		if n.Status == "phase-1" && len(referenced[n.ID]) == 0 {
			untestedNorms = append(untestedNorms, n.ID)
		}
	}
	sort.Strings(untestedNorms)

	fmt.Printf("registry: %d requirements, %d norms\n", len(reg.Requirements), len(reg.Norms))
	fmt.Printf("coverage: %d test functions reference %d distinct ids\n", len(lines), len(referenced))
	if len(violations) > 0 {
		for _, v := range violations {
			fmt.Printf("FAIL: %s\n", v)
		}
		return fmt.Errorf("registry verification failed: %d violation(s)", len(violations))
	}
	if len(untestedNorms) > 0 {
		fmt.Printf("warning: phase-1 norms without test references: %s\n", strings.Join(untestedNorms, " "))
	}
	fmt.Println("registry verified: every requirement has a test and every test id exists")
	return nil
}
