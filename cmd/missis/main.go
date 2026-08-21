package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	stdctx "context"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
	"github.com/ravinsharma7/missis/pkg/missis/render"
)

const (
	exitSuccess    = 0
	exitInvalid    = 2
	exitNotFound   = 3
	exitValidation = 4
	exitConflict   = 5
	exitStorage    = 8
)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func normalizeScopeInputs(values []string) []string {
	seen := make(map[string]struct{})
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				seen[item] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

type newResult struct {
	Ref        string  `json:"ref"`
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Status     string  `json:"status"`
	Project    *string `json:"project"`
	RecordedAt string  `json:"recorded_at"`
}

type showTicket struct {
	Ref        string              `json:"ref"`
	ID         string              `json:"id"`
	Title      string              `json:"title"`
	Status     string              `json:"status"`
	RecordedAt string              `json:"recorded_at"`
	Parts      map[string]showPart `json:"parts"`
}

type showPart struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Value     any    `json:"value"`
	ValueKind string `json:"value_kind"`
	ParentID  any    `json:"parent_id"`
	CreatedBy string `json:"created_by"`
}

type showEvent struct {
	ID          string `json:"id"`
	Alias       string `json:"alias"`
	Sequence    uint64 `json:"sequence"`
	Operation   string `json:"operation"`
	Target      string `json:"target"`
	Value       any    `json:"value"`
	RecordedAt  string `json:"recorded_at"`
	EffectiveAt string `json:"effective_at"`
	Actor       string `json:"actor"`
	Reason      string `json:"reason,omitempty"`
}

type errorResult struct {
	Error              string   `json:"error"`
	Target             *string  `json:"target"`
	Message            string   `json:"message"`
	Ontology           *string  `json:"ontology"`
	MissingObligations []string `json:"missing_obligations"`
}

const modulePath = "github.com/ravinsharma7/missis"

const unknownCommit = "unknown"

const agentBriefCommands = `missis new "Title" [--priority X] [--type T]... [--tag T]... [--from FILE|--stdin] [--json]
missis show [REF] [--json|--format markdown] [--search S] [--status S] [--type T] [--tag T]
missis set <REF> <VALUE> [--add] [--retract [--recursive] [--reason R]] [--json]`

const agentBriefRules = `- No destructive delete; use --retract --reason instead.
- If a ticket is requested without a title, derive it from the session focus in .missis.d/active.local.md (or active.example.md) and state the assumption; do not block on a question.
- Prefer missis refs (#N) over free text.
- Shells treat "#" as a comment: in commands, quote refs (missis show '#55') or use bare numbers (missis show 55); an unquoted #55 silently drops the ref and following flags.
- Use --json for machine-readable output.
- For the active project/group/focus, run: missis show --context`

const agentPointerSnippet = `## missis quick reference

Run ` + "`missis --ag-brief`" + ` once before ticket work. It prints the exact
new/show/set syntax and the rules from the CLI itself; do not copy that syntax
into this file. For the active session focus, run ` + "`missis show --context`" + `.

In shell commands, quote ticket refs (` + "`missis show '#55'`" + `) or use the
bare number (` + "`missis show 55`" + `); an unquoted ` + "`#55`" + ` is a shell
comment and silently runs the command without the ref or its flags.

When asked to create a ticket without a title, derive one from the active
focus and state the assumption; do not block on a clarifying question.`

func buildVersion() (string, string, string) {
	version := "dev"
	commit := unknownCommit
	note := ""
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version, commit, "no build metadata embedded in this binary"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if setting.Value != "" {
				commit = setting.Value
			}
		case "vcs.modified":
			if commit != unknownCommit && setting.Value == "true" {
				commit += "-dirty"
			}
		}
	}
	if commit == unknownCommit {
		note = commitUnknownNote(info.Main.Version)
	}
	return version, commit, note
}

// commitUnknownNote picks the most specific explanation the build metadata
// supports. A concrete module version (vX.Y.Z or a pseudo-version) with no
// VCS settings means the binary came from a module download, e.g.
// `go install module@version`: the proxy serves a zip with no git directory.
// "(devel)" (or an empty main version) means a local source build, where the
// missing hash is due to VCS stamping being disabled or absent from the
// source directory. Go's build info does not record which of those two
// sub-cases applies, so both are listed together.
func commitUnknownNote(mainVersion string) string {
	if mainVersion == "" || mainVersion == "(devel)" {
		return "built from a source tree without git metadata (VCS stamping disabled or the source directory is not a git checkout)"
	}
	return "built from a module download (e.g. 'go install module@version'), which embeds no git metadata"
}

// commitLabel renders the commit for human-facing output, adding a short
// explanation when the hash is unavailable so "unknown" is not presented
// without context.
func commitLabel(commit, note string) string {
	if commit == unknownCommit {
		return commit + " (" + note + ")"
	}
	return commit
}

// storePermissionWarnings reports observable permission problems on the store
// file so a redirected or shared store is visible instead of silent.
func storePermissionWarnings(path string) []string {
	// Private-by-default is POSIX-scoped; Windows relies on user-profile ACLs
	// and mode-bit warnings do not apply (ticket #55).
	if runtime.GOOS == "windows" {
		return nil
	}
	var warnings []string
	if fi, err := os.Stat(path); err == nil {
		if perm := fi.Mode().Perm(); perm&0o077 != 0 {
			warnings = append(warnings, fmt.Sprintf("store file permissions are %04o; private stores should be 0600", perm))
		}
	}
	if dir := filepath.Dir(path); dir != "." {
		if fi, err := os.Stat(dir); err == nil {
			if perm := fi.Mode().Perm(); perm&0o077 != 0 {
				warnings = append(warnings, fmt.Sprintf("store directory permissions are %04o; private stores should be 0700", perm))
			}
		}
	}
	return warnings
}

type moduleVersion struct {
	Version string `json:"Version"`
	Time    string `json:"Time"`
}

func latestModuleVersion() (moduleVersion, error) {
	var info moduleVersion
	cmd := exec.Command("go", "list", "-m", "-json", modulePath+"@latest")
	cmd.Env = append(os.Environ(), "GOPROXY=direct")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return info, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return info, err
	}
	return info, nil
}

