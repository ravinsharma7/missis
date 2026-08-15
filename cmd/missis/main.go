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
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/implementation/model"
	"github.com/ravinsharma7/missis/implementation/store"
	"github.com/ravinsharma7/missis/pkg/missis"
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

type newResult struct {
	Ref        string  `json:"ref"`
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Status     string  `json:"status"`
	Project    *string `json:"project"`
	RecordedAt string  `json:"recorded_at"`
}

type setResult struct {
	Ref       string `json:"ref"`
	Event     string `json:"event"`
	Operation string `json:"operation"`
	Value     any    `json:"value"`
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

func buildVersion() (string, string) {
	version := "dev"
	commit := "unknown"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version, commit
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			commit = setting.Value
		case "vcs.modified":
			if setting.Value == "true" {
				commit += "-dirty"
			}
		}
	}
	return version, commit
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
	currentVersion, currentCommit := buildVersion()
	latest, err := latestModuleVersion()
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	if jsonMode {
		writeJSON(map[string]string{
			"current_version": currentVersion,
			"current_commit":  currentCommit,
			"latest_version":  latest.Version,
			"latest_time":     latest.Time,
		})
		return exitSuccess
	}
	fmt.Printf("current version=%s commit=%s\n", currentVersion, currentCommit)
	fmt.Printf("latest version=%s time=%s\n", latest.Version, latest.Time)
	return exitSuccess
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
	version, commit := buildVersion()
	if jsonMode {
		writeJSON(map[string]string{
			"version": version,
			"commit":  commit,
		})
		return
	}
	fmt.Printf("missis version=%s commit=%s\n", version, commit)
}

func outputContext(storePath string, jsonMode bool) {
	project, group, focus := "none", "none", ""
	if data, err := os.ReadFile(filepath.Join(".missis.d", "active.md")); err == nil {
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
		}
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
	if err := os.MkdirAll(filepath.Dir(absTarget), 0o755); err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	rel, err := filepath.Rel(cwd, absTarget)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	if err := os.WriteFile(markerPath, []byte(rel+"\n"), 0o644); err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".missis.d"), 0o755); err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	client, err := missis.OpenPath(absTarget)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
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
	if os.Args[1] == "--init" {
		os.Exit(runInit(os.Args[2:]))
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
		usage()
		code = exitInvalid
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  missis [--init] [--self-update-check|--self-update]")
	fmt.Fprintln(os.Stderr, "  missis new|show|set ...")
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
	storePath, err := missis.ResolveStorePath(storeFlag)
	if err != nil {
		printError(err, exitInvalid, jsonMode, nil)
		return exitInvalid
	}
	client, err := missis.OpenPath(storePath)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	defer client.Close()
	db := client.Store()

	recordedAt := time.Now().UTC()
	effectiveTime := recordedAt
	if effectiveAt != "" {
		effectiveTime, err = parseTime(effectiveAt)
		if err != nil {
			printError(err, exitInvalid, jsonMode, nil)
			return exitInvalid
		}
	}

	if kind != "" {
		if kind != "project" && kind != "group" {
			printError(fmt.Errorf("invalid kind: %s", kind), exitInvalid, jsonMode, nil)
			return exitInvalid
		}
		if id == "" {
			printError(fmt.Errorf("--id is required for project or group"), exitInvalid, jsonMode, nil)
			return exitInvalid
		}
		if err := model.ValidatePathSegments([]string{id}); err != nil {
			printError(err, exitInvalid, jsonMode, nil)
			return exitInvalid
		}
		existing, err := db.LoadEvents()
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		for _, event := range existing {
			if event.Target.Kind == model.Kind(kind) && event.Target.Entity == id {
				printError(fmt.Errorf("%s already exists: %s", kind, id), exitValidation, jsonMode, nil)
				return exitValidation
			}
		}
		stream := model.Ref{Kind: model.Kind(kind), Entity: id}
		event := model.Event{
			ID:          model.EventID(missis.NewID("event")),
			Stream:      stream,
			Operation:   model.OpCreateEntity,
			Target:      model.Ref{Kind: model.Kind(kind), Entity: id},
			Value:       model.Value{Kind: model.ValueKindText, Text: fs.Arg(0)},
			RecordedAt:  recordedAt,
			EffectiveAt: effectiveTime,
			Actor:       parseActor(actor),
		}
		result := newResult{Ref: kind + ":" + id, ID: kind + ":" + id, Title: fs.Arg(0), Status: "open", RecordedAt: recordedAt.Format(time.RFC3339)}
		if _, err := db.AppendBatch([]model.Event{event}, idemKey, nil, &result); err != nil {
			printError(err, mapStoreError(err), jsonMode, nil)
			return mapStoreError(err)
		}
		if jsonMode {
			writeJSON(result)
		} else {
			fmt.Printf("%s:%s  %s\n", kind, id, fs.Arg(0))
		}
		return exitSuccess
	}

	if fromFile != "" || stdin {
		content, artifact, err := readImportSource(fromFile, stdin)
		if err != nil {
			printError(err, exitInvalid, jsonMode, nil)
			return exitInvalid
		}
		parts, err := model.ParseMarkdownParts(content)
		if err != nil {
			printError(err, exitValidation, jsonMode, nil)
			return exitValidation
		}
		title := fs.Arg(0)
		if title == "" {
			for i, part := range parts {
				if len(part.Path) == 1 {
					title = part.Path[0]
					parts = append(parts[:i], parts[i+1:]...)
					break
				}
			}
		}
		if title == "" {
			if fromFile != "" {
				title = filepath.Base(fromFile)
			} else {
				title = "stdin"
			}
		}
		ticketID := model.TicketID(missis.NewID("ticket"))
		batchID := model.BatchID(missis.NewID("batch"))
		stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
		actorRef := parseActor(actor)
		events := []model.Event{
			missis.NewEvent(stream, model.OpCreateEntity, model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, model.Value{}, actorRef, recordedAt, effectiveTime, batchID, ""),
			missis.PartEvent(stream, "title", title, model.ValueKindText, actorRef, recordedAt, effectiveTime, batchID),
			missis.PartEvent(stream, "status", "open", model.ValueKindStatus, actorRef, recordedAt, effectiveTime, batchID),
		}
		events = append(events, buildImportEvents(stream, parts, actorRef, recordedAt, effectiveTime, batchID, artifact)...)
		result := newResult{}
		outcome, alias, err := db.AppendTicketBatch(events, idemKey, &result)
		if err != nil {
			printError(err, mapStoreError(err), jsonMode, nil)
			return mapStoreError(err)
		}
		if outcome.Replayed {
			writeNewResult(jsonMode, result)
			return exitSuccess
		}
		result = newResult{Ref: "#" + strconv.FormatUint(alias, 10), ID: string(ticketID), Title: title, Status: "open", Project: stringPtrOrNil(project), RecordedAt: recordedAt.Format(time.RFC3339)}
		if idemKey != "" {
			_ = db.UpdateIdempotencyResult(idemKey, result)
		}
		writeNewResult(jsonMode, result)
		return exitSuccess
	}

	ticketID := model.TicketID(missis.NewID("ticket"))
	batchID := model.BatchID(missis.NewID("batch"))
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	actorRef := parseActor(actor)
	events := []model.Event{
		missis.NewEvent(stream, model.OpCreateEntity, model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, model.Value{}, actorRef, recordedAt, effectiveTime, batchID, ""),
		missis.PartEvent(stream, "title", title, model.ValueKindText, actorRef, recordedAt, effectiveTime, batchID),
		missis.PartEvent(stream, "status", "open", model.ValueKindStatus, actorRef, recordedAt, effectiveTime, batchID),
	}
	if priority != "" {
		events = append(events, missis.PartEvent(stream, "priority", priority, model.ValueKindPriority, actorRef, recordedAt, effectiveTime, batchID))
	}
	if len(types) > 0 {
		events = append(events, missis.PartEvent(stream, "type", []string(types), model.ValueKindList, actorRef, recordedAt, effectiveTime, batchID))
	}
	if len(tags) > 0 {
		events = append(events, missis.PartEvent(stream, "tag", []string(tags), model.ValueKindList, actorRef, recordedAt, effectiveTime, batchID))
	}

	result := newResult{}
	outcome, alias, err := db.AppendTicketBatch(events, idemKey, &result)
	if err != nil {
		printError(err, mapStoreError(err), jsonMode, nil)
		return mapStoreError(err)
	}
	if outcome.Replayed {
		if jsonMode {
			writeJSON(result)
		} else {
			fmt.Printf("%s  %s\n", result.Ref, result.Title)
			fmt.Printf("status: %s\n", result.Status)
		}
		return exitSuccess
	}
	result = newResult{
		Ref:        "#" + strconv.FormatUint(alias, 10),
		ID:         string(ticketID),
		Title:      title,
		Status:     "open",
		Project:    stringPtrOrNil(project),
		RecordedAt: recordedAt.Format(time.RFC3339),
	}
	if idemKey != "" {
		if err := db.UpdateIdempotencyResult(idemKey, result); err != nil {
			printError(err, mapStoreError(err), jsonMode, nil)
			return mapStoreError(err)
		}
	}
	if result.Ref == "" {
		result = newResult{
			Ref:        "#" + strconv.FormatUint(alias, 10),
			ID:         string(ticketID),
			Title:      title,
			Status:     "open",
			Project:    stringPtrOrNil(project),
			RecordedAt: recordedAt.Format(time.RFC3339),
		}
	}

	if jsonMode {
		writeJSON(result)
	} else {
		projectText := ""
		if project != "" {
			projectText = project
		}
		fmt.Printf("%s  %s\n", result.Ref, title)
		fmt.Printf("status: open\n")
		if projectText != "" {
			fmt.Printf("project: %s\n", projectText)
		}
	}
	return exitSuccess
}

