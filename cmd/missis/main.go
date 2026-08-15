package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/ravinsharma7/missis/implementation/model"
	"github.com/ravinsharma7/missis/implementation/store"
)

const (
	exitSuccess     = 0
	exitInvalid     = 2
	exitNotFound    = 3
	exitValidation  = 4
	exitConflict    = 5
	exitStorage     = 8
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
	Ref        string                 `json:"ref"`
	ID         string                 `json:"id"`
	Title      string                 `json:"title"`
	Status     string                 `json:"status"`
	RecordedAt string                 `json:"recorded_at"`
	Parts      map[string]showPart    `json:"parts"`
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
	Error               string   `json:"error"`
	Target              *string  `json:"target"`
	Message             string   `json:"message"`
	Ontology            *string  `json:"ontology"`
	MissingObligations []string `json:"missing_obligations"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitInvalid)
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
	fmt.Fprintln(os.Stderr, "usage: missis new|show|set ...")
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
	if err := fs.Parse(args); err != nil {
		return exitInvalid
	}

	title := fs.Arg(0)
	storePath := resolveStorePath(storeFlag)
	db, err := store.Open(storePath)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	defer db.Close()

	recordedAt := time.Now().UTC()
	effectiveTime := recordedAt
	if effectiveAt != "" {
		effectiveTime, err = parseTime(effectiveAt)
		if err != nil {
			printError(err, exitInvalid, jsonMode, nil)
			return exitInvalid
		}
	}

	ticketID := model.TicketID(newID("ticket"))
	batchID := model.BatchID(newID("batch"))
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	actorRef := parseActor(actor)
	events := []model.Event{
		newEvent(stream, model.OpCreateEntity, model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, model.Value{}, actorRef, recordedAt, effectiveTime, batchID, ""),
		partEvent(stream, "title", title, model.ValueKindText, actorRef, recordedAt, effectiveTime, batchID),
		partEvent(stream, "status", "open", model.ValueKindStatus, actorRef, recordedAt, effectiveTime, batchID),
	}
	if priority != "" {
		events = append(events, partEvent(stream, "priority", priority, model.ValueKindPriority, actorRef, recordedAt, effectiveTime, batchID))
	}
	if len(types) > 0 {
		events = append(events, partEvent(stream, "type", []string(types), model.ValueKindList, actorRef, recordedAt, effectiveTime, batchID))
	}
	if len(tags) > 0 {
		events = append(events, partEvent(stream, "tag", []string(tags), model.ValueKindList, actorRef, recordedAt, effectiveTime, batchID))
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
		"direction": true, "depth": true, "relations": true,
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
	if err := fs.Parse(args); err != nil {
		return exitInvalid
	}

	ref := fs.Arg(0)
	storePath := resolveStorePath(storeFlag)
	db, err := store.Open(storePath)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	defer db.Close()

	if health {
		if err := db.CheckConsistency(); err != nil {
			if jsonMode {
				writeJSON(errorResult{Error: "storage_failure", Target: nil, Message: err.Error(), Ontology: nil, MissingObligations: []string{}})
			} else {
				fmt.Fprintf(os.Stderr, "missis: consistency failure: %s\n", err.Error())
			}
			return exitStorage
		}
		if jsonMode {
			writeJSON(map[string]any{"status": "ok"})
		} else {
			fmt.Println("ok")
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
	outputProjection(ticketID, proj, partPath, jsonMode, recordedAtText, ticketRef)
	return exitSuccess
}

func runSet(args []string) int {
	args = reorderArgs(args, map[string]bool{
		"actor": true, "effective-at": true, "reason": true, "name": true,
		"parent": true, "supersedes": true, "because": true,
		"if-current": true, "idempotency-key": true, "store": true,
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
	if err := fs.Parse(args); err != nil {
		return exitInvalid
	}

	if fs.NArg() < 1 {
		return exitInvalid
	}
	ref := fs.Arg(0)
	value := fs.Arg(1)

	storePath := resolveStorePath(storeFlag)
	db, err := store.Open(storePath)
	if err != nil {
		printError(err, exitStorage, jsonMode, nil)
		return exitStorage
	}
	defer db.Close()

	recordedAt := time.Now().UTC()
	effectiveTime := recordedAt
	if effectiveAt != "" {
		effectiveTime, err = parseTime(effectiveAt)
		if err != nil {
			printError(err, exitInvalid, jsonMode, &ref)
			return exitInvalid
		}
	}

	baseTicketID, basePath, err := resolveTicketRef(db, ref, effectiveTime)
	if err == nil && len(basePath) > 0 && basePath[len(basePath)-1] == "links" && (add || retract) {
		return runSetLink(db, ref, baseTicketID, basePath, value, actor, reason, effectiveTime, recordedAt, add, retract, idemKey, jsonMode)
	}

	actorRef := parseActor(actor)
	batchID := model.BatchID(newID("batch"))

	requiresExisting := retract || name != "" || parent != ""
	var (
		ticketID        model.TicketID
		partID          model.PartID
		currentPath     []string
		creationEvents  []model.Event
		partExisted     bool
		stream          model.Ref
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
			TargetEntity:        string(partID),
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
		batchID := model.BatchID(newID("batch"))
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

func newEvent(stream model.Ref, operation model.Operation, target model.Ref, value model.Value, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID, reason string) model.Event {
	return model.Event{
		ID:          model.EventID(newID("event")),
		Stream:      stream,
		Operation:   operation,
		Target:      target,
		Value:       value,
		RecordedAt:  recordedAt,
		EffectiveAt: effectiveAt,
		Actor:       actor,
		BatchID:     &batchID,
		Reason:      reason,
	}
}

func partEvent(stream model.Ref, path string, value any, kind model.ValueKind, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID) model.Event {
	partID := model.PartID(newID("part"))
	target := model.Ref{Kind: model.KindPart, Entity: string(partID), Path: []string{path}}
	var valueModel model.Value
	switch typed := value.(type) {
	case string:
		valueModel = model.Value{Kind: kind, Text: typed}
	case []string:
		if kind == model.ValueKindList {
			valueModel = model.Value{Kind: kind, List: typed}
		} else {
			valueModel = model.Value{Kind: kind, Data: typed}
		}
	default:
		valueModel = model.Value{Kind: kind, Data: value}
	}
	return model.Event{
		ID:          model.EventID(newID("event")),
		Stream:      stream,
		Operation:   model.OpCreatePart,
		Target:      target,
		Value:       valueModel,
		RecordedAt:  recordedAt,
		EffectiveAt: effectiveAt,
		Actor:       actor,
		BatchID:     &batchID,
	}
}

func resolveStorePath(storeFlag string) string {
	if storeFlag != "" {
		return storeFlag
	}
	if marker, ok := findMissisMarker(); ok {
		return marker
	}
	if env := os.Getenv("MISSIS_STORE"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".missis", "missis.db")
	}
	return filepath.Join(home, ".local", "share", "missis", "missis.db")
}

func findMissisMarker() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		marker := filepath.Join(dir, ".missis")
		info, statErr := os.Stat(marker)
		if statErr == nil {
			if info.IsDir() {
				return filepath.Join(marker, "missis.db"), true
			}
			data, readErr := os.ReadFile(marker)
			if readErr == nil {
				line := strings.TrimSpace(string(data))
				if line == "" {
					return filepath.Join(dir, ".missis", "missis.db"), true
				}
				if filepath.IsAbs(line) {
					return line, true
				}
				return filepath.Join(dir, line), true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
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

func newID(prefix string) string {
	return prefix + ":" + ulid.Make().String()
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
		parentID      *model.PartID
		events        []model.Event
		partID        model.PartID
		existed       = true
		currentPath   []string
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
		newIDValue := model.PartID(newID("part"))
		target := model.Ref{Kind: model.KindPart, Entity: string(newIDValue), Path: append([]string(nil), currentPath...)}
		var parentRef *model.Ref
		if parentID != nil {
			parentRef = &model.Ref{Kind: model.KindPart, Entity: string(*parentID)}
		}
		event := model.Event{
			ID:          model.EventID(newID("event")),
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
	for _, summary := range summaries {
		fmt.Printf("%s\t%s\t%s\t%s\n", summary.Ref, summary.Status, summary.Title, summary.RecordedAt.UTC().Format(time.RFC3339))
	}
}

func outputProjection(ticketID model.TicketID, proj *model.Projection, pathFilter []string, jsonMode bool, recordedAt, ticketRef string) {
	title, status := projectionTitleStatus(proj)
	if !jsonMode {
		fmt.Printf("%s  %s\n", ticketRef, title)
		fmt.Printf("status: %s\n", status)
		for path := range proj.Paths {
			if !pathMatches(path, pathFilter) {
				continue
			}
			part := proj.Parts[proj.Paths[path]]
			if part != nil && part.Value != nil {
				fmt.Printf("%s: %s\n", path, valueText(*part.Value))
			}
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
			Error:   errorCode(code),
			Target:  target,
			Message: err.Error(),
			Ontology: nil,
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
