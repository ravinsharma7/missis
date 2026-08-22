package plugin

import (
	"context"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
)

type testIngestionPlugin func(context.Context, IngestInput) (IngestProposal, error)

func (f testIngestionPlugin) Propose(ctx context.Context, input IngestInput) (IngestProposal, error) {
	return f(ctx, input)
}

func TestIngestionRegistrySelectsByMetadataAndAddsManifestProvenance(t *testing.T) {
	registry := NewIngestionRegistry()
	if err := registry.Register(IngestionRegistration{
		Manifest: Manifest{ID: "test/markdown", Version: "2.0.0", CodeHash: "abc"},
		ID:       "import",
		Selector: IngestSelector{Operation: "import", MediaType: "text/markdown", TargetKind: model.KindTicket},
		Plugin: testIngestionPlugin(func(_ context.Context, input IngestInput) (IngestProposal, error) {
			return IngestProposal{Events: []model.Event{{Invocation: &input.Invocation}}}, nil
		}),
	}); err != nil {
		t.Fatal(err)
	}
	proposal, selected, err := registry.Run(context.Background(), IngestInput{
		Request:    IngestRequest{Operation: "import", MediaType: "text/markdown", Target: model.Ref{Kind: model.KindTicket, Entity: "ticket:1"}},
		Invocation: model.InvocationRef{ID: "run:1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected != "test/markdown/import" {
		t.Fatalf("selected = %q", selected)
	}
	if got := proposal.Events[0].Invocation; got == nil || got.Plugin != "test/markdown" || got.Version != "2.0.0" || got.CodeHash != "abc" {
		t.Fatalf("invocation provenance = %+v", got)
	}
}

func TestIngestionRegistryRejectsAmbiguityAndMissingPlugin(t *testing.T) {
	registry := NewIngestionRegistry()
	for _, id := range []string{"one", "two"} {
		if err := registry.Register(IngestionRegistration{
			Manifest: Manifest{ID: "test/" + id, Version: "1", CodeHash: "hash-" + id}, ID: "run",
			Selector: IngestSelector{Operation: "same", MediaType: "text/plain"},
			Plugin:   testIngestionPlugin(func(context.Context, IngestInput) (IngestProposal, error) { return IngestProposal{}, nil }),
		}); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := registry.Run(context.Background(), IngestInput{Request: IngestRequest{Operation: "same", MediaType: "text/plain"}, Invocation: model.InvocationRef{ID: "run:1"}})
	if err == nil || !containsError(err, ErrAmbiguousIngestion) {
		t.Fatalf("ambiguity error = %v", err)
	}
	_, _, err = registry.Run(context.Background(), IngestInput{Request: IngestRequest{Operation: "missing"}, Invocation: model.InvocationRef{ID: "run:2"}})
	if err == nil || !containsError(err, ErrNoIngestionPlugin) {
		t.Fatalf("missing plugin error = %v", err)
	}
}

func containsError(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}