func runShow(args []string) int {
	args = reorderArgs(args, map[string]bool{
		"at": true, "effective-at": true, "known-at": true,
		"since": true, "between": true, "store": true,
		"direction": true, "depth": true, "relations": true, "format": true,
		"project": true, "group": true,
		"search": true, "status": true, "type": true, "tag": true,
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
		relations   string
		format      string
		project     string
		group       string
		search      string
		status      string
		typeFilter  string
		tagFilter   string
		version     bool
		context     bool
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
	fs.StringVar(&relations, "relations", "", "comma-separated relation allow-list")
	fs.StringVar(&format, "format", "", "output format: text, json, or markdown")
	fs.StringVar(&project, "project", "", "show project scope")
	fs.StringVar(&group, "group", "", "show group scope")
	fs.StringVar(&search, "search", "", "search query")
	fs.StringVar(&status, "status", "", "filter by status")
	fs.StringVar(&typeFilter, "type", "", "filter by type")
	fs.StringVar(&tagFilter, "tag", "", "filter by tag")
	fs.BoolVar(&version, "version", false, "show version")
	fs.BoolVar(&context, "context", false, "show active project/group context")
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
	client, err := missis.OpenPath(storePath)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	defer client.Close()
	db := client.Store()

	if health {
		if err := db.CheckConsistency(); err != nil {
			if jsonMode {
				writeJSON(errorResult{Error: "storage_failure", Target: nil, Message: err.Error(), Ontology: nil, MissingObligations: []string{}})
			} else {
				fmt.Fprintf(os.Stderr, "missis: consistency failure: %s\n", err.Error())
			}
			return exitStorage
		}
		storeID, _ := db.StoreID()
		headHash, _ := db.HeadHash()
		eventCount, _ := db.EventCount()
		version, commit := buildVersion()
		if jsonMode {
			writeJSON(map[string]any{
				"status":      "ok",
				"store_id":    storeID,
				"head_hash":   headHash,
				"event_count": eventCount,
				"version":     version,
				"commit":      commit,
			})
		} else {
			fmt.Printf("ok store=%s head=%s events=%d version=%s commit=%s\n", storeID, headHash, eventCount, version, commit)
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

	if project != "" || group != "" || search != "" || status != "" || typeFilter != "" || tagFilter != "" {
		summaries, err := db.ListTickets(effectiveTime)
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		filtered, err := filterTicketSummaries(db, summaries, search, status, project, group, typeFilter, tagFilter, effectiveTime, knownTime)
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		outputTicketList(filtered, jsonMode)
		return exitSuccess
	}

	if ref == "" {
		summaries, err := db.ListTickets(effectiveTime)
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		outputTicketList(summaries, jsonMode)
		return exitSuccess
	}

	if strings.HasPrefix(ref, "@") {
		event, err := db.GetEventByAlias(ref)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &ref)
			return exitNotFound
		}
		outputEvent(event, jsonMode)
		return exitSuccess
	}

	ticketID, partPath, err := resolveTicketRef(db, ref, effectiveTime)
	if err != nil {
		printError(err, exitNotFound, jsonMode, &ref)
		return exitNotFound
	}

	if history {
		events, err := db.LoadTicketEvents(ticketID)
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		filtered := filterHistory(events, effectiveTime, knownTime, since, between, partPath)
		outputHistory(filtered, jsonMode)
		return exitSuccess
	}

	if lineage {
		targetRef, err := resolveAnyRef(db, ref, ticketID, effectiveTime)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &ref)
			return exitNotFound
		}
		events, err := db.LoadLinkEvents()
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		graph, err := model.BuildLineageGraph(events, effectiveTime, knownTime)
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		relationSet := make(map[string]bool)
		if relations != "" {
			for _, relation := range strings.Split(relations, ",") {
				relation = strings.TrimSpace(relation)
				if relation == "" {
					continue
				}
				if !model.ValidRelation(relation) {
					printError(fmt.Errorf("unsupported relation: %s", relation), exitValidation, jsonMode, &ref)
					return exitValidation
				}
				relationSet[relation] = true
			}
		}
		edges, err := graph.Walk(targetRef, direction, depth, relationSet)
		if err != nil {
			printError(err, exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
		if jsonMode {
			items := make([]map[string]any, 0, len(edges))
			for _, edge := range edges {
				items = append(items, map[string]any{
					"from":       targetText(edge.From),
					"relation":   edge.Relation,
					"to":         targetText(edge.To),
					"direction":  edge.Direction,
					"depth":      edge.Depth,
					"origin":     edge.Origin,
					"created_by": string(edge.CreatedBy),
				})
			}
			writeJSON(map[string]any{"start": targetText(targetRef), "edges": items})
		} else {
			for _, edge := range edges {
				fmt.Printf("%d %s %s %s %s\n", edge.Depth, edge.Direction, targetText(edge.From), edge.Relation, targetText(edge.To))
			}
		}
		return exitSuccess
	}

	if references {
		targetRef, err := resolveAnyRef(db, ref, ticketID, effectiveTime)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &ref)
			return exitNotFound
		}
		events, err := db.LoadLinkEvents()
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		links, err := model.LinksForRef(events, targetRef, effectiveTime, knownTime)
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		if jsonMode {
			items := make([]map[string]any, 0, len(links))
			for _, link := range links {
				items = append(items, map[string]any{
					"from":       targetText(link.From),
					"relation":   link.Relation,
					"to":         targetText(link.To),
					"direction":  link.Direction,
					"origin":     link.Origin,
					"created_by": string(link.CreatedBy),
				})
			}
			writeJSON(map[string]any{"links": items})
		} else {
			for _, link := range links {
				fmt.Printf("%s %s %s %s\n", link.Direction, link.Relation, targetText(link.From), targetText(link.To))
			}
		}
		return exitSuccess
	}

	proj, err := db.BitemporalProjection(ticketID, effectiveTime, knownTime)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	if len(partPath) > 0 {
		pathKey := strings.Join(partPath, "/")
		if _, ok := proj.Paths[pathKey]; !ok {
			printError(fmt.Errorf("part path not found: %s", pathKey), exitNotFound, jsonMode, &ref)
			return exitNotFound
		}
	}
	recordedAtText := ticketRecordedAt(db, ticketID)
	ticketRef := ticketRefFor(db, ticketID)
	if format == "markdown" {
		targetRef, err := resolveAnyRef(db, ref, ticketID, effectiveTime)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &ref)
			return exitNotFound
		}
		linkEvents, err := db.LoadLinkEvents()
		if err != nil {
			printError(err, exitStorage, jsonMode, nil)
			return exitStorage
		}
		links, _ := model.LinksForRef(linkEvents, targetRef, effectiveTime, knownTime)
		outputMarkdownProjection(ticketID, proj, partPath, ticketRef, links)
		return exitSuccess
	}
	outputProjection(ticketID, proj, partPath, jsonMode, recordedAtText, ticketRef)
	return exitSuccess
}

