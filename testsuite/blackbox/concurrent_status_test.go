package blackbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/ravinsharma7/missis/implementation/model"
	"github.com/ravinsharma7/missis/implementation/store"
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

	if repairBin != "" && os.Getenv("MISSIS_STORM_NO_REPAIR") == "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				cmd := exec.Command(repairBin, storePath)
				if _, err := cmd.CombinedOutput(); err != nil {
					errs <- fmt.Errorf("repair %d failed under concurrent sets: %v", i, err)
					return
				}
			}
		}()
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

	// A sequential repair + health check is the final arbiter: any real
	// corruption left by the storm must be repairable and then consistent.
	if repairBin != "" {
		if out, err := exec.Command(repairBin, storePath).CombinedOutput(); err != nil {
			t.Fatalf("final sequential repair failed: %v: %s", err, out)
		}
	}
	health = runMissis(t, storePath, "show", "--health", "--json")
	if health.code != 0 {
		t.Fatalf("health after final repair: code=%d stdout=%s stderr=%s", health.code, health.stdout, health.stderr)
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