func runSelfUpdateCheck(jsonMode bool) int {
	currentVersion, currentCommit, commitNote := buildVersion()
	latest, err := latestModuleVersion()
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	if jsonMode {
		writeJSON(selfUpdateCheckJSON(currentVersion, currentCommit, commitNote, latest))
		return exitSuccess
	}
	fmt.Printf("current version=%s commit=%s\n", currentVersion, commitLabel(currentCommit, commitNote))
	fmt.Printf("latest version=%s time=%s\n", latest.Version, latest.Time)
	return exitSuccess
}

func selfUpdateCheckJSON(currentVersion, currentCommit, commitNote string, latest moduleVersion) map[string]string {
	m := map[string]string{
		"current_version": currentVersion,
		"current_commit":  currentCommit,
		"latest_version":  latest.Version,
		"latest_time":     latest.Time,
	}
	if currentCommit == unknownCommit {
		m["current_commit_note"] = commitNote
	}
	return m
}

func runSelfUpdate(jsonMode bool) int {
	latest, err := latestModuleVersion()
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	cmd := exec.Command("go", "install", modulePath+"/cmd/missis@latest")
	cmd.Env = append(os.Environ(), "GOPROXY=direct")
	output, err := cmd.CombinedOutput()
	if err != nil {
		printError(fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output))), exitStorage, jsonMode, nil)
		return exitStorage
	}
	if jsonMode {
		writeJSON(map[string]string{
			"status":         "updated",
			"latest_version": latest.Version,
		})
		return exitSuccess
	}
	fmt.Printf("updated to %s\n", latest.Version)
	return exitSuccess
}

func printVersion(jsonMode bool) {
	version, commit, commitNote := buildVersion()
	if jsonMode {
		writeJSON(versionJSON(version, commit, commitNote))
		return
	}
	fmt.Printf("missis version=%s commit=%s\n", version, commitLabel(commit, commitNote))
}

func versionJSON(version, commit, commitNote string) map[string]string {
	m := map[string]string{
		"version": version,
		"commit":  commit,
	}
	if commit == unknownCommit {
		m["commit_note"] = commitNote
	}
	return m
}

func readActivePointer() (project, group, focus, ticket string) {
	project, group, focus, ticket = "none", "none", "", ""
	activePath := filepath.Join(".missis.d", "active.local.md")
	if _, err := os.Stat(activePath); err != nil {
		activePath = filepath.Join(".missis.d", "active.example.md")
	}
	if data, err := os.ReadFile(activePath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "project:") {
				project = strings.TrimSpace(strings.TrimPrefix(line, "project:"))
			}
			if strings.HasPrefix(line, "group:") {
				group = strings.TrimSpace(strings.TrimPrefix(line, "group:"))
			}
			if strings.HasPrefix(line, "focus:") {
				focus = strings.TrimSpace(strings.TrimPrefix(line, "focus:"))
			}
			if strings.HasPrefix(line, "ticket:") {
				ticket = strings.TrimSpace(strings.TrimPrefix(line, "ticket:"))
			}
		}
	}
	return project, group, focus, ticket
}

func outputContext(storePath string, jsonMode bool) {
	project, group, focus, _ := readActivePointer()
	if env := os.Getenv("MISSIS_PROJECT"); env != "" {
		project = env
	}
	if env := os.Getenv("MISSIS_GROUP"); env != "" {
		group = env
	}
	if jsonMode {
		writeJSON(map[string]string{
			"store":   storePath,
			"project": project,
			"group":   group,
			"focus":   focus,
		})
		return
	}
	fmt.Printf("store: %s\n", storePath)
	fmt.Printf("project: %s\n", project)
	fmt.Printf("group: %s\n", group)
	if focus != "" {
		fmt.Printf("focus: %s\n", focus)
	}
}

func runAgentBrief(args []string) int {
	storeFlag := ""
	jsonMode := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--store":
			if i+1 < len(args) {
				storeFlag = args[i+1]
				i++
			}
		case "--json":
			jsonMode = true
		}
	}
	storePath, err := missis.ResolveStorePath(storeFlag)
	if err != nil {
		printError(err, exitInvalid, jsonMode, nil)
		return exitInvalid
	}
	if jsonMode {
		writeJSON(map[string]any{
			"store":    storePath,
			"commands": strings.Split(agentBriefCommands, "\n"),
			"rules":    strings.Split(agentBriefRules, "\n"),
		})
		return exitSuccess
	}
	fmt.Printf("store: %s\n", storePath)
	fmt.Println("\ncommands:")
	for _, line := range strings.Split(agentBriefCommands, "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println("\nrules:")
	for _, line := range strings.Split(agentBriefRules, "\n") {
		fmt.Printf("  %s\n", line)
	}
	return exitSuccess
}

func runGetStarted() int {
	fmt.Print(getStartedText)
	return exitSuccess
}

const getStartedText = `missis getting started

URL-first setup for a new project:
     https://github.com/ravinsharma7/missis/blob/main/docs/agent-setup.md
     Prefer an immutable tag or commit in this URL for reproducibility.

1. Install both CLIs at the same ref (from a checkout, or pin a tag/commit):
     export MISSIS_REF=v0.2.0
     go install "github.com/ravinsharma7/missis/cmd/missis@$MISSIS_REF"
     go install "github.com/ravinsharma7/missis/tools/missis-tools@$MISSIS_REF"
     # local checkout alternative:
     # go install ./cmd/missis
     # go install ./tools/missis-tools
     export PATH="$(go env GOPATH)/bin:$PATH"

2. Initialize or verify the project:
     cd /path/to/your/project
     if [ -f .missis ]; then
       echo "Missis is already initialized; preserving existing state"
     else
       missis --init --json
     fi
     missis show --health
     missis show --context
     missis --ag-brief

3. First ticket and everyday workflow:
     missis new "First ticket" --json
     missis show 1 --format markdown
     missis set 1/status doing
     missis set '#1/notes' "some context"

4. Correct and remove (append-only; no destructive delete):
     missis set '#1/notes' "revised text"            # overwrites current value
     missis set '#1/notes' --retract --reason "moved elsewhere"

5. Backup, manifest, health, repair, and remote sync:
     MISSIS_STORE="$PWD/.missis-store/missis.db" \
       missis-tools backup "$PWD/backups/missis.db"
     missis-tools manifest
     missis-tools gaps .missis-store/missis.db
     missis-tools repair .missis-store/missis.db
     missis-tools remote upload
     missis-tools remote download "$PWD/backups/restored.db"

6. Optional: consume via the Go SDK:
     import "github.com/ravinsharma7/missis/pkg/missis"

For fresh-project, existing-project, PowerShell, and optional agent-integration
details, read docs/agent-setup.md. See README.md and spec section 14
(Projects, groups, and scopes) for the domain model.
`

