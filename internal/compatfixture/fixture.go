// Package compatfixture builds the immutable cross-platform store corpus used
// to detect accidental durable-format changes.
package compatfixture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/externalref"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/plugin"
	"github.com/ravinsharma7/missis/internal/plugin/builtin"
	"github.com/ravinsharma7/missis/internal/store"
)

const RevisionDirectory = "revision-0006"

type ArtifactExpectation struct {
	Ref       string `json:"ref"`
	Digest    string `json:"digest"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
}

type Manifest struct {
	StoreFormatRevision int                   `json:"store_format_revision"`
	SchemaVersion       string                `json:"schema_version"`
	EventCount          int64                 `json:"event_count"`
	HeadHash            string                `json:"head_hash"`
	ProjectionSHA256    string                `json:"projection_sha256"`
	Operations          []string              `json:"operations"`
	OperationVersions   []string              `json:"operation_versions"`
	ValueKinds          []string              `json:"value_kinds"`
	InlineKinds         []string              `json:"inline_kinds"`
	RefKinds            []string              `json:"ref_kinds"`
	Relations           []string              `json:"relations"`
	Plugins             []string              `json:"plugins"`
	PluginContracts     []string              `json:"plugin_contracts"`
	SchemaDeclarations  []string              `json:"schema_declarations"`
	OrderedContainment  bool                  `json:"ordered_containment"`
	LegacyContainment   bool                  `json:"legacy_unordered_containment"`
	IdempotencyKeys     []string              `json:"idempotency_keys"`
	Artifacts           []ArtifactExpectation `json:"artifacts"`
}

type fixedIDs struct{ counts map[string]int }

func (f *fixedIDs) New(prefix string) string {
	if f.counts == nil {
		f.counts = make(map[string]int)
	}
	f.counts[prefix]++
	return fmt.Sprintf("%s:plugin-fixture-%04d", prefix, f.counts[prefix])
}

// Build creates a complete current-revision store and artifact CAS under root.
// root must not already contain a fixture database.
func Build(ctx context.Context, root string) (Manifest, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Manifest{}, err
	}
	dbPath := filepath.Join(root, "fixture.db")
	if _, err := os.Stat(dbPath); err == nil {
		return Manifest{}, fmt.Errorf("fixture database already exists: %s", dbPath)
	} else if !os.IsNotExist(err) {
		return Manifest{}, err
	}
	artifactRoot := filepath.Join(root, "artifacts")
	cas, err := artifact.NewLocalStore(artifactRoot)
	if err != nil {
		return Manifest{}, err
	}
	s, err := store.Open(dbPath)
	if err != nil {
		return Manifest{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = s.Close()
		}
	}()

	base := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	contents := []struct {
		name, mediaType, body string
	}{
		{"markdown", "text/markdown", "# Fixture\n\n## Plugin prose\n\n![inert](https://example.invalid/image.png)\n"},
		{"image", "image/png", "fixture-image-bytes"},
		{"audio", "audio/mpeg", "fixture-audio-bytes"},
		{"video", "video/mp4", "fixture-video-bytes"},
		{"generic", "application/octet-stream", "fixture-generic-bytes"},
	}
	metadata := make(map[string]artifact.Metadata, len(contents))
	for i, item := range contents {
		meta, err := cas.Put(ctx, strings.NewReader(item.body), item.mediaType)
		if err != nil {
			return Manifest{}, err
		}
		metadata[item.name] = meta
		if err := s.RecordArtifact(ctx, store.ArtifactRecord{
			Ref: meta.Ref.String(), Algorithm: meta.Algorithm, Digest: meta.Digest,
			MediaType: meta.MediaType, Size: meta.Size, Backend: "local",
			RecordedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			return Manifest{}, err
		}
	}

	ids := &fixedIDs{}
	stream := model.Ref{Kind: model.KindTicket, Entity: "ticket:compatibility"}
	actor := model.ActorRef{Kind: "user", ID: "actor:fixture", Name: "Compatibility Fixture"}
	batch := model.BatchID("batch:fixture-base")
	var events []model.Event
	var tick int
	add := func(operation model.Operation, target model.Ref, value model.Value) model.EventID {
		tick++
		id := model.EventID(fmt.Sprintf("event:fixture-%04d", tick))
		at := base.Add(time.Duration(tick) * time.Second)
		events = append(events, model.Event{
			ID: id, Stream: stream, BatchID: &batch, Operation: operation,
			Target: target, Value: value, RecordedAt: at, EffectiveAt: at, Actor: actor,
		})
		return id
	}
	add(model.OpCreateEntity, stream, model.Value{})
	rootPart := model.Ref{Kind: model.KindPart, Entity: "part:fixture-root", Path: []string{"fixture"}}
	add(model.OpCreatePart, rootPart, model.Value{Kind: model.ValueKindMarkdown, Text: "Fixture root", OrderKey: "00000000000001000000"})
	legacyPart := model.Ref{Kind: model.KindPart, Entity: "part:fixture-legacy", Path: []string{"legacy"}}
	add(model.OpCreatePart, legacyPart, model.Value{Kind: model.ValueKindText, Text: "legacy unordered", Ref: &rootPart})

	values := valueSamples(metadata)
	for i, kind := range model.AllBuiltInValueKinds() {
		value := values[kind]
		if err := model.ValidateBuiltInValue(value); err != nil {
			return Manifest{}, fmt.Errorf("fixture value %s: %w", kind, err)
		}
		part := model.Ref{Kind: model.KindPart, Entity: fmt.Sprintf("part:value-%02d", i+1), Path: []string{"values", string(kind)}}
		value.OrderKey = fmt.Sprintf("%020d", (i+2)*100000)
		id := add(model.OpCreatePart, part, value)
		if kind == model.ValueKindCodeRef {
			line1, line2 := 10, 20
			events[len(events)-1].Sources = []model.SourceRef{{
				Ref:       model.Ref{Kind: model.KindArtifact, Entity: metadata["markdown"].Ref.String()},
				MediaType: "text/markdown", Span: &model.Span{StartLine: &line1, EndLine: &line2},
			}}
			events[len(events)-1].Inputs = []model.Ref{
				{Kind: model.KindEvent, Entity: string(id)}, {Kind: model.KindRun, Entity: "run:fixture"},
				{Kind: model.KindCode, Entity: "code:fixture"}, {Kind: model.KindGit, Entity: "git:fixture"},
			}
			events[len(events)-1].Causes = []model.Ref{{Kind: model.KindProject, Entity: "project:fixture"}}
			events[len(events)-1].Effects = []model.Effect{{
				Kind: "created", Ref: &model.Ref{Kind: model.KindArtifact, Entity: metadata["generic"].Ref.String()},
				ObservedAt: ptr(base), Evidence: []model.Ref{{Kind: model.KindPart, Entity: rootPart.Entity}},
			}}
			events[len(events)-1].Reason = "exercise complete durable provenance"
			events[len(events)-1].Ontologies = []model.OntologyRef{{ID: "ontology:fixture", Version: "1", Hash: strings.Repeat("a", 64)}}
		}
	}

	opsPart := model.Ref{Kind: model.KindPart, Entity: "part:operations", Path: []string{"operations"}}
	add(model.OpCreatePart, opsPart, model.Value{Kind: model.ValueKindList, List: []string{"initial"}})
	setID := add(model.OpSetValue, opsPart, model.Value{Kind: model.ValueKindText, Text: "set"})
	add(model.OpAddValue, opsPart, model.Value{Text: "added"})
	add(model.OpRetractValue, opsPart, model.Value{})
	add(model.OpRenamePart, opsPart, model.Value{Text: "operations-renamed"})
	add(model.OpMovePart, opsPart, model.Value{Ref: &rootPart, OrderKey: "00000000000002000000"})
	add(model.OpAttachChild, opsPart, model.Value{Ref: &rootPart, OrderKey: "00000000000002500000"})
	add(model.OpDetachChild, opsPart, model.Value{OrderKey: "00000000000003000000"})
	temporary := model.Ref{Kind: model.KindPart, Entity: "part:temporary", Path: []string{"temporary"}}
	add(model.OpCreatePart, temporary, model.Value{Kind: model.ValueKindText, Text: "temporary"})
	add(model.OpRetractSubtree, temporary, model.Value{})
	add(model.OpRestorePart, temporary, model.Value{Kind: model.ValueKindText, Text: "restored"})
	project := model.Ref{Kind: model.KindProject, Entity: "project:fixture"}
	add(model.OpAssignOntology, opsPart, model.Value{Ref: &project})
	add(model.OpRemoveOntology, opsPart, model.Value{Ref: &project})
	group := model.Ref{Kind: model.KindGroup, Entity: "group:fixture"}
	add(model.OpJoinScope, stream, model.Value{Text: model.RelationMemberOf, Ref: &group})
	add(model.OpLeaveScope, stream, model.Value{Text: model.RelationMemberOf, Ref: &group})
	backdated := add(model.OpObserveEffect, opsPart, model.Value{Text: "backdated observation"})
	events[len(events)-1].EffectiveAt = base.Add(-time.Hour)
	events[len(events)-1].Effects = []model.Effect{{Kind: "observed", ObservedAt: ptr(base.Add(-time.Hour))}}
	add(model.OpAttachEvidence, opsPart, model.Value{Ref: &model.Ref{Kind: model.KindArtifact, Entity: metadata["generic"].Ref.String()}})
	events[len(events)-1].Sources = []model.SourceRef{{Ref: model.Ref{Kind: model.KindArtifact, Entity: metadata["generic"].Ref.String()}, MediaType: "application/octet-stream"}}
	add(model.OpRecordVerification, opsPart, model.Value{Text: "verified"})
	supersedeID := add(model.OpSupersedeEvent, opsPart, model.Value{Kind: model.ValueKindText, Text: "superseding value"})
	events[len(events)-1].Supersedes = []model.EventID{setID, backdated}
	events[len(events)-1].Invocation = &model.InvocationRef{
		ID: "run:custom-plugin", Plugin: "fixture/custom-kind", Version: "1.0.0",
		CodeHash: strings.Repeat("b", 64), RequestedBy: &actor,
	}
	_ = supersedeID
	custom := model.Ref{Kind: model.KindPart, Entity: "part:plugin-card", Path: []string{"plugin", "card"}}
	add(model.OpCreatePart, custom, model.Value{Kind: model.ValueKind("plugin/card"), Text: "custom plugin value"})
	events[len(events)-1].Invocation = &model.InvocationRef{
		ID: "run:custom-kind", Plugin: "fixture/custom-kind", Version: "1.0.0",
		CodeHash: strings.Repeat("b", 64), RequestedBy: &actor,
	}

	for i, relation := range model.ValidRelations() {
		to := model.Ref{Kind: model.KindTicket, Entity: fmt.Sprintf("ticket:relation-%02d", i+1)}
		add(model.OpAssertLink, rootPart, model.Value{Text: relation, Ref: &to})
		if relation == "related" {
			add(model.OpRetractLink, rootPart, model.Value{Text: relation, Ref: &to})
		}
	}

	if _, _, err := s.AppendTicketBatch(events, "fixture/base", map[string]any{"fixture": true}); err != nil {
		return Manifest{}, fmt.Errorf("append base fixture: %w", err)
	}
	schemaStream := model.Ref{Kind: model.KindProject, Entity: "project:fixture-schema"}
	schemaBatch := model.BatchID("batch:fixture-schema")
	schemaEvents := []model.Event{
		{ID: "event:fixture-schema-0001", Stream: schemaStream, BatchID: &schemaBatch, Operation: model.OpCreateEntity, Target: schemaStream, RecordedAt: base.Add(2 * time.Hour), EffectiveAt: base.Add(2 * time.Hour), Actor: actor},
		{ID: "event:fixture-schema-0002", Stream: schemaStream, BatchID: &schemaBatch, Operation: model.OpCreatePart,
			Target: model.Ref{Kind: model.KindPart, Entity: "part:schema-plugin-card", Path: []string{"schema", "plugin-card"}},
			Value:  model.Value{Kind: model.ValueKindText, Text: "plugin/card"}, RecordedAt: base.Add(2*time.Hour + time.Second), EffectiveAt: base.Add(2*time.Hour + time.Second), Actor: actor},
	}
	if _, err := s.AppendBatch(schemaEvents, "fixture/schema", nil, map[string]any{"schema": true}); err != nil {
		return Manifest{}, fmt.Errorf("append schema fixture: %w", err)
	}

	registry := plugin.NewIngestionRegistry()
	if err := registry.Register(plugin.IngestionRegistration{
		Manifest: builtin.MarkdownManifest(), ID: "markdown",
		Selector: plugin.IngestSelector{Operation: "import-markdown", MediaType: "text/markdown", TargetKind: model.KindTicket},
		Plugin:   builtin.NewMarkdownImporter(),
	}); err != nil {
		return Manifest{}, err
	}
	if err := registry.Register(plugin.IngestionRegistration{
		Manifest: builtin.ArtifactManifest(), ID: "attach",
		Selector: plugin.IngestSelector{Operation: "attach-artifact"}, Plugin: builtin.NewArtifactAttacher(),
	}); err != nil {
		return Manifest{}, err
	}
	openArtifact := func(meta artifact.Metadata) func(context.Context) (io.ReadCloser, error) {
		return func(openCtx context.Context) (io.ReadCloser, error) { return cas.Open(openCtx, meta.Ref) }
	}
	pluginActor := model.ActorRef{Kind: "user", ID: "actor:plugin-request"}
	markdownInput := plugin.IngestInput{
		Request:  plugin.IngestRequest{Operation: "import-markdown", Target: model.Ref{Kind: model.KindTicket, Entity: "ticket:plugin-markdown"}, MediaType: "text/markdown", SourceName: "fixture.md"},
		Artifact: metadata["markdown"], Open: openArtifact(metadata["markdown"]),
		Invocation: model.InvocationRef{ID: "run:markdown-fixture"}, Actor: pluginActor, RequestedBy: pluginActor,
		RecordedAt: base.Add(3 * time.Hour), EffectiveAt: base.Add(3 * time.Hour), BatchID: "batch:plugin-markdown", NewID: ids.New,
	}
	markdownProposal, _, err := registry.Run(ctx, markdownInput)
	if err != nil {
		return Manifest{}, err
	}
	if _, err := s.AppendBatch(markdownProposal.Events, "fixture/plugin-markdown", nil, markdownProposal.Result); err != nil {
		return Manifest{}, fmt.Errorf("append Markdown plugin fixture: %w", err)
	}
	artifactInput := plugin.IngestInput{
		Request:  plugin.IngestRequest{Operation: "attach-artifact", Target: model.Ref{Kind: model.KindTicket, Entity: "ticket:plugin-artifact"}, SourceName: "image.png"},
		Artifact: metadata["image"], Open: openArtifact(metadata["image"]),
		Invocation: model.InvocationRef{ID: "run:artifact-fixture"}, Actor: pluginActor, RequestedBy: pluginActor,
		RecordedAt: base.Add(4 * time.Hour), EffectiveAt: base.Add(4 * time.Hour), BatchID: "batch:plugin-artifact", NewID: ids.New,
	}
	artifactProposal, _, err := registry.Run(ctx, artifactInput)
	if err != nil {
		return Manifest{}, err
	}
	if _, err := s.AppendBatch(artifactProposal.Events, "fixture/plugin-artifact", nil, artifactProposal.Result); err != nil {
		return Manifest{}, fmt.Errorf("append artifact plugin fixture: %w", err)
	}

	manifest, err := Inspect(ctx, s)
	if err != nil {
		return Manifest{}, err
	}
	if err := s.Close(); err != nil {
		return Manifest{}, err
	}
	closed = true
	// Advisory lock ownership ended with Close. The persistent lock filename is
	// operational state, not part of the portable fixture corpus.
	_ = os.Remove(dbPath + ".maintenance.lock")
	if err := WriteManifest(filepath.Join(root, "manifest.json"), manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func valueSamples(metadata map[string]artifact.Metadata) map[model.ValueKind]model.Value {
	commit := "0123456789abcdef0123456789abcdef01234567"
	branch := "main"
	lineStart, lineEnd, byteStart, byteEnd := 2, 8, 0, 128
	symbol := "Fixture"
	artifactRef := func(name string) model.Ref {
		return model.Ref{Kind: model.KindArtifact, Entity: metadata[name].Ref.String()}
	}
	media := func(kind model.ValueKind, name string) model.MediaDescriptor {
		return model.MediaDescriptor{Kind: kind, URI: metadata[name].Ref.String(), MediaType: metadata[name].MediaType, Alt: "fixture " + name}
	}
	inline := model.InlineSequence{Items: []model.InlineItem{
		{ID: "inline-markdown", Kind: model.InlineMarkdownText, Text: "Prose"},
		{ID: "inline-code", Kind: model.InlineCodeRef, Data: model.CodeRef{Repository: "https://example.invalid/repo.git", Commit: commit, Path: "fixture.go"}},
		{ID: "inline-git", Kind: model.InlineGitRef, Data: model.GitRef{Repository: "https://example.invalid/repo.git", Branch: &branch}},
		{ID: "inline-artifact", Kind: model.InlineArtifact, Data: model.ArtifactDescriptor{Ref: artifactRef("generic"), MediaType: metadata["generic"].MediaType, Size: metadata["generic"].Size}},
		{ID: "inline-image", Kind: model.InlineImage, Data: media(model.ValueKindImage, "image")},
		{ID: "inline-audio", Kind: model.InlineAudio, Data: media(model.ValueKindAudio, "audio")},
		{ID: "inline-video", Kind: model.InlineVideo, Data: media(model.ValueKindVideo, "video")},
		{ID: "inline-raw", Kind: model.InlineRawMarkdown, Text: "![inert](https://example.invalid/raw.png)"},
	}}
	return map[model.ValueKind]model.Value{
		model.ValueKindText:         {Kind: model.ValueKindText, Text: "plain text"},
		model.ValueKindMarkdown:     {Kind: model.ValueKindMarkdown, Text: "**Markdown** with [inert URL](https://example.invalid/)"},
		model.ValueKindScalar:       {Kind: model.ValueKindScalar, Text: "42"},
		model.ValueKindStatus:       {Kind: model.ValueKindStatus, Text: "open"},
		model.ValueKindPriority:     {Kind: model.ValueKindPriority, Text: "high"},
		model.ValueKindMap:          {Kind: model.ValueKindMap, Data: map[string]any{"alpha": "one", "count": float64(2)}},
		model.ValueKindList:         {Kind: model.ValueKindList, List: []string{"one", "two"}},
		model.ValueKindRef:          {Kind: model.ValueKindRef, Ref: &model.Ref{Kind: model.KindPart, Entity: "part:fixture-root", Path: []string{"fixture"}}},
		model.ValueKindCodeRef:      {Kind: model.ValueKindCodeRef, Data: model.CodeRef{Repository: "https://example.invalid/repo.git", Commit: commit, Path: "fixture.go", StartLine: &lineStart, EndLine: &lineEnd, StartByte: &byteStart, EndByte: &byteEnd, Symbol: &symbol}},
		model.ValueKindGitRef:       {Kind: model.ValueKindGitRef, Data: model.GitRef{Repository: "https://example.invalid/repo.git", Branch: &branch}},
		model.ValueKindEvidence:     {Kind: model.ValueKindEvidence, Text: "evidence", Ref: &model.Ref{Kind: model.KindArtifact, Entity: metadata["generic"].Ref.String()}},
		model.ValueKindVerification: {Kind: model.ValueKindVerification, Text: "passed"},
		model.ValueKindJSON:         {Kind: model.ValueKindJSON, Data: map[string]any{"safe": true, "url": "https://example.invalid/inert"}},
		model.ValueKindArtifact:     {Kind: model.ValueKindArtifact, Data: model.ArtifactDescriptor{Ref: artifactRef("generic"), MediaType: metadata["generic"].MediaType, Size: metadata["generic"].Size}},
		model.ValueKindExternalRef: {Kind: model.ValueKindExternalRef, Data: externalref.ReferenceV1{
			Version: externalref.VersionV1, StoreID: "store:v1:sha256:120311f35ac84b69682c5b5be1dbe7ab96994ef4a8db9d43473d8d0f1f379867",
			Namespace: "missis", Kind: "ticket", EntityID: "ticket:external-fixture", SubentityID: "part:problem",
			Observation: &externalref.ObservedV1{CurrentEventID: "event:external-observed"}, DisplayHint: "external fixture",
		}},
		model.ValueKindAnnotation:     {Kind: model.ValueKindAnnotation, Text: "generated annotation"},
		model.ValueKindImage:          {Kind: model.ValueKindImage, Data: media(model.ValueKindImage, "image")},
		model.ValueKindVideo:          {Kind: model.ValueKindVideo, Data: media(model.ValueKindVideo, "video")},
		model.ValueKindAudio:          {Kind: model.ValueKindAudio, Data: media(model.ValueKindAudio, "audio")},
		model.ValueKindEmbed:          {Kind: model.ValueKindEmbed, Data: model.MediaDescriptor{Kind: model.ValueKindEmbed, URI: "https://example.invalid/embed", MediaType: "text/html"}},
		model.ValueKindInlineSequence: {Kind: model.ValueKindInlineSequence, Data: inline},
	}
}

// Inspect derives the logical compatibility manifest from a store.
func Inspect(ctx context.Context, s *store.Store) (Manifest, error) {
	events, err := s.LoadEventsContext(ctx)
	if err != nil {
		return Manifest{}, err
	}
	records, err := s.ListArtifacts(ctx)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{}
	manifest.StoreFormatRevision, err = s.FormatRevisionContext(ctx)
	if err != nil {
		return Manifest{}, err
	}
	manifest.SchemaVersion, err = s.SchemaVersionContext(ctx)
	if err != nil {
		return Manifest{}, err
	}
	manifest.EventCount, err = s.EventCountContext(ctx)
	if err != nil {
		return Manifest{}, err
	}
	manifest.HeadHash, err = s.HeadHashContext(ctx)
	if err != nil {
		return Manifest{}, err
	}
	sets := newCoverageSets()
	streams := make(map[string]model.Ref)
	for _, event := range events {
		sets.observeEvent(event)
		streams[string(event.Stream.Kind)+":"+event.Stream.Entity] = event.Stream
	}
	manifest.Operations = sortedSet(sets.operations)
	manifest.OperationVersions = sortedSet(sets.operationVersions)
	manifest.ValueKinds = sortedSet(sets.valueKinds)
	manifest.InlineKinds = sortedSet(sets.inlineKinds)
	manifest.RefKinds = sortedSet(sets.refKinds)
	manifest.Relations = sortedSet(sets.relations)
	manifest.Plugins = sortedSet(sets.plugins)
	manifest.PluginContracts = sortedSet(sets.pluginContracts)
	manifest.SchemaDeclarations = sortedSet(sets.schemaDeclarations)
	manifest.OrderedContainment = sets.orderedContainment
	manifest.LegacyContainment = sets.legacyContainment
	for _, key := range []string{"fixture/base", "fixture/schema", "fixture/plugin-markdown", "fixture/plugin-artifact"} {
		var result any
		found, err := s.LookupIdempotencyContext(ctx, key, &result)
		if err != nil {
			return Manifest{}, err
		}
		if found {
			manifest.IdempotencyKeys = append(manifest.IdempotencyKeys, key)
		}
	}
	for _, record := range records {
		manifest.Artifacts = append(manifest.Artifacts, ArtifactExpectation{Ref: record.Ref, Digest: record.Digest, MediaType: record.MediaType, Size: record.Size})
	}
	sort.Slice(manifest.Artifacts, func(i, j int) bool { return manifest.Artifacts[i].Ref < manifest.Artifacts[j].Ref })

	keys := make([]string, 0, len(streams))
	for key := range streams {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var projections []any
	for _, key := range keys {
		stream := streams[key]
		projection, err := s.CurrentStreamProjectionContext(ctx, stream, maxEffectiveAt(events))
		if err != nil {
			return Manifest{}, err
		}
		projections = append(projections, projection)
	}
	raw, err := json.Marshal(projections)
	if err != nil {
		return Manifest{}, err
	}
	sum := sha256.Sum256(raw)
	manifest.ProjectionSHA256 = hex.EncodeToString(sum[:])
	return manifest, nil
}

func WriteManifest(path string, manifest Manifest) error {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func ReadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	err = json.Unmarshal(raw, &manifest)
	return manifest, err
}

func EqualManifest(a, b Manifest) bool {
	aRaw, _ := json.Marshal(a)
	bRaw, _ := json.Marshal(b)
	return bytes.Equal(aRaw, bRaw)
}

type coverageSets struct {
	operations, operationVersions, valueKinds, inlineKinds, refKinds, relations, plugins, pluginContracts, schemaDeclarations map[string]struct{}
	orderedContainment, legacyContainment                                                                                     bool
}

func newCoverageSets() *coverageSets {
	return &coverageSets{
		operations: map[string]struct{}{}, operationVersions: map[string]struct{}{}, valueKinds: map[string]struct{}{}, inlineKinds: map[string]struct{}{},
		refKinds: map[string]struct{}{}, relations: map[string]struct{}{}, plugins: map[string]struct{}{}, pluginContracts: map[string]struct{}{}, schemaDeclarations: map[string]struct{}{},
	}
}

func (s *coverageSets) observeEvent(event model.Event) {
	s.operations[string(event.Operation)] = struct{}{}
	if descriptor, ok := model.LookupOperation(event.Operation); ok {
		s.operationVersions[fmt.Sprintf("%s/v%d", event.Operation, descriptor.Version)] = struct{}{}
	}
	s.observeRef(event.Stream)
	s.observeRef(event.Target)
	s.observeValue(event.Value)
	for _, source := range event.Sources {
		s.observeRef(source.Ref)
	}
	for _, refs := range [][]model.Ref{event.Inputs, event.Causes} {
		for _, ref := range refs {
			s.observeRef(ref)
		}
	}
	for _, effect := range event.Effects {
		if effect.Ref != nil {
			s.observeRef(*effect.Ref)
		}
		for _, ref := range effect.Evidence {
			s.observeRef(ref)
		}
	}
	if event.Invocation != nil && event.Invocation.Plugin != "" {
		s.plugins[event.Invocation.Plugin] = struct{}{}
		s.pluginContracts[fmt.Sprintf("%s@%s#%s", event.Invocation.Plugin, event.Invocation.Version, event.Invocation.CodeHash)] = struct{}{}
	}
	if event.Operation == model.OpAssertLink || event.Operation == model.OpRetractLink || event.Operation == model.OpJoinScope || event.Operation == model.OpLeaveScope {
		s.relations[event.Value.Text] = struct{}{}
	}
	if (event.Stream.Kind == model.KindProject || event.Stream.Kind == model.KindGroup) && len(event.Target.Path) > 1 && event.Target.Path[0] == "schema" {
		s.schemaDeclarations[strings.Join(event.Target.Path, "/")+"="+event.Value.Text] = struct{}{}
	}
	if event.Target.Kind == model.KindPart && event.Value.Ref != nil && event.Value.Ref.Kind == model.KindPart {
		if event.Value.OrderKey == "" {
			s.legacyContainment = true
		} else {
			s.orderedContainment = true
		}
	}
}

func (s *coverageSets) observeValue(value model.Value) {
	if value.Kind != "" {
		s.valueKinds[string(value.Kind)] = struct{}{}
	}
	if value.Ref != nil {
		s.observeRef(*value.Ref)
	}
	switch data := value.Data.(type) {
	case model.ArtifactDescriptor:
		s.observeRef(data.Ref)
	case model.InlineSequence:
		for _, item := range data.Items {
			s.inlineKinds[string(item.Kind)] = struct{}{}
			if item.Ref != nil {
				s.observeRef(*item.Ref)
			}
			if item.Source != nil {
				s.observeRef(item.Source.Ref)
			}
			switch typed := item.Data.(type) {
			case model.ArtifactDescriptor:
				s.observeRef(typed.Ref)
			}
		}
	}
}

func (s *coverageSets) observeRef(ref model.Ref) {
	if ref.Kind != "" {
		s.refKinds[string(ref.Kind)] = struct{}{}
	}
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func ptr[T any](value T) *T { return &value }

func maxEffectiveAt(events []model.Event) time.Time {
	var result time.Time
	for _, event := range events {
		if event.EffectiveAt.After(result) {
			result = event.EffectiveAt
		}
	}
	return result
}