func runSet(args []string) int {
	args = reorderArgs(args, map[string]bool{
		"actor": true, "effective-at": true, "reason": true, "name": true,
		"parent": true, "supersedes": true, "because": true,
		"if-current": true, "idempotency-key": true, "store": true,
		"from": true,
	})
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		jsonMode    bool
		actor       string
		effectiveAt string
		retract     bool
		recursive   bool
		reason      string
		add         bool
		name        string
		parent      string
		supersedes  string
		because     string
		ifCurrent   string
		idemKey     string
		storeFlag   string
		fromFile    string
		stdin       bool
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
	if err := fs.Parse(args); err != nil {
		return exitInvalid
	}

	if fs.NArg() < 1 {
		printError(fmt.Errorf("set requires a reference"), exitInvalid, jsonMode, nil)
		return exitInvalid
	}
	ref := fs.Arg(0)
	value := fs.Arg(1)

	storePath, err := missis.ResolveStorePath(storeFlag)
	if err != nil {
		printError(err, exitInvalid, jsonMode, nil)
		return exitInvalid
	}
	client, err := missis.OpenPath(storePath)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	defer client.Close()
	db := client.Store()

	recordedAt := time.Now().UTC()
	effectiveTime := recordedAt
	if effectiveAt != "" {
		effectiveTime, err = parseTime(effectiveAt)
		if err != nil {
			printError(err, exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
	}

	if fromFile != "" || stdin {
		content, artifact, err := readImportSource(fromFile, stdin)
		if err != nil {
			printError(err, exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
		parts, err := model.ParseMarkdownParts(content)
		if err != nil {
			printError(err, exitValidation, jsonMode, &ref)
			return exitValidation
		}
		for i, part := range parts {
			if len(part.Path) == 1 {
				parts = append(parts[:i], parts[i+1:]...)
				break
			}
		}
		ticketID, partPath, err := resolveTicketRef(db, ref, effectiveTime)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &ref)
			return exitNotFound
		}
		if len(partPath) != 0 {
			printError(fmt.Errorf("import target must be a ticket reference"), exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
		batchID := model.BatchID(missis.NewID("batch"))
		actorRef := parseActor(actor)
		events, err := buildReimportEvents(db, ticketID, parts, actorRef, recordedAt, effectiveTime, batchID, artifact)
		if err != nil {
			printError(err, exitValidation, jsonMode, &ref)
			return exitValidation
		}
		if len(events) == 0 {
			if jsonMode {
				writeJSON(setResult{Ref: ref, Operation: "import", Value: 0})
			} else {
				fmt.Printf("%s import 0 parts\n", ref)
			}
			return exitSuccess
		}
		result := setResult{Ref: ref, Operation: "import", Value: len(events)}
		outcome, err := db.AppendBatch(events, idemKey, nil, &result)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				printError(err, exitConflict, jsonMode, &ref)
				return exitConflict
			}
			printError(err, mapStoreError(err), jsonMode, &ref)
			return mapStoreError(err)
		}
		if len(outcome.Events) > 0 {
			result.Event = "@e" + fmt.Sprintf("%d", outcome.Events[len(outcome.Events)-1].AliasSeq)
		}
		if jsonMode {
			writeJSON(result)
		} else {
			fmt.Printf("%s import %d parts\n", ref, len(events))
		}
		return exitSuccess
	}

	if (add || retract) && strings.HasSuffix(ref, "/links") && (strings.HasPrefix(ref, "project:") || strings.HasPrefix(ref, "group:")) {
		return runSetScopeLink(db, ref, value, actor, reason, effectiveTime, recordedAt, add, retract, idemKey, jsonMode)
	}

	baseTicketID, basePath, err := resolveTicketRef(db, ref, effectiveTime)
	if err == nil && len(basePath) > 0 && basePath[len(basePath)-1] == "links" && (add || retract) {
		return runSetLink(db, ref, baseTicketID, basePath, value, actor, reason, effectiveTime, recordedAt, add, retract, idemKey, jsonMode)
	}

	actorRef := parseActor(actor)
	batchID := model.BatchID(missis.NewID("batch"))

	requiresExisting := retract || name != "" || parent != ""
	var (
		ticketID       model.TicketID
		partID         model.PartID
		currentPath    []string
		creationEvents []model.Event
		partExisted    bool
		stream         model.Ref
	)
	if requiresExisting {
		ticketID, partID, currentPath, err = resolvePartRef(db, ref, effectiveTime)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &ref)
			return exitNotFound
		}
		partExisted = true
		stream = model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	} else {
		ticketID, currentPath, err = resolveTicketRef(db, ref, effectiveTime)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &ref)
			return exitNotFound
		}
		if len(currentPath) == 0 {
			printError(fmt.Errorf("part reference required"), exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
		stream = model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
		creationEvents, partID, partExisted, err = ensurePartPath(db, ticketID, currentPath, actorRef, recordedAt, effectiveTime, stream, batchID)
		if err != nil {
			printError(err, exitStorage, jsonMode, &ref)
			return exitStorage
		}
	}

	target := model.Ref{Kind: model.KindPart, Entity: string(partID), Path: currentPath}
	event := model.Event{
		Stream:      stream,
		Target:      target,
		RecordedAt:  recordedAt,
		EffectiveAt: effectiveTime,
		Actor:       actorRef,
		Reason:      reason,
		BatchID:     &batchID,
	}

	switch {
	case retract && recursive:
		event.Operation = model.OpRetractSubtree
	case retract:
		event.Operation = model.OpRetractValue
	case name != "":
		if err := model.ValidatePathSegments([]string{name}); err != nil {
			printError(err, exitValidation, jsonMode, &ref)
			return exitValidation
		}
		event.Operation = model.OpRenamePart
		event.Value = model.Value{Kind: model.ValueKindText, Text: name}
	case parent != "":
		parentRef, err := resolveParentRef(db, parent, ticketID, effectiveTime)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &parent)
			return exitNotFound
		}
		event.Operation = model.OpMovePart
		event.Value = model.Value{Ref: &parentRef}
	default:
		valueKind := inferValueKind(currentPath, value)
		if value != "" || add {
			event.Operation = model.OpSetValue
			if add {
				event.Operation = model.OpAddValue
			}
			if add {
				event.Value = model.Value{Kind: model.ValueKindList, Text: value}
			} else {
				event.Value = model.Value{Kind: valueKind, Text: value}
			}
			if valueKind == model.ValueKindList || valueKind == model.ValueKindJSON {
				event.Value.Data = value
			}
		} else {
			printError(fmt.Errorf("value or mutation flag is required"), exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
	}

	if supersedes != "" {
		oldEvent, err := db.GetEventByAlias(supersedes)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &supersedes)
			return exitNotFound
		}
		event.Supersedes = append(event.Supersedes, oldEvent.ID)
		event.Operation = model.OpSupersedeEvent
	}

	if because != "" {
		causeRef, err := parseReference(db, because, ticketID, effectiveTime)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &because)
			return exitNotFound
		}
		event.Causes = append(event.Causes, causeRef)
	}

	if !retract && name == "" && parent == "" {
		if err := validateStatusSet(currentPath, value, reason); err != nil {
			printError(err, exitValidation, jsonMode, &ref)
			return exitValidation
		}
	}

	var preconditions []store.Precondition
	if ifCurrent != "" {
		currentEvent, err := db.GetEventByAlias(ifCurrent)
		if err != nil {
			printError(err, exitNotFound, jsonMode, &ifCurrent)
			return exitNotFound
		}
		if !partExisted {
			printError(fmt.Errorf("expected current event on new part"), exitConflict, jsonMode, &ref)
			return exitConflict
		}
		preconditions = append(preconditions, store.Precondition{
			TargetEntity:         string(partID),
			ExpectedCurrentEvent: currentEvent.ID,
		})
	}

	result := setResult{
		Ref:       ref,
		Operation: string(event.Operation),
		Value:     valueOrNil(event.Value),
	}
	batch := append(creationEvents, event)
	outcome, err := db.AppendBatch(batch, idemKey, preconditions, &result)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			printError(err, exitConflict, jsonMode, &ref)
			return exitConflict
		}
		printError(err, mapStoreError(err), jsonMode, &ref)
		return mapStoreError(err)
	}
	if len(outcome.Events) > 0 {
		last := outcome.Events[len(outcome.Events)-1]
		result.Event = "@e" + fmt.Sprintf("%d", last.AliasSeq)
	}

	if jsonMode {
		writeJSON(result)
	} else {
		fmt.Printf("%s %s", ref, string(event.Operation))
		if result.Event != "" {
			fmt.Printf(" %s", result.Event)
		}
		fmt.Println()
	}
	return exitSuccess
}