func runPointer() int {
	fmt.Println(agentPointerSnippet)
	return exitSuccess
}

func runInstallSkill(args []string) int {
	from := ""
	dest := ""
	force := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--from":
			if i+1 < len(args) {
				from = args[i+1]
				i++
			}
		case "--dest":
			if i+1 < len(args) {
				dest = args[i+1]
				i++
			}
		case "--force":
			force = true
		}
	}
	src, err := resolveSkillSource(from)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInvalid
	}
	if dest == "" {
		dest = defaultSkillDest()
	}
	if _, statErr := os.Stat(filepath.Join(dest, "SKILL.md")); statErr == nil && !force {
		fmt.Fprintf(os.Stderr, "missis skill already installed at %s (use --force to overwrite)\n", dest)
		return exitInvalid
	}
	if err := copyDir(src, dest); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitStorage
	}
	fmt.Printf("installed missis skill to %s\n", dest)
	fmt.Println("the skill is available on the next agent turn")
	return exitSuccess
}

func resolveSkillSource(from string) (string, error) {
	if from != "" {
		abs, err := filepath.Abs(from)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(abs, "SKILL.md")); err != nil {
			return "", fmt.Errorf("skill source not found at %s", abs)
		}
		return abs, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "tools", "skills", "missis")
		if _, statErr := os.Stat(filepath.Join(candidate, "SKILL.md")); statErr == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("skill source not found; run from a missis checkout or pass --from <dir>")
}

func defaultSkillDest() string {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".codex", "skills", "missis")
		}
		home = filepath.Join(userHome, ".codex")
	}
	return filepath.Join(home, "skills", "missis")
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func runInit(args []string) int {
	storeFlag := ""
	jsonMode := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--store":
			if i+1 < len(args) {
				storeFlag = args[i+1]
				i++
			}
		case "--json":
			jsonMode = true
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	target := storeFlag
	if target == "" {
		target = filepath.Join(cwd, ".missis-store", "missis.db")
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	markerPath := filepath.Join(cwd, ".missis")
	if _, statErr := os.Stat(markerPath); statErr == nil {
		if jsonMode {
			writeJSON(map[string]string{"status": "already_initialized", "store_path": absTarget})
		} else {
			fmt.Printf("already initialized: %s\n", absTarget)
		}
		return exitSuccess
	}
	if err := os.MkdirAll(filepath.Dir(absTarget), 0o700); err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	rel, err := filepath.Rel(cwd, absTarget)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		printError(fmt.Errorf("external store %s cannot be initialized with a repo marker; use MISSIS_STORE or --store", absTarget), exitInvalid, jsonMode, nil)
		return exitInvalid
	}
	if err := os.WriteFile(markerPath, []byte(rel+"\n"), 0o644); err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".missis.d"), 0o755); err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	contextPath := filepath.Join(cwd, ".missis.d", "context.md")
	if _, statErr := os.Stat(contextPath); os.IsNotExist(statErr) {
		defaultContext := `# Current Context

This is a short-lived scratchpad for agents and collaborators. The authoritative
live work items are in the repo-local missis store.

Read this before starting implementation. Then run:

` + "```bash\nmissis show\n```\n" + `
For the current active project/group/ticket focus, read
` + "`active.local.md`" + ` when present, otherwise ` + "`active.example.md`" + `.

## Current local setup

` + "```text\n.missis -> ./.missis-store/missis.db\n.missis.d/ -> committed project metadata\n.missis-store/ -> ignored SQLite database\n```\n"
		if err := os.WriteFile(contextPath, []byte(defaultContext), 0o644); err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
	}
	activePath := filepath.Join(cwd, ".missis.d", "active.example.md")
	if _, statErr := os.Stat(activePath); os.IsNotExist(statErr) {
		defaultActive := `# Active Session

This is a short-lived agent pointer. It is not authoritative domain data.

` + "```text\nstore: .missis-store/missis.db\nproject: none\ngroup: none\nfocus: \nticket: \n```\n" + `
Rules:

- Prefer missis refs over free-text descriptions.
- Do not duplicate authoritative ticket content here.
- Update this file only when the active project, group, or ticket focus changes.
- ` + "`project:`" + ` and ` + "`group:`" + ` values are canonical IDs, not display titles.
`
		if err := os.WriteFile(activePath, []byte(defaultActive), 0o644); err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
	}
	svc, err := application.OpenPath(absTarget)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	client := missis.NewClient(svc)
	defer client.Close()
	if jsonMode {
		writeJSON(map[string]string{"status": "initialized", "store_path": absTarget})
	} else {
		fmt.Printf("initialized %s\n", absTarget)
	}
	return exitSuccess
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitInvalid)
	}
	switch os.Args[1] {
	case "--version":
		jsonMode := false
		for _, arg := range os.Args[2:] {
			if arg == "--json" {
				jsonMode = true
			}
		}
		printVersion(jsonMode)
		os.Exit(exitSuccess)
	case "--help":
		fmt.Print(usageText())
		os.Exit(exitSuccess)
	}
	if os.Args[1] == "--init" || os.Args[1] == "--start" {
		os.Exit(runInit(os.Args[2:]))
	}
	if os.Args[1] == "--ag-brief" {
		os.Exit(runAgentBrief(os.Args[2:]))
	}
	if os.Args[1] == "--get-started" {
		os.Exit(runGetStarted())
	}
	if os.Args[1] == "--ag-pointer" {
		os.Exit(runPointer())
	}
	if os.Args[1] == "--ag-install-skill" {
		os.Exit(runInstallSkill(os.Args[2:]))
	}
	if os.Args[1] == "--self-update-check" || os.Args[1] == "--self-update" {
		jsonMode := false
		for _, arg := range os.Args[2:] {
			if arg == "--json" {
				jsonMode = true
			}
		}
		if os.Args[1] == "--self-update-check" {
			os.Exit(runSelfUpdateCheck(jsonMode))
		}
		os.Exit(runSelfUpdate(jsonMode))
	}
	var code int
	switch os.Args[1] {
	case "new":
		code = runNew(os.Args[2:])
	case "show":
		code = runShow(os.Args[2:])
	case "set":
		code = runSet(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "missis: unknown command: %s\n", os.Args[1])
		usage()
		code = exitInvalid
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprint(os.Stderr, usageText())
}

