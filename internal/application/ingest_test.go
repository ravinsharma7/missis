package application

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/plugin"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestConcurrentMarkdownImportsAcrossClientsAndTickets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missis.db")
	now := fixedNow()
	svc1, err := OpenPathWithClock(path, fixedClock{now})
	if err != nil {
		t.Fatal(err)
	}
	svc2, err := OpenPathWithClock(path, fixedClock{now})
	if err != nil {
		_ = svc1.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = svc1.Close()
		_ = svc2.Close()
	})
	clients := []*missis.Client{missis.NewClient(svc1), missis.NewClient(svc2)}
	const jobs = 8
	start := make(chan struct{})
	results := make(chan missis.NewTicketResult, jobs)
	errs := make(chan error, jobs)
	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := clients[i%len(clients)].ImportMarkdown(context.Background(), missis.RequestContext{
				Actor:          "client/" + string(rune('a'+i%len(clients))),
				IdempotencyKey: "concurrent-import-" + string(rune('a'+i)),
			}, missis.ImportOptions{
				Content:  "# Ticket " + string(rune('A'+i)) + "\n\n## body\n\nbody " + string(rune('A'+i)) + "\n",
				Artifact: "ticket-" + string(rune('a'+i)) + ".md",
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("concurrent import: %v", err)
	}
	if len(results) != jobs {
		t.Fatalf("completed imports = %d, want %d", len(results), jobs)
	}

	seen := make(map[string]bool, jobs)
	for result := range results {
		if result.Ref == "" || seen[result.Ref] {
			t.Fatalf("duplicate or empty import result: %+v", result)
		}
		seen[result.Ref] = true
		projection, err := clients[0].ShowTicket(context.Background(), result.Ref, missis.ShowOptions{})
		if err != nil {
			t.Fatalf("show imported ticket %s: %v", result.Ref, err)
		}
		if _, ok := projection.Parts["body"]; !ok {
			t.Fatalf("imported ticket %s has no body Part: %+v", result.Ref, projection.Parts)
		}
	}
	if artifacts, err := svc1.Store().ListArtifacts(context.Background()); err != nil {
		t.Fatal(err)
	} else if len(artifacts) != jobs {
		t.Fatalf("artifact rows = %d, want %d", len(artifacts), jobs)
	}
}

func TestAtomicMarkdownImportProposalFailureLeavesNoTicketOrArtifactIndex(t *testing.T) {
	svc := openFixed(t, fixedClock{fixedNow()})
	if err := svc.RegisterIngestionPlugin(plugin.IngestionRegistration{
		Manifest: plugin.Manifest{ID: "test/ambiguous", Version: "1", CodeHash: "ambiguous-hash"},
		ID:       "markdown",
		Selector: plugin.IngestSelector{Operation: "import-markdown", MediaType: "text/markdown", TargetKind: model.KindTicket},
		Plugin:   badIngestionPlugin{},
	}); err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := svc.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ImportMarkdown(context.Background(), missis.RequestContext{}, missis.ImportOptions{
		Content:  "# Should not commit\n\n## body\n\nbody\n",
		Artifact: "failed.md",
	})
	if err == nil || !strings.Contains(err.Error(), "ambiguous ingestion plugin") {
		t.Fatalf("error = %v, want explicit plugin selection failure", err)
	}
	afterEvents, err := svc.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	if afterEvents != beforeEvents {
		t.Fatalf("events changed after failed proposal: before=%d after=%d", beforeEvents, afterEvents)
	}
	artifacts, err := svc.Store().ListArtifacts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifact index changed after failed proposal: %+v", artifacts)
	}
}

func TestIngestMarkdownStoresArtifactAndProvenance(t *testing.T) {
	svc := openFixed(t, fixedClock{fixedNow()})
	ctx := context.Background()
	created, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "Ingest"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Ingest(ctx, missis.RequestContext{IdempotencyKey: "ingest-markdown"}, missis.IngestOptions{
		Operation:  "import-markdown",
		Target:     created.ID,
		MediaType:  "text/markdown",
		SourceName: "note.md",
		Content:    strings.NewReader("# Explanation\n\nhello\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Artifact, "artifact:sha256:") || result.Value != 1 {
		t.Fatalf("result = %+v", result)
	}
	artifacts, err := svc.Store().ListArtifacts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].Ref != result.Artifact {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	events, err := svc.LoadTicketEvents(ctx, model.TicketID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	var imported *model.Event
	for i := range events {
		if events[i].Operation == model.OpCreatePart && events[i].Value.Kind == model.ValueKindMarkdown {
			imported = &events[i]
		}
	}
	if imported == nil || imported.Invocation == nil || imported.Invocation.Plugin != "missis/markdown-import" {
		t.Fatalf("imported event provenance = %+v", imported)
	}
	if len(imported.Sources) != 1 || imported.Sources[0].Ref.Entity != result.Artifact {
		t.Fatalf("imported sources = %+v", imported.Sources)
	}

	replayed, err := svc.Ingest(ctx, missis.RequestContext{IdempotencyKey: "ingest-markdown"}, missis.IngestOptions{
		Operation: "import-markdown", Target: created.ID, MediaType: "text/markdown", Content: strings.NewReader("different content"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Artifact != result.Artifact || replayed.Event != result.Event {
		t.Fatalf("replayed = %+v, first = %+v", replayed, result)
	}
}

func TestIngestValidationFailureLeavesNoArtifactIndexOrEvent(t *testing.T) {
	svc := openFixed(t, fixedClock{fixedNow()})
	if err := svc.RegisterIngestionPlugin(plugin.IngestionRegistration{
		Manifest: plugin.Manifest{ID: "test/bad", Version: "1", CodeHash: "test-hash"},
		ID:       "bad",
		Selector: plugin.IngestSelector{Operation: "bad-ingest"},
		Plugin:   badIngestionPlugin{},
	}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.NewTicket(context.Background(), missis.RequestContext{}, missis.NewTicketOptions{Title: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Ingest(context.Background(), missis.RequestContext{}, missis.IngestOptions{
		Operation: "bad-ingest", Target: created.ID, MediaType: "text/plain", Content: bytes.NewBufferString("bad"),
	})
	if err == nil || !strings.Contains(err.Error(), "artifact input") {
		t.Fatalf("error = %v", err)
	}
	after, err := svc.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("events changed: before=%d after=%d", before, after)
	}
	artifacts, err := svc.Store().ListArtifacts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifact index = %+v", artifacts)
	}
}

type badIngestionPlugin struct{}

func (badIngestionPlugin) Propose(_ context.Context, input plugin.IngestInput) (plugin.IngestProposal, error) {
	return plugin.IngestProposal{Events: []model.Event{{
		Stream: input.Request.Target, Operation: model.OpCreatePart,
		Target: model.Ref{Kind: model.KindPart, Entity: "part:bad", Path: []string{"bad"}},
		Value:  model.Value{Kind: model.ValueKindText, Text: "bad"},
		Actor:  model.ActorRef{Kind: "plugin", ID: "test/bad"},
	}}}, nil
}