func runSetLink(db *store.Store, ref string, ticketID model.TicketID, path []string, value, actor, reason string, effectiveTime, recordedAt time.Time, add, retract bool, idemKey string, jsonMode bool) int {
	if !add && !retract {
		printError(fmt.Errorf("link mutation requires --add or --retract"), exitInvalid, jsonMode, &ref)
		return exitInvalid
	}
	relation, targetStr, ok := strings.Cut(value, ":")
	if !ok || relation == "" || targetStr == "" {
		printError(fmt.Errorf("link value must be relation:ref"), exitInvalid, jsonMode, &ref)
		return exitInvalid
	}
	if !model.ValidRelation(relation) {
		printError(fmt.Errorf("unsupported relation: %s", relation), exitValidation, jsonMode, &ref)
		return exitValidation
	}

	fromRef, err := resolveAnyRef(db, strings.TrimSuffix(ref, "/links"), ticketID, effectiveTime)
	if err != nil {
		printError(err, exitNotFound, jsonMode, &ref)
		return exitNotFound
	}
	toRef, err := resolveAnyRef(db, targetStr, ticketID, effectiveTime)
	if err != nil {
		printError(err, exitNotFound, jsonMode, &targetStr)
		return exitNotFound
	}

	operation := model.OpAssertLink
	if retract {
		operation = model.OpRetractLink
	}
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	event := model.Event{
		Stream:      stream,
		Operation:   operation,
		Target:      fromRef,
		Value:       model.Value{Text: relation, Ref: &toRef},
		RecordedAt:  recordedAt,
		EffectiveAt: effectiveTime,
		Actor:       parseActor(actor),
		Reason:      reason,
	}
	if idemKey != "" {
		batchID := model.BatchID(missis.NewID("batch"))
		event.BatchID = &batchID
	}

	result := setResult{
		Ref:       ref,
		Operation: string(operation),
		Value:     relation + ":" + targetStr,
	}
	outcome, err := db.AppendBatch([]model.Event{event}, idemKey, nil, &result)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			printError(err, exitConflict, jsonMode, &ref)
			return exitConflict
		}
		printError(err, mapStoreError(err), jsonMode, &ref)
		return mapStoreError(err)
	}
	if len(outcome.Events) > 0 {
		result.Event = "@e" + fmt.Sprintf("%d", outcome.Events[len(outcome.Events)-1].AliasSeq)
	}
	if jsonMode {
		writeJSON(result)
	} else {
		fmt.Printf("%s %s %s\n", ref, operation, result.Value)
	}
	return exitSuccess
}

