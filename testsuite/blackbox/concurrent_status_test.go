package blackbox

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
)

// TestConcurrentStatusChangesAcrossClients hammers one shared store with many
// separate missis clients changing status on different tickets at the same
// time, while a concurrent repair-store client renumbers sequences. The
// invariant after the storm: every set succeeded, no sequence gaps, health ok.
func TestConcurrentStatusChangesAcrossClients(t *testing.T) {
	t.Parallel()
	storePath := filepath.Join(t.TempDir(), "missis.db")
	const tickets = 8
	for i := 0; i < tickets; i++ {
		newTicket(t, storePath, fmt.Sprintf("status-%d", i))
	}

	iterations := 10
	if v := os.Getenv("MISSIS_STORM_ITERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("invalid MISSIS_STORM_ITERATIONS: %q", v)
		}
		iterations = n
	}

	const workers = 8
	statuses := []string{"open", "doing", "done"}
	errs := make(chan error, workers*2+2)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				ref := fmt.Sprintf("#%d", (w+i)%tickets+1)
				status := statuses[(w+i)%len(statuses)]
				result, err := runMissisRaw(storePath, "set", "--json", ref+"/status", status)
				if err != nil {
					errs <- fmt.Errorf("worker %d iter %d: %v", w, i, err)
					return
				}
				if result.code != 0 {
					errs <- fmt.Errorf("worker %d iter %d failed: code=%d stderr=%s", w, i, result.code, result.stderr)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	health := runMissis(t, storePath, "show", "--health", "--json")
	if health.code != 0 {
		dumpStore(t, storePath)
		t.Fatalf("health after concurrent status changes: code=%d stdout=%s stderr=%s", health.code, health.stdout, health.stderr)
	}
	shown := mustJSON(t, runMissis(t, storePath, "show", "--json"))
	if got := len(shown["tickets"].([]any)); got != tickets {
		t.Fatalf("expected %d tickets, got %d", tickets, got)
	}

	// The append path must never create gaps on its own.
	s, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("open after storm: %v", err)
	}
	defer s.Close()
	gaps, err := s.SequenceGaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 0 {
		t.Fatalf("concurrent status sets created sequence gaps: %+v", gaps)
	}
}

func TestRepairStoreRefusesInPlaceRepair(t *testing.T) {
	t.Parallel()
	storePath := filepath.Join(t.TempDir(), "missis.db")
	created := newTicket(t, storePath, "Gap")
	ticketID := created["id"].(string)

	rawDB, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the historical allocation bug: next_sequence skips a number
	// without any event being written for it.
	if _, err := rawDB.Exec(`UPDATE streams SET next_sequence = next_sequence + 1 WHERE stream_kind = ? AND stream_entity = ?`, string(model.KindTicket), ticketID); err != nil {
		t.Fatal(err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatal(err)
	}

	// A subsequent set lands past the skipped number, leaving a visible gap.
	if result := runMissis(t, storePath, "set", "--json", created["ref"].(string)+"/status", "doing"); result.code != 0 {
		t.Fatalf("set after skipped sequence: %d %s", result.code, result.stderr)
	}

	// Health surfaces the incident instead of hiding it.
	health := runMissis(t, storePath, "show", "--health", "--json")
	if health.code == 0 {
		t.Fatalf("health should fail with sequence gaps: %s", health.stdout)
	}

	// repair-store refuses to rewrite accepted history.
	refusal := exec.Command(repairBin, storePath)
	var stderr bytes.Buffer
	refusal.Stderr = &stderr
	if err := refusal.Run(); err == nil {
		t.Fatalf("repair-store should refuse in-place repair")
	}
	if !strings.Contains(stderr.String(), "restore from a backup") {
		t.Fatalf("unexpected refusal message: %s", stderr.String())
	}
}

func dumpStore(t *testing.T, path string) {
	t.Helper()
	s, err := store.Open(path)
	if err != nil {
		t.Logf("dump: open: %v", err)
		return
	}
	defer s.Close()
	gaps, err := s.SequenceGaps()
	if err != nil {
		t.Logf("dump: gaps: %v", err)
		return
	}
	for _, gap := range gaps {
		t.Logf("gap: %s:%s missing %v", gap.StreamKind, gap.StreamEntity, gap.Missing)
		events, err := s.LoadTicketEvents(model.TicketID(gap.StreamEntity))
		if err != nil {
			t.Logf("dump: load %s: %v", gap.StreamEntity, err)
			continue
		}
		sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
		for _, ev := range events {
			pathStr := ""
			for _, seg := range ev.Target.Path {
				if pathStr != "" {
					pathStr += "/"
				}
				pathStr += seg
			}
			t.Logf("  seq=%d op=%s target=%s/%s val=%q", ev.Sequence, ev.Operation, ev.Target.Kind, pathStr, ev.Value.Text)
		}
	}
}
