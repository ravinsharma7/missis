package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

type fixedClock struct {
	t time.Time
}

func (c fixedClock) Now() time.Time {
	return c.t
}

func openFixed(t *testing.T, clock missis.Clock) *Service {
	t.Helper()
	svc, err := OpenPathWithClock(filepath.Join(t.TempDir(), "missis.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func fixedNow() time.Time {
	return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
}

func TestDeterministicBitemporalAndDefaults(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()

	created, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "Bitemporal"})
	if err != nil {
		t.Fatal(err)
	}
	// Defaults applied in one place: recorded_at and effective_at are the
	// injected clock time, and the actor defaults to human/local.
	if created.RecordedAt != now.Format(time.RFC3339) {
		t.Fatalf("recorded_at = %q, want %q", created.RecordedAt, now.Format(time.RFC3339))
	}
	history, err := svc.ShowHistory(ctx, created.Ref, missis.HistoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defaultActor := false
	for _, e := range history {
		if e.Actor == "human/local" {
			defaultActor = true
		}
	}
	if !defaultActor {
		t.Fatalf("expected default actor human/local in history: %+v", history)
	}

	if _, err := svc.Set(ctx, missis.RequestContext{EffectiveAt: now.Add(time.Hour)}, missis.SetValue{Target: created.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus}); err != nil {
		t.Fatal(err)
	}
	current, err := svc.ShowTicket(ctx, created.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "open" {
		t.Fatalf("current status = %q", current.Status)
	}
	future, err := svc.ShowTicket(ctx, created.Ref, missis.ShowOptions{EffectiveAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if future.Status != "doing" {
		t.Fatalf("future status = %q", future.Status)
	}
}

func TestDiagnosticsFileClosesWithService(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.jsonl")
	t.Setenv("MISSIS_STORE_DIAG", path)
	svc, err := OpenPathWithClock(filepath.Join(t.TempDir(), "missis.db"), fixedClock{fixedNow()})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("diagnostics file should be released on close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenPathErrorIncludesStoreContext(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store-directory")
	if err := os.Mkdir(storePath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := OpenPath(storePath)
	if err == nil {
		t.Fatal("expected opening a directory as a store to fail")
	}
	message := err.Error()
	for _, want := range []string{"open missis store", fmt.Sprintf("%q", storePath), "discovery=flag", "runtime="} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q missing %q", message, want)
		}
	}
}

func TestCanceledContextStopsStoreReadsAndWrites(t *testing.T) {
	svc := openFixed(t, fixedClock{fixedNow()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.LoadEvents(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled load error = %v, want context.Canceled", err)
	}
	if _, err := svc.Manifest(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled manifest error = %v, want context.Canceled", err)
	}
	before, err := svc.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendBatch(ctx, []model.Event{{Operation: model.OpCreateEntity}}, "", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled append error = %v, want context.Canceled", err)
	}
	after, err := svc.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("canceled append changed event count: before=%d after=%d", before, after)
	}
}

func TestDeterministicIdempotency(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	req := missis.RequestContext{IdempotencyKey: "k-new"}

	first, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Idem"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.NewTicket(ctx, req, missis.NewTicketOptions{Title: "Idem"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Ref != second.Ref || first.ID != second.ID || first.RecordedAt != second.RecordedAt {
		t.Fatalf("idempotent new mismatch: %+v vs %+v", first, second)
	}
}

func TestTypedErrors(t *testing.T) {
	svc := openFixed(t, fixedClock{fixedNow()})
	ctx := context.Background()

	if _, err := svc.ShowTicket(ctx, "#9999", missis.ShowOptions{}); err != nil {
		var de *missis.DomainError
		if !errors.As(err, &de) || de.Kind != missis.ErrNotFound {
			t.Fatalf("expected NotFound domain error, got %v", err)
		}
	} else {
		t.Fatal("expected not found error")
	}

	created, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "Errors"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, missis.RequestContext{}, missis.SetValue{Target: created.Ref + "/status", Value: "blocked", Kind: model.ValueKindStatus}); err != nil {
		var de *missis.DomainError
		if !errors.As(err, &de) || de.Kind != missis.ErrValidation {
			t.Fatalf("expected Validation domain error, got %v", err)
		}
	} else {
		t.Fatal("expected validation error for blocked status without reason")
	}
}

func TestDeterministicConflict(t *testing.T) {
	now := fixedNow()
	svc := openFixed(t, fixedClock{now})
	ctx := context.Background()
	created, err := svc.NewTicket(ctx, missis.RequestContext{}, missis.NewTicketOptions{Title: "Conflict"})
	if err != nil {
		t.Fatal(err)
	}
	history, err := svc.ShowHistory(ctx, created.Ref+"/status", missis.HistoryOptions{PartPath: []string{"status"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("status history = %d events", len(history))
	}
	oldAlias := history[0].Alias
	if _, err := svc.Set(ctx, missis.RequestContext{}, missis.SetValue{Target: created.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus}); err != nil {
		t.Fatal(err)
	}
	conflictReq := missis.RequestContext{IfCurrent: oldAlias}
	if _, err := svc.Set(ctx, conflictReq, missis.SetValue{Target: created.Ref + "/status", Value: "done", Kind: model.ValueKindStatus}); err != nil {
		var de *missis.DomainError
		if !errors.As(err, &de) || de.Kind != missis.ErrConflict {
			t.Fatalf("expected Conflict domain error, got %v", err)
		}
	} else {
		t.Fatal("expected optimistic concurrency conflict")
	}
}