func runSetScopeLink(db *store.Store, ref, value, actor, reason string, effectiveTime, recordedAt time.Time, add, retract bool, idemKey string, jsonMode bool) int {
	if !add && !retract {
		printError(fmt.Errorf("link mutation requires --add or --retract"), exitInvalid, jsonMode, &ref)
		return exitInvalid
	}
	relation, targetStr, ok := strings.Cut(value, ":")
	if !ok || relation == "" || targetStr == "" {
		printError(fmt.Errorf("link value must be relation:ref"), exitInvalid, jsonMode, &ref)
		return exitInvalid
	}
	if !model.ValidRelation(relation) {
		printError(fmt.Errorf("unsupported relation: %s", relation), exitValidation, jsonMode, &ref)
		return exitValidation
	}
	fromRef, err := resolveAnyRef(db, strings.TrimSuffix(ref, "/links"), "", effectiveTime)
	if err != nil {
		printError(err, exitNotFound, jsonMode, &ref)
		return exitNotFound
	}
	toRef, err := resolveAnyRef(db, targetStr, "", effectiveTime)
	if err != nil {
		printError(err, exitNotFound, jsonMode, &targetStr)
		return exitNotFound
	}
	operation := model.OpAssertLink
	if retract {
		operation = model.OpRetractLink
	}
	stream := model.Ref{Kind: fromRef.Kind, Entity: fromRef.Entity}
	event := model.Event{
		Stream:      stream,
		Operation:   operation,
		Target:      fromRef,
		Value:       model.Value{Text: relation, Ref: &toRef},
		RecordedAt:  recordedAt,
		EffectiveAt: effectiveTime,
		Actor:       parseActor(actor),
		Reason:      reason,
	}
	result := setResult{Ref: ref, Operation: string(operation), Value: relation + ":" + targetStr}
	outcome, err := db.AppendBatch([]model.Event{event}, idemKey, nil, &result)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			printError(err, exitConflict, jsonMode, &ref)
			return exitConflict
		}
		printError(err, mapStoreError(err), jsonMode, &ref)
		return mapStoreError(err)
	}
	if len(outcome.Events) > 0 {
		result.Event = "@e" + fmt.Sprintf("%d", outcome.Events[len(outcome.Events)-1].AliasSeq)
	}
	if jsonMode {
		writeJSON(result)
	} else {
		fmt.Printf("%s %s %s\n", ref, operation, result.Value)
	}
	return exitSuccess
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func parseActor(value string) model.ActorRef {
	kind := "human"
	if idx := strings.IndexByte(value, '/'); idx > 0 {
		kind = value[:idx]
	}
	return model.ActorRef{Kind: kind, ID: value, Name: value}
}

func shortID(id model.TicketID) string {
	raw := strings.TrimPrefix(string(id), "ticket:")
	if len(raw) > 8 {
		return raw[:8]
	}
	return raw
}