func usageText() string {
	return "usage:\n" +
		"  missis [--version|--help] [--init|--start] [--self-update-check|--self-update] [--ag-brief [--json]] [--get-started] [--ag-pointer] [--ag-install-skill [--from DIR] [--dest DIR] [--force]]\n" +
		"  missis new|show|set ...\n"
}

func reorderArgs(args []string, valueFlags map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			flags = append(flags, args[i:]...)
			break
		}
		if strings.HasPrefix(arg, "--") {
			flags = append(flags, arg)
			name := strings.TrimPrefix(arg, "--")
			if valueFlags[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, arg)
	}
	return append(flags, positional...)
}

func runNew(args []string) int {
	args = reorderArgs(args, map[string]bool{
		"actor": true, "effective-at": true, "project": true, "priority": true,
		"type": true, "tag": true, "idempotency-key": true, "store": true,
		"from": true, "kind": true, "id": true,
	})
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		jsonMode    bool
		actor       string
		effectiveAt string
		project     string
		priority    string
		types       stringList
		tags        stringList
		idemKey     string
		storeFlag   string
		fromFile    string
		stdin       bool
		kind        string
		id          string
	)
	fs.BoolVar(&jsonMode, "json", false, "JSON output")
	fs.StringVar(&actor, "actor", "human/local", "actor reference")
	fs.StringVar(&effectiveAt, "effective-at", "", "effective timestamp")
	fs.StringVar(&project, "project", "", "project name")
	fs.StringVar(&priority, "priority", "", "priority value")
	fs.Var(&types, "type", "ticket type")
	fs.Var(&tags, "tag", "ticket tag")
	fs.StringVar(&idemKey, "idempotency-key", "", "idempotency key")
	fs.StringVar(&storeFlag, "store", "", "store path")
	fs.StringVar(&fromFile, "from", "", "import Markdown from file")
	fs.BoolVar(&stdin, "stdin", false, "import Markdown from stdin")
	fs.StringVar(&kind, "kind", "", "entity kind: project or group")
	fs.StringVar(&id, "id", "", "entity ID")
	if err := fs.Parse(args); err != nil {
		return exitInvalid
	}

	title := fs.Arg(0)
	if kind == "" && fromFile == "" && !stdin && strings.TrimSpace(title) == "" {
		printError(fmt.Errorf("title is required for missis new"), exitInvalid, jsonMode, nil)
		return exitInvalid
	}
	svc, err := application.Open(storeFlag)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	client := missis.NewClient(svc)
	defer client.Close()
	effectiveTime := time.Now().UTC()
	if effectiveAt != "" {
		effectiveTime, err = parseTime(effectiveAt)
		if err != nil {
			printError(err, exitInvalid, jsonMode, nil)
			return exitInvalid
		}
	}
	ctx := stdctx.Background()
	req := missis.RequestContext{Actor: actor, EffectiveAt: effectiveTime, IdempotencyKey: idemKey}

	if kind != "" {
		if kind != "project" && kind != "group" {
			printError(fmt.Errorf("invalid kind: %s", kind), exitInvalid, jsonMode, nil)
			return exitInvalid
		}
		if id == "" {
			printError(fmt.Errorf("--id is required for project or group"), exitInvalid, jsonMode, nil)
			return exitInvalid
		}
		result, err := client.NewEntity(ctx, req, missis.EntityOptions{Kind: kind, ID: id, Title: title})
		if err != nil {
			printError(err, mapError(err), jsonMode, nil)
			return mapError(err)
		}
		if jsonMode {
			writeJSON(result)
		} else {
			fmt.Printf("%s:%s  %s\n", kind, id, title)
		}
		return exitSuccess
	}

	if fromFile != "" || stdin {
		content, artifact, err := readImportSource(fromFile, stdin)
		if err != nil {
			printError(err, exitInvalid, jsonMode, nil)
			return exitInvalid
		}
		result, err := client.ImportMarkdown(ctx, req, missis.ImportOptions{Title: title, Content: content, Artifact: artifact, Project: project})
		if err != nil {
			printError(err, mapError(err), jsonMode, nil)
			return mapError(err)
		}
		writeNewResult(jsonMode, newResultFromSDK(result))
		return exitSuccess
	}

	result, err := client.NewTicket(ctx, req, missis.NewTicketOptions{
		Title:    title,
		Project:  project,
		Priority: priority,
		Types:    []string(types),
		Tags:     []string(tags),
	})
	if err != nil {
		printError(err, mapError(err), jsonMode, nil)
		return mapError(err)
	}
	writeNewResult(jsonMode, newResultFromSDK(result))
	return exitSuccess
}

