package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/buildinfo"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/internal/update"
	missispkg "github.com/ravinsharma7/missis/pkg/missis"
)

type setupCheck struct {
	Status   string `json:"status"`
	Required bool   `json:"required"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
	Identity string `json:"identity,omitempty"`
}

type setupChecks struct {
	Installation setupCheck `json:"installation"`
	Marker       setupCheck `json:"marker"`
	Store        setupCheck `json:"store"`
	Health       setupCheck `json:"health"`
	Scope        setupCheck `json:"scope"`
	Legacy       setupCheck `json:"legacy_metadata"`
	AgentHandoff setupCheck `json:"agent_handoff"`
}

type setupResult struct {
	Status     string      `json:"status"`
	Mode       string      `json:"mode"`
	ProjectDir string      `json:"project_dir"`
	Actions    []string    `json:"actions"`
	Checks     setupChecks `json:"checks"`
}

func initialSetupResult(mode, project string) setupResult {
	unknownRequired := func(message string) setupCheck {
		return setupCheck{Status: "unknown", Required: true, Message: message}
	}
	return setupResult{
		Status: "failed", Mode: mode, ProjectDir: project, Actions: []string{},
		Checks: setupChecks{
			Installation: unknownRequired("installation has not been inspected"),
			Marker:       unknownRequired("project marker has not been inspected"),
			Store:        unknownRequired("store has not been inspected"),
			Health:       unknownRequired("store health has not been inspected"),
			Scope:        setupCheck{Status: "not_applicable", Required: false, Message: "no explicit project or group scope"},
			Legacy:       setupCheck{Status: "unknown", Required: false, Message: "legacy metadata has not been inspected"},
			AgentHandoff: setupCheck{Status: "unknown", Required: false, Message: "agent handoff has not been inspected"},
		},
	}
}

func samePath(a, b string) bool {
	aAbs, aErr := filepath.Abs(a)
	bAbs, bErr := filepath.Abs(b)
	if aErr != nil || bErr != nil {
		return false
	}
	aEval, aErr := filepath.EvalSymlinks(aAbs)
	if aErr == nil {
		aAbs = aEval
	}
	bEval, bErr := filepath.EvalSymlinks(bAbs)
	if bErr == nil {
		bAbs = bEval
	}
	return filepath.Clean(aAbs) == filepath.Clean(bAbs)
}

func inspectSetupInstallation(allowDevelopment bool) setupCheck {
	info := buildinfo.Read()
	binDir := ""
	identity := fmt.Sprintf("%s commit=%s store_format=%d %s/%s", info.DisplayVersion, info.Commit, info.StoreFormatRevision, runtime.GOOS, runtime.GOARCH)
	if buildinfo.IsStable(info) && info.Commit != buildinfo.UnknownCommit {
		var installation update.Installation
		var err error
		binDir, installation, err = update.VerifyCurrentInstallation(info)
		if err != nil {
			return setupCheck{Status: "failed", Required: true, Message: err.Error(), Identity: identity}
		}
		identity = fmt.Sprintf("%s commit=%s store_format=%d %s/%s", installation.Version, installation.Commit, installation.StoreFormatRevision, runtime.GOOS, runtime.GOARCH)
	} else {
		if !allowDevelopment {
			return setupCheck{Status: "not_confirmed", Required: true, Message: "development installation requires --allow-development", Identity: identity}
		}
		var err error
		binDir, err = update.VerifyDevelopmentPair(info)
		if err != nil {
			return setupCheck{Status: "failed", Required: true, Message: err.Error(), Identity: identity}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return setupCheck{Status: "failed", Required: true, Message: err.Error(), Identity: identity}
	}
	resolved, err := exec.LookPath("missis")
	if err != nil || !samePath(resolved, executable) {
		return setupCheck{Status: "failed", Required: true, Message: fmt.Sprintf("PATH does not resolve missis to the running binary in %s", binDir), Path: binDir, Identity: identity}
	}
	toolName := "missis-tools"
	if runtime.GOOS == "windows" {
		toolName += ".exe"
	}
	resolvedTool, err := exec.LookPath("missis-tools")
	if err != nil || !samePath(resolvedTool, filepath.Join(binDir, toolName)) {
		return setupCheck{Status: "failed", Required: true, Message: fmt.Sprintf("PATH does not resolve missis-tools to the verified companion in %s", binDir), Path: binDir, Identity: identity}
	}
	if !buildinfo.IsStable(info) || info.Commit == buildinfo.UnknownCommit {
		return setupCheck{Status: "not_confirmed", Required: true, Message: "development pair explicitly allowed; release installation is not confirmed", Path: binDir, Identity: identity}
	}
	return setupCheck{Status: "confirmed", Required: true, Message: "verified paired installation", Path: binDir, Identity: identity}
}

func resolveSetupStore(projectDir, storeFlag string) (string, string, bool, error) {
	markerPath := filepath.Join(projectDir, ".missis")
	info, err := os.Lstat(markerPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", markerPath, true, fmt.Errorf(".missis marker must not be a symbolic link")
		}
		raw := filepath.Join(".missis", "missis.db")
		if !info.IsDir() {
			data, readErr := os.ReadFile(markerPath)
			if readErr != nil {
				return "", markerPath, true, readErr
			}
			raw = strings.TrimSpace(string(data))
			if raw == "" || strings.ContainsRune(raw, '\n') || strings.ContainsRune(raw, 0) {
				return "", markerPath, true, fmt.Errorf(".missis marker must contain exactly one non-empty path line")
			}
		}
		if filepath.IsAbs(raw) {
			return "", markerPath, true, fmt.Errorf(".missis marker contains an absolute path")
		}
		target, pathErr := containedSetupPath(projectDir, filepath.Join(projectDir, raw))
		if pathErr != nil {
			return "", markerPath, true, fmt.Errorf(".missis marker escapes the project directory: %w", pathErr)
		}
		if storeFlag != "" {
			requested := filepath.Clean(filepath.Join(projectDir, storeFlag))
			if filepath.IsAbs(storeFlag) || !samePath(requested, target) {
				return "", markerPath, true, fmt.Errorf("--store conflicts with the existing .missis marker")
			}
		}
		return target, markerPath, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", markerPath, false, err
	}
	relStore := storeFlag
	if relStore == "" {
		relStore = filepath.Join(".missis-store", "missis.db")
	}
	if filepath.IsAbs(relStore) {
		return "", markerPath, false, fmt.Errorf("--store must be relative to the project directory")
	}
	target, err := containedSetupPath(projectDir, filepath.Join(projectDir, relStore))
	if err != nil {
		return "", markerPath, false, fmt.Errorf("--store escapes the project directory: %w", err)
	}
	return target, markerPath, false, nil
}

func containedSetupPath(projectDir, target string) (string, error) {
	candidate := filepath.Clean(target)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			rel, relErr := filepath.Rel(projectDir, resolved)
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("resolved path is outside %s", projectDir)
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", err
		}
		suffix = append(suffix, filepath.Base(candidate))
		candidate = parent
	}
}

func inspectOptionalSetupState(result *setupResult) {
	legacyPath := filepath.Join(result.ProjectDir, ".missis.d")
	if _, err := os.Stat(legacyPath); err == nil {
		result.Checks.Legacy = setupCheck{Status: "confirmed", Required: false, Message: "legacy metadata found and preserved", Path: legacyPath}
	} else if errors.Is(err, os.ErrNotExist) {
		result.Checks.Legacy = setupCheck{Status: "not_applicable", Required: false, Message: "legacy metadata not found"}
	} else {
		result.Checks.Legacy = setupCheck{Status: "unknown", Required: false, Message: err.Error(), Path: legacyPath}
	}
	handoffPath := filepath.Join(result.ProjectDir, "AGENTS.md")
	if data, err := os.ReadFile(handoffPath); err == nil && strings.Contains(string(data), "missis --ag-brief") {
		result.Checks.AgentHandoff = setupCheck{Status: "confirmed", Required: false, Message: "reviewed Missis handoff found", Path: handoffPath}
	} else if errors.Is(err, os.ErrNotExist) || err == nil {
		result.Checks.AgentHandoff = setupCheck{Status: "not_confirmed", Required: false, Message: "optional Missis handoff is not configured", Path: handoffPath}
	} else {
		result.Checks.AgentHandoff = setupCheck{Status: "unknown", Required: false, Message: err.Error(), Path: handoffPath}
	}
}

func checkScopeReadOnly(ctx context.Context, storePath string) error {
	project, group := os.Getenv("MISSIS_PROJECT"), os.Getenv("MISSIS_GROUP")
	if project == "" && group == "" {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(storePath)+"?mode=ro&immutable=1")
	if err != nil {
		return err
	}
	defer db.Close()
	for _, item := range []struct{ kind, id string }{{"project", project}, {"group", group}} {
		if item.id == "" {
			continue
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE stream_kind = ? AND stream_entity = ?`, item.kind, item.id).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("explicit %s scope does not exist: %s:%s", item.kind, item.kind, item.id)
		}
	}
	return nil
}