func ticketRefFor(db *store.Store, ticketID model.TicketID) string {
	number, err := db.LookupTicketAlias(ticketID)
	if err == nil && number > 0 {
		return "#" + strconv.FormatUint(number, 10)
	}
	return "#" + shortID(ticketID)
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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

func buildImportEvents(stream model.Ref, parts []model.MarkdownPart, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID, artifact string) []model.Event {
	events := make([]model.Event, 0, len(parts))
	partIDs := make(map[string]model.PartID)
	sort.Slice(parts, func(i, j int) bool {
		if len(parts[i].Path) != len(parts[j].Path) {
			return len(parts[i].Path) < len(parts[j].Path)
		}
		return strings.Join(parts[i].Path, "/") < strings.Join(parts[j].Path, "/")
	})
	for _, part := range parts {
		partIDs[strings.Join(part.Path, "/")] = model.PartID(missis.NewID("part"))
	}
	for _, part := range parts {
		start, end := part.StartLine, part.EndLine
		source := model.SourceRef{
			Ref:       model.Ref{Kind: model.KindArtifact, Entity: artifact},
			MediaType: "text/markdown",
			Span:      &model.Span{StartLine: &start, EndLine: &end},
		}
		value := model.Value{}
		var parentRef *model.Ref
		if len(part.Path) > 1 {
			parentKey := strings.Join(part.Path[:len(part.Path)-1], "/")
			if parentID, ok := partIDs[parentKey]; ok {
				parentRef = &model.Ref{Kind: model.KindPart, Entity: string(parentID)}
			}
		}
		if part.Body != "" {
			value = model.Value{Kind: model.ValueKindMarkdown, Text: part.Body}
		}
		value.Ref = parentRef
		partID := partIDs[strings.Join(part.Path, "/")]
		events = append(events, model.Event{
			ID:          model.EventID(missis.NewID("event")),
			Stream:      stream,
			Operation:   model.OpCreatePart,
			Target:      model.Ref{Kind: model.KindPart, Entity: string(partID), Path: part.Path},
			Value:       value,
			RecordedAt:  recordedAt,
			EffectiveAt: effectiveAt,
			Actor:       actor,
			BatchID:     &batchID,
			Sources:     []model.SourceRef{source},
		})
	}
	return events
}

func buildReimportEvents(db *store.Store, ticketID model.TicketID, parts []model.MarkdownPart, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID, artifact string) ([]model.Event, error) {
	proj, err := db.CurrentProjection(ticketID, effectiveAt)
	if err != nil {
		return nil, err
	}
	sort.Slice(parts, func(i, j int) bool {
		if len(parts[i].Path) != len(parts[j].Path) {
			return len(parts[i].Path) < len(parts[j].Path)
		}
		return strings.Join(parts[i].Path, "/") < strings.Join(parts[j].Path, "/")
	})

	pathToID := make(map[string]model.PartID, len(proj.Paths))
	for path, id := range proj.Paths {
		pathToID[path] = id
	}
	matched := make(map[model.PartID]bool)
	events := make([]model.Event, 0, len(parts))

	for _, part := range parts {
		pathKey := strings.Join(part.Path, "/")
		partID, ok := pathToID[pathKey]
		if !ok {
			for id, existing := range proj.Parts {
				if sourceMatchesArtifact(existing, artifact, part.StartLine, part.EndLine) {
					partID = id
					break
				}
			}
		}

		if partID == "" {
			partID = model.PartID(missis.NewID("part"))
			parentRef := parentRefForPath(part.Path, pathToID)
			events = append(events, importPartEvent(proj.TicketID, partID, part, parentRef, actor, recordedAt, effectiveAt, batchID, artifact, model.OpCreatePart, model.ValueKindMarkdown))
			pathToID[pathKey] = partID
			continue
		}

		matched[partID] = true
		existing := proj.Parts[partID]
		existingPath := currentPathForPart(proj, partID)
		if !equalPaths(existingPath, part.Path) {
			if parentPathsDiffer(existingPath, part.Path) {
				parentRef := parentRefForPath(part.Path, pathToID)
				events = append(events, importPartEvent(proj.TicketID, partID, part, parentRef, actor, recordedAt, effectiveAt, batchID, artifact, model.OpMovePart, ""))
			}
			if len(existingPath) == 0 || existingPath[len(existingPath)-1] != part.Path[len(part.Path)-1] {
				events = append(events, importPartEvent(proj.TicketID, partID, part, nil, actor, recordedAt, effectiveAt, batchID, artifact, model.OpRenamePart, model.ValueKindText))
			}
			pathToID[pathKey] = partID
		}

		currentBody := ""
		if existing.Value != nil {
			currentBody = existing.Value.Text
		}
		if part.Body != currentBody {
			events = append(events, importPartEvent(proj.TicketID, partID, part, nil, actor, recordedAt, effectiveAt, batchID, artifact, model.OpSetValue, model.ValueKindMarkdown))
		}
	}

	for id, existing := range proj.Parts {
		if !matched[id] && sourceHasArtifact(existing, artifact) {
			path := currentPathForPart(proj, id)
			return nil, fmt.Errorf("existing imported part missing from source: %s", strings.Join(path, "/"))
		}
	}
	return events, nil
}

func importPartEvent(ticketID model.TicketID, partID model.PartID, part model.MarkdownPart, parentRef *model.Ref, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID, artifact string, operation model.Operation, valueKind model.ValueKind) model.Event {
	start, end := part.StartLine, part.EndLine
	source := model.SourceRef{
		Ref:       model.Ref{Kind: model.KindArtifact, Entity: artifact},
		MediaType: "text/markdown",
		Span:      &model.Span{StartLine: &start, EndLine: &end},
	}
	value := model.Value{}
	switch operation {
	case model.OpCreatePart:
		if part.Body != "" {
			value = model.Value{Kind: valueKind, Text: part.Body}
		}
		value.Ref = parentRef
	case model.OpSetValue:
		value = model.Value{Kind: valueKind, Text: part.Body}
	case model.OpRenamePart:
		value = model.Value{Kind: valueKind, Text: part.Path[len(part.Path)-1]}
	case model.OpMovePart:
		value = model.Value{Ref: parentRef}
	}
	return model.Event{
		ID:          model.EventID(missis.NewID("event")),
		Stream:      model.Ref{Kind: model.KindTicket, Entity: string(ticketID)},
		Operation:   operation,
		Target:      model.Ref{Kind: model.KindPart, Entity: string(partID), Path: part.Path},
		Value:       value,
		RecordedAt:  recordedAt,
		EffectiveAt: effectiveAt,
		Actor:       actor,
		BatchID:     &batchID,
		Sources:     []model.SourceRef{source},
	}
}

func sourceMatchesArtifact(part *model.Part, artifact string, startLine, endLine int) bool {
	for _, source := range part.Sources {
		if source.Ref.Entity != artifact || source.Span == nil {
			continue
		}
		sourceStart := 0
		sourceEnd := 0
		if source.Span.StartLine != nil {
			sourceStart = *source.Span.StartLine
		}
		if source.Span.EndLine != nil {
			sourceEnd = *source.Span.EndLine
		}
		if startLine <= sourceEnd && endLine >= sourceStart {
			return true
		}
	}
	return false
}

func sourceHasArtifact(part *model.Part, artifact string) bool {
	for _, source := range part.Sources {
		if source.Ref.Entity == artifact {
			return true
		}
	}
	return false
}

func parentRefForPath(path []string, pathToID map[string]model.PartID) *model.Ref {
	if len(path) <= 1 {
		return nil
	}
	parentKey := strings.Join(path[:len(path)-1], "/")
	parentID, ok := pathToID[parentKey]
	if !ok {
		return nil
	}
	return &model.Ref{Kind: model.KindPart, Entity: string(parentID)}
}

func equalPaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func parentPathsDiffer(a, b []string) bool {
	if len(a) <= 1 || len(b) <= 1 {
		return len(a) != len(b)
	}
	return !equalPaths(a[:len(a)-1], b[:len(b)-1])
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

func resolveTicketRef(db *store.Store, ref string, effectiveAt time.Time) (model.TicketID, []string, error) {
	clean := strings.TrimPrefix(ref, "#")
	parts := strings.Split(clean, "/")
	short := parts[0]
	summaries, err := db.ListTickets(effectiveAt)
	if err != nil {
		return "", nil, err
	}
	var ticketID model.TicketID
	for _, summary := range summaries {
		if summary.Ref == "#"+short || strconv.FormatUint(summary.Number, 10) == short || string(summary.ID) == short {
			ticketID = summary.ID
			break
		}
	}
	if ticketID == "" {
		if strings.HasPrefix(short, "ticket:") {
			ticketID = model.TicketID(short)
		} else {
			return "", nil, fmt.Errorf("ticket not found: %s", short)
		}
	}
	var path []string
	if len(parts) > 1 {
		path = parts[1:]
	}
	return ticketID, path, nil
}

func resolvePartRef(db *store.Store, ref string, effectiveAt time.Time) (model.TicketID, model.PartID, []string, error) {
	if strings.HasPrefix(ref, "part:") {
		partID := model.PartID(strings.TrimPrefix(ref, "part:"))
		ticketID, err := findTicketForPart(db, partID)
		if err != nil {
			return "", "", nil, err
		}
		proj, err := db.CurrentProjection(ticketID, effectiveAt)
		if err != nil {
			return "", "", nil, err
		}
		path := currentPathForPart(proj, partID)
		return ticketID, partID, path, nil
	}
	ticketID, path, err := resolveTicketRef(db, ref, effectiveAt)
	if err != nil {
		return "", "", nil, err
	}
	if len(path) == 0 {
		return "", "", nil, fmt.Errorf("part reference required")
	}
	proj, err := db.CurrentProjection(ticketID, effectiveAt)
	if err != nil {
		return "", "", nil, err
	}
	key := strings.Join(path, "/")
	partID, ok := proj.Paths[key]
	if !ok {
		return "", "", nil, fmt.Errorf("part path not found: %s", key)
	}
	return ticketID, partID, path, nil
}

func ensurePartPath(db *store.Store, ticketID model.TicketID, path []string, actor model.ActorRef, recordedAt, effectiveAt time.Time, stream model.Ref, batchID model.BatchID) ([]model.Event, model.PartID, bool, error) {
	proj, err := db.CurrentProjection(ticketID, effectiveAt)
	if err != nil {
		return nil, "", false, err
	}
	var (
		parentID    *model.PartID
		events      []model.Event
		partID      model.PartID
		existed     = true
		currentPath []string
	)
	for _, segment := range path {
		currentPath = append(currentPath, segment)
		key := strings.Join(currentPath, "/")
		if id, ok := proj.Paths[key]; ok {
			parentID = &id
			partID = id
			continue
		}
		existed = false
		newIDValue := model.PartID(missis.NewID("part"))
		target := model.Ref{Kind: model.KindPart, Entity: string(newIDValue), Path: append([]string(nil), currentPath...)}
		var parentRef *model.Ref
		if parentID != nil {
			parentRef = &model.Ref{Kind: model.KindPart, Entity: string(*parentID)}
		}
		event := model.Event{
			ID:          model.EventID(missis.NewID("event")),
			Stream:      stream,
			Operation:   model.OpCreatePart,
			Target:      target,
			RecordedAt:  recordedAt,
			EffectiveAt: effectiveAt,
			Actor:       actor,
			BatchID:     &batchID,
		}
		if parentRef != nil {
			event.Value = model.Value{Ref: parentRef}
		}
		events = append(events, event)
		parentID = &newIDValue
		partID = newIDValue
	}
	return events, partID, existed, nil
}

func findTicketForPart(db *store.Store, partID model.PartID) (model.TicketID, error) {
	events, err := db.LoadEvents()
	if err != nil {
		return "", err
	}
	for _, event := range events {
		if event.Target.Kind == model.KindPart && event.Target.Entity == string(partID) {
			return model.TicketID(event.Stream.Entity), nil
		}
	}
	return "", fmt.Errorf("part not found: %s", partID)
}

func currentPathForPart(proj *model.Projection, partID model.PartID) []string {
	for path, id := range proj.Paths {
		if id == partID {
			return strings.Split(path, "/")
		}
	}
	return nil
}

func resolveParentRef(db *store.Store, ref string, ticketID model.TicketID, effectiveAt time.Time) (model.Ref, error) {
	if strings.HasPrefix(ref, "part:") {
		return model.Ref{Kind: model.KindPart, Entity: strings.TrimPrefix(ref, "part:")}, nil
	}
	_, partID, _, err := resolvePartRef(db, ref, effectiveAt)
	if err != nil {
		return model.Ref{}, err
	}
	return model.Ref{Kind: model.KindPart, Entity: string(partID)}, nil
}

func parseReference(db *store.Store, ref string, ticketID model.TicketID, effectiveAt time.Time) (model.Ref, error) {
	if strings.HasPrefix(ref, "part:") {
		return model.Ref{Kind: model.KindPart, Entity: strings.TrimPrefix(ref, "part:")}, nil
	}
	if strings.HasPrefix(ref, "@") {
		event, err := db.GetEventByAlias(ref)
		if err != nil {
			return model.Ref{}, err
		}
		return model.Ref{Kind: model.KindEvent, Entity: string(event.ID)}, nil
	}
	_, partID, _, err := resolvePartRef(db, ref, effectiveAt)
	if err != nil {
		return model.Ref{}, err
	}
	return model.Ref{Kind: model.KindPart, Entity: string(partID)}, nil
}

func resolveAnyRef(db *store.Store, ref string, ticketID model.TicketID, effectiveAt time.Time) (model.Ref, error) {
	if strings.HasPrefix(ref, "part:") {
		return model.Ref{Kind: model.KindPart, Entity: strings.TrimPrefix(ref, "part:")}, nil
	}
	if strings.HasPrefix(ref, "ticket:") {
		return model.Ref{Kind: model.KindTicket, Entity: strings.TrimPrefix(ref, "ticket:")}, nil
	}
	if strings.HasPrefix(ref, "project:") {
		return model.Ref{Kind: model.KindProject, Entity: strings.TrimPrefix(ref, "project:")}, nil
	}
	if strings.HasPrefix(ref, "group:") {
		return model.Ref{Kind: model.KindGroup, Entity: strings.TrimPrefix(ref, "group:")}, nil
	}
	if strings.HasPrefix(ref, "@") {
		event, err := db.GetEventByAlias(ref)
		if err != nil {
			return model.Ref{}, err
		}
		return model.Ref{Kind: model.KindEvent, Entity: string(event.ID)}, nil
	}
	if strings.HasPrefix(ref, "#") {
		ticket, path, err := resolveTicketRef(db, ref, effectiveAt)
		if err != nil {
			return model.Ref{}, err
		}
		if len(path) == 0 {
			return model.Ref{Kind: model.KindTicket, Entity: string(ticket)}, nil
		}
		_, partID, _, err := resolvePartRef(db, ref, effectiveAt)
		if err != nil {
			return model.Ref{}, err
		}
		return model.Ref{Kind: model.KindPart, Entity: string(partID), Path: path}, nil
	}
	return model.Ref{}, fmt.Errorf("unsupported reference: %s", ref)
}

func inferValueKind(path []string, value string) model.ValueKind {
	if len(path) == 0 {
		return model.ValueKindText
	}
	switch path[len(path)-1] {
	case "status":
		return model.ValueKindStatus
	case "priority":
		return model.ValueKindPriority
	default:
		if strings.HasPrefix(strings.TrimSpace(value), "{") || strings.HasPrefix(strings.TrimSpace(value), "[") {
			return model.ValueKindJSON
		}
		return model.ValueKindText
	}
}

func validateStatusSet(path []string, value, reason string) error {
	if len(path) == 0 || path[len(path)-1] != "status" {
		return nil
	}
	switch value {
	case "open", "doing", "done":
		return nil
	case "blocked":
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("blocked status requires a reason")
		}
		return nil
	default:
		return fmt.Errorf("invalid status: %s", value)
	}
}

func outputTicketList(summaries []store.TicketSummary, jsonMode bool) {
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
				ID:         string(summary.ID),
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

func filterTicketSummaries(db *store.Store, summaries []store.TicketSummary, search, status, project, group, typeFilter, tagFilter string, effectiveAt, knownAt time.Time) ([]store.TicketSummary, error) {
	var linkEvents []model.Event
	if project != "" || group != "" {
		var err error
		linkEvents, err = db.LoadLinkEvents()
		if err != nil {
			return nil, err
		}
	}
	projectTicketIDs := make(map[model.TicketID]bool)
	if project != "" {
		links, err := model.LinksForRef(linkEvents, model.Ref{Kind: model.KindProject, Entity: project}, effectiveAt, knownAt)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			if link.Relation == "contains" && link.Direction == "asserted" && link.To.Kind == model.KindTicket {
				projectTicketIDs[model.TicketID(link.To.Entity)] = true
			}
		}
	}
	if group != "" {
		groupRef := model.Ref{Kind: model.KindGroup, Entity: group}
		groupLinks, err := model.LinksForRef(linkEvents, groupRef, effectiveAt, knownAt)
		if err != nil {
			return nil, err
		}
		projectIDs := make(map[string]bool)
		for _, link := range groupLinks {
			if link.Direction == "asserted" && (link.Relation == "contains" || link.Relation == "governs") && link.To.Kind == model.KindProject {
				projectIDs[link.To.Entity] = true
			}
		}
		for projectID := range projectIDs {
			links, err := model.LinksForRef(linkEvents, model.Ref{Kind: model.KindProject, Entity: projectID}, effectiveAt, knownAt)
			if err != nil {
				return nil, err
			}
			for _, link := range links {
				if link.Relation == "contains" && link.Direction == "asserted" && link.To.Kind == model.KindTicket {
					projectTicketIDs[model.TicketID(link.To.Entity)] = true
				}
			}
		}
	}

	result := make([]store.TicketSummary, 0, len(summaries))
	for _, summary := range summaries {
		if status != "" && summary.Status != status {
			continue
		}
		if (project != "" || group != "") && !projectTicketIDs[summary.ID] {
			continue
		}
		proj, err := db.BitemporalProjection(summary.ID, effectiveAt, knownAt)
		if err != nil {
			return nil, err
		}
		if search != "" {
			text := summary.Title + " " + projectionText(proj)
			if !matchesAllTokens(text, search) {
				continue
			}
		}
		if typeFilter != "" && !partHasValue(proj, "type", typeFilter) {
			continue
		}
		if tagFilter != "" && !partHasValue(proj, "tag", tagFilter) {
			continue
		}
		result = append(result, summary)
	}
	return result, nil
}

func projectionText(proj *model.Projection) string {
	var b strings.Builder
	for _, part := range proj.Parts {
		if part == nil || part.Value == nil {
			continue
		}
		b.WriteString(fmt.Sprint(valueText(*part.Value)))
		b.WriteByte(' ')
	}
	return b.String()
}

func partHasValue(proj *model.Projection, path, want string) bool {
	partID, ok := proj.Paths[path]
	if !ok {
		return false
	}
	part := proj.Parts[partID]
	if part == nil || part.Value == nil {
		return false
	}
	if len(part.Value.List) > 0 {
		for _, value := range part.Value.List {
			if value == want {
				return true
			}
		}
	}
	return strings.EqualFold(part.Value.Text, want)
}

func matchesAllTokens(text, query string) bool {
	text = strings.ToLower(text)
	for _, token := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, token) {
			return false
		}
	}
	return true
}