func runShow(args []string) int {
	args = reorderArgs(args, map[string]bool{
		"at": true, "effective-at": true, "known-at": true,
		"since": true, "between": true, "store": true,
		"direction": true, "depth": true, "relations": true, "format": true,
		"project": true, "group": true,
		"search": true, "status": true, "type": true, "tag": true,
		"kind": true,
	})
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		jsonMode    bool
		at          string
		effectiveAt string
		knownAt     string
		history     bool
		since       string
		between     string
		storeFlag   string
		health      bool
		references  bool
		lineage     bool
		direction   string
		depth       int
		relations   stringList
		format      string
		project     stringList
		group       stringList
		unscoped    bool
		search      string
		status      stringList
		typeFilter  stringList
		tagFilter   stringList
		version     bool
		context     bool
		kind        string
	)
	fs.BoolVar(&jsonMode, "json", false, "JSON output")
	fs.StringVar(&at, "at", "", "set both effective and known time")
	fs.StringVar(&effectiveAt, "effective-at", "", "effective timestamp")
	fs.StringVar(&knownAt, "known-at", "", "known timestamp")
	fs.BoolVar(&history, "history", false, "show event history")
	fs.StringVar(&since, "since", "", "history lower bound")
	fs.StringVar(&between, "between", "", "history interval")
	fs.StringVar(&storeFlag, "store", "", "store path")
	fs.BoolVar(&health, "health", false, "run store consistency check")
	fs.BoolVar(&references, "references", false, "show incoming and outgoing links")
	fs.BoolVar(&lineage, "lineage", false, "traverse typed links")
	fs.StringVar(&direction, "direction", "both", "lineage direction: both, outgoing, or incoming")
	fs.IntVar(&depth, "depth", 3, "maximum lineage depth")
	fs.Var(&relations, "relations", "comma-separated relation allow-list")
	fs.StringVar(&format, "format", "", "output format: text, json, or markdown")
	fs.Var(&project, "project", "show project scope")
	fs.Var(&group, "group", "show group scope")
	fs.BoolVar(&unscoped, "unscoped", false, "show tickets with no project or group scope")
	fs.StringVar(&search, "search", "", "search query")
	fs.Var(&status, "status", "filter by status")
	fs.Var(&typeFilter, "type", "filter by type")
	fs.Var(&tagFilter, "tag", "filter by tag")
	fs.BoolVar(&version, "version", false, "show version")
	fs.BoolVar(&context, "context", false, "show active project/group context")
	fs.StringVar(&kind, "kind", "", "entity kind: project or group")
	if err := fs.Parse(args); err != nil {
		return exitInvalid
	}
	if format == "json" {
		jsonMode = true
	}
	if version {
		printVersion(jsonMode)
		return exitSuccess
	}

	ref := fs.Arg(0)
	storePath, err := missis.ResolveStorePath(storeFlag)
	if err != nil {
		printError(err, exitInvalid, jsonMode, nil)
		return exitInvalid
	}
	if context {
		outputContext(storePath, jsonMode)
		return exitSuccess
	}
	svc, err := application.Open(storeFlag)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := stdctx.Background()

	if health {
		if err := client.CheckConsistency(ctx); err != nil {
			if jsonMode {
				writeJSON(errorResult{Error: "storage_failure", Target: nil, Message: err.Error(), Ontology: nil, MissingObligations: []string{}})
			} else {
				fmt.Fprintf(os.Stderr, "missis: consistency failure: %s\n", err.Error())
			}
			return exitStorage
		}
		gaps, err := client.SequenceGaps(ctx)
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		if len(gaps) > 0 {
			var summary []string
			for _, gap := range gaps {
				summary = append(summary, fmt.Sprintf("%s:%s missing %v", gap.StreamKind, gap.StreamEntity, gap.Missing))
			}
			msg := "sequence gaps detected: " + strings.Join(summary, "; ") + "; accepted events are immutable, restore from a backup or create a new store"
			if jsonMode {
				writeJSON(errorResult{Error: "integrity_incident", Target: nil, Message: msg, Ontology: nil, MissingObligations: []string{}})
			} else {
				fmt.Fprintf(os.Stderr, "missis: %s\n", msg)
			}
			return exitStorage
		}
		storePath := client.StorePath()
		source := string(client.DiscoverySource())
		warnings := storePermissionWarnings(storePath)
		storeID, _ := client.StoreID()
		headHash, _ := client.HeadHash()
		eventCount, _ := client.EventCount()
		version, commit, _ := buildVersion()
		if jsonMode {
			writeJSON(map[string]any{
				"status":           "ok",
				"store_id":         storeID,
				"head_hash":        headHash,
				"event_count":      eventCount,
				"version":          version,
				"commit":           commit,
				"store_path":       storePath,
				"discovery_source": source,
				"warnings":         warnings,
			})
		} else {
			fmt.Printf("ok store=%s head=%s events=%d version=%s commit=%s\n", storeID, headHash, eventCount, version, commit)
			for _, warning := range warnings {
				fmt.Printf("warning: %s\n", warning)
			}
		}
		return exitSuccess
	}

	now := time.Now().UTC()
	effectiveTime, knownTime := now, now
	if at != "" {
		parsed, err := parseTime(at)
		if err != nil {
			printError(err, exitInvalid, jsonMode, nil)
			return exitInvalid
		}
		effectiveTime, knownTime = parsed, parsed
	}
	if effectiveAt != "" {
		effectiveTime, err = parseTime(effectiveAt)
		if err != nil {
			printError(err, exitInvalid, jsonMode, nil)
			return exitInvalid
		}
	}
	if knownAt != "" {
		knownTime, err = parseTime(knownAt)
		if err != nil {
			printError(err, exitInvalid, jsonMode, nil)
			return exitInvalid
		}
	}

	if kind != "" {
		if kind != "project" && kind != "group" {
			printError(fmt.Errorf("invalid kind: %s; expected project or group", kind), exitInvalid, jsonMode, nil)
			return exitInvalid
		}
		entities, err := client.ListEntities(ctx, model.Kind(kind), missis.ListFilter{
			Status:      strings.Join(status, ","),
			Query:       search,
			EffectiveAt: effectiveTime,
			KnownAt:     knownTime,
		})
		if err != nil {
			printError(err, mapError(err), jsonMode, nil)
			return mapError(err)
		}
		outputEntityList(entities, jsonMode)
		return exitSuccess
	}

	projectExplicit := len(project) > 0
	groupExplicit := len(group) > 0
	filterProjects := normalizeScopeInputs(project)
	filterGroups := normalizeScopeInputs(group)
	if unscoped && (len(filterProjects) > 0 || len(filterGroups) > 0) {
		printError(fmt.Errorf("--unscoped cannot be combined with --project or --group"), exitInvalid, jsonMode, nil)
		return exitInvalid
	}
	if !unscoped && !projectExplicit {
		if env := os.Getenv("MISSIS_PROJECT"); env != "" {
			filterProjects = normalizeScopeInputs([]string{env})
		}
	}
	if !unscoped && !groupExplicit {
		if env := os.Getenv("MISSIS_GROUP"); env != "" {
			filterGroups = normalizeScopeInputs([]string{env})
		}
	}
	if unscoped || len(filterProjects) > 0 || len(filterGroups) > 0 || search != "" || len(status) > 0 || len(typeFilter) > 0 || len(tagFilter) > 0 {
		filtered, err := client.ListTicketsFiltered(ctx, missis.ListFilter{
			Projects:    filterProjects,
			Groups:      filterGroups,
			Unscoped:    unscoped,
			Status:      strings.Join(status, ","),
			Type:        strings.Join(typeFilter, ","),
			Tag:         strings.Join(tagFilter, ","),
			Query:       search,
			EffectiveAt: effectiveTime,
			KnownAt:     knownTime,
		})
		if err != nil {
			printError(err, mapError(err), jsonMode, nil)
			return mapError(err)
		}
		outputTicketList(filtered, jsonMode)
		return exitSuccess
	}

	if ref == "" {
		summaries, err := client.ListTicketSummaries(ctx, effectiveTime)
		if err != nil {
			printError(err, mapError(err), jsonMode, nil)
			return mapError(err)
		}
		outputTicketList(summaries, jsonMode)
		return exitSuccess
	}

	if strings.HasPrefix(ref, "@") {
		event, err := client.ShowEvent(ctx, ref)
		if err != nil {
			printError(err, mapError(err), jsonMode, &ref)
			return mapError(err)
		}
		outputEventView(event, jsonMode)
		return exitSuccess
	}

	partPath := showRefPath(ref)

	if history {
		sinceTime := time.Time{}
		if since != "" {
			if parsed, parseErr := parseTime(since); parseErr == nil {
				sinceTime = parsed
			}
		}
		events, err := client.ShowHistory(ctx, ref, missis.HistoryOptions{EffectiveAt: effectiveTime, KnownAt: knownTime, Since: sinceTime, PartPath: partPath})
		if err != nil {
			printError(err, mapError(err), jsonMode, &ref)
			return mapError(err)
		}
		outputHistoryViews(events, jsonMode)
		return exitSuccess
	}

	if lineage {
		start, err := client.ResolveAnyRef(ctx, ref, effectiveTime)
		if err != nil {
			printError(err, mapError(err), jsonMode, &ref)
			return mapError(err)
		}
		var relationList []string
		for _, relation := range strings.Split(strings.Join(relations, ","), ",") {
			relation = strings.TrimSpace(relation)
			if relation != "" {
				relationList = append(relationList, relation)
			}
		}
		edges, err := client.ShowLineage(ctx, ref, missis.LineageOptions{Direction: direction, Depth: depth, Relations: relationList, EffectiveAt: effectiveTime, KnownAt: knownTime})
		if err != nil {
			printError(err, mapError(err), jsonMode, &ref)
			return mapError(err)
		}
		outputLineage(edges, start, jsonMode)
		return exitSuccess
	}

	if references {
		links, err := client.ShowReferences(ctx, ref, missis.ShowOptions{EffectiveAt: effectiveTime, KnownAt: knownTime})
		if err != nil {
			printError(err, mapError(err), jsonMode, &ref)
			return mapError(err)
		}
		outputReferences(links, jsonMode)
		return exitSuccess
	}

	var proj missis.TicketProjection
	if strings.HasPrefix(ref, "project:") || strings.HasPrefix(ref, "group:") {
		proj, err = client.ShowEntity(ctx, ref, missis.ShowOptions{EffectiveAt: effectiveTime, KnownAt: knownTime})
	} else {
		proj, err = client.ShowTicket(ctx, ref, missis.ShowOptions{EffectiveAt: effectiveTime, KnownAt: knownTime})
	}
	if err != nil {
		printError(err, mapError(err), jsonMode, &ref)
		return mapError(err)
	}
	if len(partPath) > 0 {
		pathKey := strings.Join(partPath, "/")
		if _, ok := proj.Parts[pathKey]; !ok {
			printError(fmt.Errorf("part path not found: %s", pathKey), exitNotFound, jsonMode, &ref)
			return exitNotFound
		}
	}
	if format == "markdown" {
		links, err := client.ShowReferences(ctx, ref, missis.ShowOptions{EffectiveAt: effectiveTime, KnownAt: knownTime})
		if err != nil {
			printError(err, mapError(err), jsonMode, &ref)
			return mapError(err)
		}
		outputMarkdownProjectionSDK(proj, partPath, links)
		return exitSuccess
	}
	outputProjectionSDK(proj, partPath, jsonMode)
	return exitSuccess
}