func writeMarkerAtomic(markerPath, projectDir, storePath string) error {
	rel, err := filepath.Rel(projectDir, storePath)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(projectDir, ".missis-setup-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := fmt.Fprintln(tmp, rel); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, markerPath)
}

func outputSetupResult(result setupResult, jsonMode bool) {
	if jsonMode {
		writeJSON(result)
		return
	}
	fmt.Printf("setup: %s\nproject: %s\n", result.Status, result.ProjectDir)
	for _, item := range []struct {
		name  string
		check setupCheck
	}{
		{"installation", result.Checks.Installation}, {"marker", result.Checks.Marker},
		{"store", result.Checks.Store}, {"health", result.Checks.Health},
		{"scope", result.Checks.Scope}, {"legacy_metadata", result.Checks.Legacy},
		{"agent_handoff", result.Checks.AgentHandoff},
	} {
		fmt.Printf("%s: %s - %s\n", item.name, item.check.Status, item.check.Message)
	}
}

func runSetup(args []string) int {
	fs := flag.NewFlagSet("--setup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	projectFlag, storeFlag := "", ""
	checkMode, allowDevelopment, jsonMode := false, false, false
	fs.StringVar(&projectFlag, "project", "", "existing project directory")
	fs.StringVar(&storeFlag, "store", "", "store path relative to the project directory")
	fs.BoolVar(&checkMode, "check", false, "inspect without changing project or store state")
	fs.BoolVar(&allowDevelopment, "allow-development", false, "allow an explicitly unverified development pair")
	fs.BoolVar(&jsonMode, "json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitSuccess
		}
		return exitInvalid
	}
	if projectFlag == "" || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "missis: --setup requires --project DIR")
		return exitInvalid
	}
	projectDir, err := filepath.Abs(projectFlag)
	if err != nil {
		printError(err, exitInvalid, jsonMode, nil)
		return exitInvalid
	}
	if resolved, resolveErr := filepath.EvalSymlinks(projectDir); resolveErr == nil {
		projectDir = resolved
	}
	result := initialSetupResult(map[bool]string{true: "check", false: "apply"}[checkMode], projectDir)
	info, err := os.Stat(projectDir)
	if err != nil || !info.IsDir() {
		result.Checks.Marker = setupCheck{Status: "failed", Required: true, Message: "project directory must already exist", Path: projectDir}
		outputSetupResult(result, jsonMode)
		return exitInvalid
	}
	if os.Getenv("MISSIS_STORE") != "" {
		result.Checks.Store = setupCheck{Status: "failed", Required: true, Message: "unset MISSIS_STORE before project setup"}
		outputSetupResult(result, jsonMode)
		return exitInvalid
	}
	result.Checks.Installation = inspectSetupInstallation(allowDevelopment)
	if result.Checks.Installation.Status == "failed" || (result.Checks.Installation.Status == "not_confirmed" && !allowDevelopment) {
		outputSetupResult(result, jsonMode)
		return exitStorage
	}
	storePath, markerPath, markerExists, err := resolveSetupStore(projectDir, storeFlag)
	if err != nil {
		result.Checks.Marker = setupCheck{Status: "failed", Required: true, Message: err.Error(), Path: markerPath}
		outputSetupResult(result, jsonMode)
		return exitStorage
	}
	if markerExists {
		result.Checks.Marker = setupCheck{Status: "confirmed", Required: true, Message: "existing marker preserved", Path: markerPath}
	} else if checkMode {
		result.Status = "not_ready"
		result.Checks.Marker = setupCheck{Status: "not_confirmed", Required: true, Message: "project is not initialized", Path: markerPath}
		result.Checks.Store = setupCheck{Status: "not_confirmed", Required: true, Message: "store is not reachable without a project marker", Path: storePath}
		result.Checks.Health = setupCheck{Status: "not_confirmed", Required: true, Message: "health was not checked"}
		inspectOptionalSetupState(&result)
		outputSetupResult(result, jsonMode)
		return exitStorage
	} else {
		result.Checks.Marker = setupCheck{Status: "not_confirmed", Required: true, Message: "marker will be created after store verification", Path: markerPath}
	}
	ctx := context.Background()
	if checkMode {
		inspection, inspectErr := store.InspectReadOnly(ctx, storePath)
		if inspectErr != nil {
			result.Checks.Store = setupCheck{Status: "failed", Required: true, Message: inspectErr.Error(), Path: storePath}
			result.Checks.Health = setupCheck{Status: "failed", Required: true, Message: "read-only health check failed"}
			inspectOptionalSetupState(&result)
			outputSetupResult(result, jsonMode)
			return exitStorage
		}
		result.Checks.Store = setupCheck{Status: "confirmed", Required: true, Message: fmt.Sprintf("store format revision %d", inspection.FormatRevision), Path: storePath}
		result.Checks.Health = setupCheck{Status: "confirmed", Required: true, Message: "read-only consistency and sequence checks passed"}
		if err := checkScopeReadOnly(ctx, storePath); err != nil {
			result.Checks.Scope = setupCheck{Status: "failed", Required: true, Message: err.Error()}
			inspectOptionalSetupState(&result)
			outputSetupResult(result, jsonMode)
			return exitStorage
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
			result.Checks.Store = setupCheck{Status: "failed", Required: true, Message: err.Error(), Path: storePath}
			outputSetupResult(result, jsonMode)
			return exitStorage
		}
		svc, openErr := application.OpenPath(storePath)
		if openErr != nil {
			result.Checks.Store = setupCheck{Status: "failed", Required: true, Message: openErr.Error(), Path: storePath}
			outputSetupResult(result, jsonMode)
			return exitStorage
		}
		client := missispkg.NewClient(svc)
		healthErr := client.CheckConsistency(ctx)
		gaps, gapsErr := client.SequenceGaps(ctx)
		if healthErr != nil || gapsErr != nil || len(gaps) != 0 {
			_ = client.Close()
			result.Checks.Health = setupCheck{Status: "failed", Required: true, Message: "store consistency or sequence check failed"}
			outputSetupResult(result, jsonMode)
			return exitStorage
		}
		if project := os.Getenv("MISSIS_PROJECT"); project != "" {
			if _, scopeErr := client.ShowEntity(ctx, "project:"+project, missispkg.ShowOptions{}); scopeErr != nil {
				_ = client.Close()
				result.Checks.Scope = setupCheck{Status: "failed", Required: true, Message: "explicit project scope does not exist: project:" + project}
				outputSetupResult(result, jsonMode)
				return exitStorage
			}
		}
		if group := os.Getenv("MISSIS_GROUP"); group != "" {
			if _, scopeErr := client.ShowEntity(ctx, "group:"+group, missispkg.ShowOptions{}); scopeErr != nil {
				_ = client.Close()
				result.Checks.Scope = setupCheck{Status: "failed", Required: true, Message: "explicit group scope does not exist: group:" + group}
				outputSetupResult(result, jsonMode)
				return exitStorage
			}
		}
		_ = client.Close()
		result.Checks.Store = setupCheck{Status: "confirmed", Required: true, Message: "store opened successfully", Path: storePath}
		result.Checks.Health = setupCheck{Status: "confirmed", Required: true, Message: "consistency and sequence checks passed"}
		if !markerExists {
			if err := writeMarkerAtomic(markerPath, projectDir, storePath); err != nil {
				result.Checks.Marker = setupCheck{Status: "failed", Required: true, Message: err.Error(), Path: markerPath}
				outputSetupResult(result, jsonMode)
				return exitStorage
			}
			result.Actions = append(result.Actions, "initialized_store", "created_marker")
			result.Checks.Marker = setupCheck{Status: "confirmed", Required: true, Message: "marker created atomically", Path: markerPath}
		}
	}
	if os.Getenv("MISSIS_PROJECT") != "" || os.Getenv("MISSIS_GROUP") != "" {
		result.Checks.Scope = setupCheck{Status: "confirmed", Required: true, Message: "explicit project/group scope exists"}
	}
	inspectOptionalSetupState(&result)
	if result.Checks.Installation.Status == "not_confirmed" {
		result.Status = "ready_development"
	} else {
		result.Status = "ready"
	}
	outputSetupResult(result, jsonMode)
	return exitSuccess
}