func outputProjection(ticketID model.TicketID, proj *model.Projection, pathFilter []string, jsonMode bool, recordedAt, ticketRef string) {
	title, status := projectionTitleStatus(proj)
	if !jsonMode {
		fmt.Printf("%s  %s\n", ticketRef, title)
		fmt.Printf("status: %s\n", status)
		paths := make([]string, 0, len(proj.Paths))
		for path := range proj.Paths {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			if path == "title" || path == "status" {
				continue
			}
			if !pathMatches(path, pathFilter) {
				continue
			}
			part := proj.Parts[proj.Paths[path]]
			if part == nil {
				continue
			}
			value := valueOrNilFromPart(part)
			if value == nil {
				continue
			}
			fmt.Printf("%s: %v\n", path, value)
		}
		return
	}
	parts := make(map[string]showPart)
	for path, partID := range proj.Paths {
		if !pathMatches(path, pathFilter) {
			continue
		}
		part := proj.Parts[partID]
		if part == nil {
			continue
		}
		parts[path] = showPart{
			ID:        string(part.ID),
			Path:      path,
			Value:     valueOrNilFromPart(part),
			ValueKind: string(part.ValueKind),
			ParentID:  parentIDOrNil(part),
			CreatedBy: string(part.CreatedBy),
		}
	}
	writeJSON(showTicket{
		Ref:        ticketRef,
		ID:         string(ticketID),
		Title:      title,
		Status:     status,
		RecordedAt: recordedAt,
		Parts:      parts,
	})
}

