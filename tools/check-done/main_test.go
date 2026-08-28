package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestResolveLifecycleStoreRejectsDefaultDiscovery(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MISSIS_STORE", "")

	_, err := resolveLifecycleStore("")
	if err == nil || !strings.Contains(err.Error(), "refusing the default path") {
		t.Fatalf("error = %v, want default-path refusal", err)
	}
}

func TestResolveLifecycleStoreRejectsMissingExplicitStore(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.db")
	_, err := resolveLifecycleStore(missing)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error = %v, want missing-store refusal", err)
	}
}

func TestResolveLifecycleStoreAcceptsExistingMarkerStore(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, ".missis-store")
	if err := os.Mkdir(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(storeDir, "missis.db")
	if err := os.WriteFile(storePath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".missis"), []byte("./.missis-store/missis.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	t.Setenv("MISSIS_STORE", "")

	got, err := resolveLifecycleStore("")
	if err != nil {
		t.Fatal(err)
	}
	if got != storePath {
		t.Fatalf("path = %q, want %q", got, storePath)
	}
}

func TestInspectTicketsReportsShowErrors(t *testing.T) {
	summaries := []missis.TicketSummary{{Ref: "#7", Title: "unreadable"}}
	wantErr := errors.New("projection unavailable")
	_, violations, inspectionErrors := inspectTickets(summaries, time.Now().UTC(), func(string, missis.ShowOptions) (missis.TicketProjection, error) {
		return missis.TicketProjection{}, wantErr
	})
	if len(violations) != 0 {
		t.Fatalf("violations = %v, want none when inspection fails", violations)
	}
	if len(inspectionErrors) != 1 {
		t.Fatalf("inspection errors = %v, want one error", inspectionErrors)
	}
	if !errors.Is(inspectionErrors[0], wantErr) {
		t.Fatalf("inspection error = %v", inspectionErrors[0])
	}
}

func TestInspectTicketsCountsDoneTickets(t *testing.T) {
	summaries := []missis.TicketSummary{{Ref: "#8", Title: "done"}}
	doneCount, violations, inspectionErrors := inspectTickets(summaries, time.Now().UTC(), func(string, missis.ShowOptions) (missis.TicketProjection, error) {
		return missis.TicketProjection{Status: "done"}, nil
	})
	if doneCount != 1 || len(violations) != 0 || len(inspectionErrors) != 0 {
		t.Fatalf("result = done=%d violations=%v errors=%v", doneCount, violations, inspectionErrors)
	}
}