func runSet(args []string) int {
	args = reorderArgs(args, map[string]bool{
		"actor": true, "effective-at": true, "reason": true, "name": true,
		"parent": true, "supersedes": true, "because": true,
		"if-current": true, "idempotency-key": true, "store": true, "kind": true,
		"assertion": true, "allow-duplicate": true,
		"from": true,
	})
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		jsonMode       bool
		actor          string
		effectiveAt    string
		retract        bool
		recursive      bool
		reason         string
		add            bool
		name           string
		parent         string
		supersedes     string
		because        string
		ifCurrent      string
		idemKey        string
		storeFlag      string
		fromFile       string
		stdin          bool
		kind           string
		assertion      string
		allowDuplicate bool
	)
	fs.BoolVar(&jsonMode, "json", false, "JSON output")
	fs.StringVar(&actor, "actor", "human/local", "actor reference")
	fs.StringVar(&effectiveAt, "effective-at", "", "effective timestamp")
	fs.BoolVar(&retract, "retract", false, "retract value")
	fs.BoolVar(&recursive, "recursive", false, "apply recursively")
	fs.StringVar(&reason, "reason", "", "reason for the change")
	fs.BoolVar(&add, "add", false, "append value")
	fs.StringVar(&name, "name", "", "new part name")
	fs.StringVar(&parent, "parent", "", "new parent reference")
	fs.StringVar(&supersedes, "supersedes", "", "event alias to supersede")
	fs.StringVar(&because, "because", "", "cause reference")
	fs.StringVar(&ifCurrent, "if-current", "", "expected current event alias")
	fs.StringVar(&idemKey, "idempotency-key", "", "idempotency key")
	fs.StringVar(&storeFlag, "store", "", "store path")
	fs.StringVar(&fromFile, "from", "", "import Markdown from file")
	fs.BoolVar(&stdin, "stdin", false, "import Markdown from stdin")
	fs.StringVar(&kind, "kind", "", "explicit value kind (required when no schema declaration matches)")
	fs.StringVar(&assertion, "assertion", "", "assertion event alias to retract (optional; without it, all active assertions are retracted)")
	fs.BoolVar(&allowDuplicate, "allow-duplicate", false, "allow another active assertion of an identical link")
	if err := fs.Parse(args); err != nil {
		return exitInvalid
	}

	if fs.NArg() < 1 {
		printError(fmt.Errorf("set requires a reference"), exitInvalid, jsonMode, nil)
		return exitInvalid
	}
	ref := fs.Arg(0)
	value := fs.Arg(1)

	svc, err := application.Open(storeFlag)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := stdctx.Background()
	effectiveTime := time.Now().UTC()
	if effectiveAt != "" {
		effectiveTime, err = parseTime(effectiveAt)
		if err != nil {
			printError(err, exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
	}
	req := missis.RequestContext{
		Actor:          actor,
		EffectiveAt:    effectiveTime,
		IdempotencyKey: idemKey,
		IfCurrent:      ifCurrent,
		Because:        because,
	}

	if fromFile != "" || stdin {
		content, artifact, err := readImportSource(fromFile, stdin)
		if err != nil {
			printError(err, exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
		result, err := client.ReimportMarkdown(ctx, req, missis.ImportOptions{Ref: ref, Content: content, Artifact: artifact})
		if err != nil {
			printError(err, mapError(err), jsonMode, &ref)
			return mapError(err)
		}
		if jsonMode {
			writeJSON(result)
		} else {
			fmt.Printf("%s import %d parts\n", ref, result.Value)
		}
		return exitSuccess
	}

	if (add || retract) && strings.HasSuffix(ref, "/links") {
		relation, targetStr, ok := strings.Cut(value, ":")
		if !ok || relation == "" || targetStr == "" {
			printError(fmt.Errorf("link value must be relation:ref"), exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
		linkOpts := missis.LinkOptions{Ref: ref, Relation: relation, Target: targetStr, Add: add, Retract: retract, Reason: reason, Assertion: assertion}
		var result missis.SetResult
		if add && !allowDuplicate {
			result, err = svc.SetLinkIfAbsent(ctx, req, linkOpts)
		} else {
			result, err = client.SetLink(ctx, req, linkOpts)
		}
		if err != nil {
			printError(err, mapError(err), jsonMode, &ref)
			return mapError(err)
		}
		writeSetResult(result, jsonMode)
		return exitSuccess
	}

	mutationCount := 0
	for _, active := range []bool{retract, add, name != "", parent != "", supersedes != ""} {
		if active {
			mutationCount++
		}
	}
	if mutationCount > 1 {
		printError(fmt.Errorf("conflicting mutation flags: --retract, --add, --name, --parent, and --supersedes are mutually exclusive"), exitInvalid, jsonMode, &ref)
		return exitInvalid
	}
	var mutation missis.Mutation
	switch {
	case retract && recursive:
		mutation = missis.RetractSubtree{Target: ref, Reason: reason}
	case retract:
		mutation = missis.RetractValue{Target: ref, Reason: reason}
	case name != "":
		mutation = missis.RenamePart{Target: ref, Name: name, Reason: reason}
	case parent != "":
		mutation = missis.MovePart{Target: ref, Parent: parent, Reason: reason}
	case supersedes != "":
		mutation = missis.SupersedeEvent{Target: ref, Value: value, Kind: model.ValueKind(kind), Supersedes: supersedes, Reason: reason}
	case add:
		mutation = missis.AddValue{Target: ref, Value: value, Reason: reason}
	default:
		if value == "" {
			printError(fmt.Errorf("value or mutation flag is required"), exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
		mutation = missis.SetValue{Target: ref, Value: value, Kind: model.ValueKind(kind), Reason: reason}
	}
	result, err := client.Set(ctx, req, mutation)
	if err != nil {
		printError(err, mapError(err), jsonMode, &ref)
		return mapError(err)
	}
	writeSetResult(result, jsonMode)
	return exitSuccess
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func readImportSource(from string, stdin bool) (string, string, error) {
	if from != "" && stdin {
		return "", "", fmt.Errorf("--from and --stdin cannot be used together")
	}
	if from != "" {
		data, err := os.ReadFile(from)
		if err != nil {
			return "", "", err
		}
		return string(data), "artifact:" + from, nil
	}
	if stdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", "", err
		}
		return string(data), "artifact:stdin", nil
	}
	return "", "", fmt.Errorf("no Markdown import source")
}

func writeNewResult(jsonMode bool, result newResult) {
	if jsonMode {
		writeJSON(result)
		return
	}
	fmt.Printf("%s  %s\n", result.Ref, result.Title)
	fmt.Printf("status: %s\n", result.Status)
	if result.Project != nil {
		fmt.Printf("project: %s\n", *result.Project)
	}
}

func outputTicketList(summaries []missis.TicketSummary, jsonMode bool) {
	if jsonMode {
		type ticketJSON struct {
			Ref        string `json:"ref"`
			ID         string `json:"id"`
			Title      string `json:"title"`
			Status     string `json:"status"`
			RecordedAt string `json:"recorded_at"`
		}
		items := make([]ticketJSON, 0, len(summaries))
		for _, summary := range summaries {
			items = append(items, ticketJSON{
				Ref:        summary.Ref,
				ID:         summary.ID,
				Title:      summary.Title,
				Status:     summary.Status,
				RecordedAt: summary.RecordedAt.UTC().Format(time.RFC3339),
			})
		}
		writeJSON(map[string]any{"tickets": items})
		return
	}
	fmt.Printf("REF\tSTATUS\tTITLE\tRECORDED_AT\n")
	for _, summary := range summaries {
		fmt.Printf("%s\t%s\t%s\t%s\n", summary.Ref, summary.Status, summary.Title, summary.RecordedAt.UTC().Format(time.RFC3339))
	}
}

func outputEntityList(entities []missis.EntitySummary, jsonMode bool) {
	if jsonMode {
		type entityJSON struct {
			Ref        string `json:"ref"`
			ID         string `json:"id"`
			Title      string `json:"title"`
			Status     string `json:"status"`
			RecordedAt string `json:"recorded_at"`
		}
		items := make([]entityJSON, 0, len(entities))
		for _, e := range entities {
			items = append(items, entityJSON{
				Ref:        e.Ref,
				ID:         e.ID,
				Title:      e.Title,
				Status:     e.Status,
				RecordedAt: e.RecordedAt.UTC().Format(time.RFC3339),
			})
		}
		writeJSON(map[string]any{"entities": items})
		return
	}
	fmt.Printf("REF\tSTATUS\tTITLE\tRECORDED_AT\n")
	for _, e := range entities {
		fmt.Printf("%s\t%s\t%s\t%s\n", e.Ref, e.Status, e.Title, e.RecordedAt.UTC().Format(time.RFC3339))
	}
}

func pathMatches(path string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	filterKey := strings.Join(filter, "/")
	return path == filterKey || strings.HasPrefix(path, filterKey+"/")
}

func outputEventView(event missis.EventView, jsonMode bool) {
	if jsonMode {
		writeJSON(eventViewJSON(event))
		return
	}
	fmt.Printf("%s %s %s %v\n", event.Alias, event.Operation, event.Target, event.Value)
}
func printError(err error, code int, jsonMode bool, target *string) {
	if jsonMode {
		writeJSON(errorResult{
			Error:              errorCode(code),
			Target:             target,
			Message:            err.Error(),
			Ontology:           nil,
			MissingObligations: []string{},
		})
		return
	}
	fmt.Fprintf(os.Stderr, "missis: %s\n", err.Error())
}

func errorCode(code int) string {
	switch code {
	case exitInvalid:
		return "invalid_input"
	case exitNotFound:
		return "reference_not_found"
	case exitValidation:
		return "validation_failed"
	case exitConflict:
		return "concurrency_conflict"
	case exitStorage:
		return "storage_failure"
	default:
		return "error"
	}
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func newResultFromSDK(result missis.NewTicketResult) newResult {
	return newResult{
		Ref:        result.Ref,
		ID:         result.ID,
		Title:      result.Title,
		Status:     result.Status,
		Project:    result.Project,
		RecordedAt: result.RecordedAt,
	}
}

func showRefPath(ref string) []string {
	clean := strings.TrimPrefix(ref, "#")
	parts := strings.Split(clean, "/")
	if len(parts) > 1 {
		return parts[1:]
	}
	return nil
}

func writeSetResult(result missis.SetResult, jsonMode bool) {
	if jsonMode {
		writeJSON(result)
		return
	}
	fmt.Printf("%s %s", result.Ref, result.Operation)
	if result.Event != "" {
		fmt.Printf(" %s", result.Event)
	}
	fmt.Println()
	if result.Warning != "" {
		fmt.Fprintf(os.Stderr, "warning: %s\n", result.Warning)
	}
}

func mapError(err error) int {
	var de *missis.DomainError
	if errors.As(err, &de) {
		switch de.Kind {
		case missis.ErrInvalidInput:
			return exitInvalid
		case missis.ErrNotFound:
			return exitNotFound
		case missis.ErrConflict:
			return exitConflict
		case missis.ErrStorage:
			return exitStorage
		case missis.ErrValidation:
			return exitValidation
		}
	}
	return exitValidation
}

func outputProjectionSDK(proj missis.TicketProjection, pathFilter []string, jsonMode bool) {
	if !jsonMode {
		if len(pathFilter) > 0 {
			filtered := proj
			filtered.Parts = make(map[string]missis.PartView, len(proj.Parts))
			for path, part := range proj.Parts {
				if pathMatches(path, pathFilter) {
					filtered.Parts[path] = part
				}
			}
			proj = filtered
		}
		out, err := render.ShowTicket(proj, "text")
		if err != nil {
			fmt.Fprintf(os.Stderr, "missis: %v\n", err)
			os.Exit(exitInvalid)
		}
		fmt.Print(out)
		return
	}
	parts := make(map[string]showPart)
	for path, part := range proj.Parts {
		if !pathMatches(path, pathFilter) {
			continue
		}
		parts[path] = showPart{
			ID:        part.ID,
			Path:      path,
			Value:     part.Value,
			ValueKind: part.ValueKind,
			ParentID:  part.ParentID,
			CreatedBy: part.CreatedBy,
		}
	}
	writeJSON(showTicket{
		Ref:        proj.Ref,
		ID:         proj.ID,
		Title:      proj.Title,
		Status:     proj.Status,
		RecordedAt: proj.RecordedAt.UTC().Format(time.RFC3339),
		Parts:      parts,
	})
}

func outputMarkdownProjectionSDK(proj missis.TicketProjection, pathFilter []string, links []missis.LinkView) {
	if len(pathFilter) > 0 {
		filtered := missis.TicketProjection{
			Ref:        proj.Ref,
			ID:         proj.ID,
			Title:      proj.Title,
			Status:     proj.Status,
			RecordedAt: proj.RecordedAt,
			Parts:      make(map[string]missis.PartView, len(proj.Parts)),
		}
		for path, part := range proj.Parts {
			if pathMatches(path, pathFilter) {
				filtered.Parts[path] = part
			}
		}
		proj = filtered
	}
	fmt.Print(render.ShowMarkdown(proj, links))
}

func outputHistoryViews(events []missis.EventView, jsonMode bool) {
	if jsonMode {
		items := make([]showEvent, 0, len(events))
		for _, event := range events {
			items = append(items, eventViewJSON(event))
		}
		writeJSON(map[string]any{"events": items})
		return
	}
	for _, event := range events {
		fmt.Printf("%s %s %s %v\n", event.Alias, event.Operation, event.Target, event.Value)
	}
}

func eventViewJSON(event missis.EventView) showEvent {
	return showEvent{
		ID:          event.ID,
		Alias:       event.Alias,
		Sequence:    event.Sequence,
		Operation:   event.Operation,
		Target:      event.Target,
		Value:       event.Value,
		RecordedAt:  event.RecordedAt.UTC().Format(time.RFC3339),
		EffectiveAt: event.EffectiveAt.UTC().Format(time.RFC3339),
		Actor:       event.Actor,
		Reason:      event.Reason,
	}
}

func outputReferences(links []missis.LinkView, jsonMode bool) {
	if jsonMode {
		items := make([]map[string]any, 0, len(links))
		for _, link := range links {
			assertions := make([]map[string]any, 0, len(link.Assertions))
			for _, assertion := range link.Assertions {
				assertions = append(assertions, map[string]any{
					"created_by": assertion.CreatedBy,
					"actor":      assertion.Actor,
					"sources":    assertion.Sources,
				})
			}
			items = append(items, map[string]any{
				"from":       link.From,
				"relation":   link.Relation,
				"to":         link.To,
				"direction":  link.Direction,
				"origin":     link.Origin,
				"created_by": link.CreatedBy,
				"assertions": assertions,
			})
		}
		writeJSON(map[string]any{"links": items})
		return
	}
	for _, link := range links {
		fmt.Printf("%s %s %s %s\n", link.Direction, link.Relation, link.From, link.To)
	}
}

func outputLineage(edges []missis.LineageEdge, start string, jsonMode bool) {
	if jsonMode {
		items := make([]map[string]any, 0, len(edges))
		for _, edge := range edges {
			items = append(items, map[string]any{
				"from":       edge.From,
				"relation":   edge.Relation,
				"to":         edge.To,
				"direction":  edge.Direction,
				"depth":      edge.Depth,
				"origin":     edge.Origin,
				"created_by": edge.CreatedBy,
			})
		}
		writeJSON(map[string]any{"start": start, "edges": items})
		return
	}
	for _, edge := range edges {
		fmt.Printf("%d %s %s %s %s\n", edge.Depth, edge.Direction, edge.From, edge.Relation, edge.To)
	}
}