func outputMarkdownProjection(ticketID model.TicketID, proj *model.Projection, pathFilter []string, ticketRef string, links []model.LinkView) {
	title, _ := projectionTitleStatus(proj)
	if title == "" {
		title = ticketRef
	}
	fmt.Printf("# %s\n\n", title)
	paths := make([]string, 0, len(proj.Paths))
	for path := range proj.Paths {
		if pathMatches(path, pathFilter) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if path == "title" || path == "status" {
			continue
		}
		part := proj.Parts[proj.Paths[path]]
		if part == nil {
			continue
		}
		depth := len(strings.Split(path, "/")) + 1
		if depth > 6 {
			depth = 6
		}
		heading := strings.Repeat("#", depth)
		last := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			last = path[idx+1:]
		}
		fmt.Printf("%s %s\n\n", heading, last)
		if part.Value != nil {
			fmt.Printf("%s\n\n", valueText(*part.Value))
		}
	}
	if len(links) > 0 {
		fmt.Printf("## Links\n\n")
		for _, link := range links {
			fmt.Printf("- %s %s %s\n", link.Relation, targetText(link.From), targetText(link.To))
		}
		fmt.Println()
	}
}

func projectionTitleStatus(proj *model.Projection) (string, string) {
	var title, status string
	if id, ok := proj.Paths["title"]; ok {
		if part := proj.Parts[id]; part != nil && part.Value != nil {
			title = part.Value.Text
		}
	}
	if id, ok := proj.Paths["status"]; ok {
		if part := proj.Parts[id]; part != nil && part.Value != nil {
			status = part.Value.Text
		}
	}
	return title, status
}

func ticketRecordedAt(db *store.Store, ticketID model.TicketID) string {
	events, err := db.LoadTicketEvents(ticketID)
	if err != nil || len(events) == 0 {
		return ""
	}
	var createdAt time.Time
	for _, event := range events {
		if event.Operation == model.OpCreateEntity {
			createdAt = event.RecordedAt
			break
		}
	}
	if createdAt.IsZero() {
		createdAt = events[0].RecordedAt
		for _, event := range events[1:] {
			if event.RecordedAt.Before(createdAt) {
				createdAt = event.RecordedAt
			}
		}
	}
	return createdAt.UTC().Format(time.RFC3339)
}

func pathMatches(path string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	filterKey := strings.Join(filter, "/")
	return path == filterKey || strings.HasPrefix(path, filterKey+"/")
}

func valueOrNil(value model.Value) any {
	if value.Kind == "" && value.Text == "" && value.Data == nil && value.Ref == nil {
		return nil
	}
	return valueText(value)
}

func valueOrNilFromPart(part *model.Part) any {
	if part == nil || part.Value == nil {
		return nil
	}
	if part.Value.Text == "" && part.Value.Data == nil && len(part.Value.List) == 0 {
		return nil
	}
	return valueText(*part.Value)
}

func valueText(value model.Value) any {
	if value.Text != "" {
		return value.Text
	}
	if len(value.List) > 0 {
		return value.List
	}
	if value.Data != nil {
		return value.Data
	}
	if value.Ref != nil {
		return value.Ref
	}
	return nil
}

func parentIDOrNil(part *model.Part) any {
	if part == nil || part.ParentID == nil {
		return nil
	}
	return string(*part.ParentID)
}

func filterHistory(events []model.Event, effectiveAt, knownAt time.Time, since, between string, partPath []string) []model.Event {
	var filtered []model.Event
	var sinceTime time.Time
	if since != "" {
		if parsed, err := parseTime(since); err == nil {
			sinceTime = parsed
		}
	}
	for _, event := range events {
		if event.EffectiveAt.After(effectiveAt) || event.RecordedAt.After(knownAt) {
			continue
		}
		if !sinceTime.IsZero() && event.RecordedAt.Before(sinceTime) {
			continue
		}
		if len(partPath) > 0 {
			if event.Target.Kind != model.KindPart || !pathEquals(event.Target.Path, partPath) {
				continue
			}
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func pathEquals(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func outputHistory(events []model.Event, jsonMode bool) {
	if jsonMode {
		items := make([]showEvent, 0, len(events))
		for _, event := range events {
			items = append(items, eventJSON(event))
		}
		writeJSON(map[string]any{"events": items})
		return
	}
	for _, event := range events {
		fmt.Printf("@e%d %s %s %s\n", event.AliasSeq, event.Operation, targetText(event.Target), valueText(event.Value))
	}
}

func outputEvent(event model.Event, jsonMode bool) {
	if jsonMode {
		writeJSON(eventJSON(event))
		return
	}
	fmt.Printf("@e%d %s %s %s\n", event.AliasSeq, event.Operation, targetText(event.Target), valueText(event.Value))
}

func eventJSON(event model.Event) showEvent {
	alias := ""
	if event.AliasSeq > 0 {
		alias = fmt.Sprintf("@e%d", event.AliasSeq)
	}
	return showEvent{
		ID:          string(event.ID),
		Alias:       alias,
		Sequence:    event.Sequence,
		Operation:   string(event.Operation),
		Target:      targetText(event.Target),
		Value:       valueText(event.Value),
		RecordedAt:  event.RecordedAt.UTC().Format(time.RFC3339),
		EffectiveAt: event.EffectiveAt.UTC().Format(time.RFC3339),
		Actor:       event.Actor.ID,
		Reason:      event.Reason,
	}
}

func targetText(ref model.Ref) string {
	if len(ref.Path) > 0 {
		return strings.Join(ref.Path, "/")
	}
	entity := ref.Entity
	if strings.HasPrefix(entity, string(ref.Kind)+":") {
		return entity
	}
	return string(ref.Kind) + ":" + entity
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

func mapStoreError(err error) int {
	if errors.Is(err, store.ErrConflict) {
		return exitConflict
	}
	return exitValidation
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func sortStrings(values []string) {
	sort.Strings(values)
}
