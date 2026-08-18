package blackbox

import (
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestMultiProcessConcurrency(t *testing.T) {
	t.Parallel()
	// covers PH1-CON-001 PH1-CON-002 N106 N107 N108 N109
	store := filepath.Join(t.TempDir(), "missis.db")
	preserveStoreOnFailure(t, store)
	base := newTicket(t, store, "base")
	if base["ref"] != "#1" {
		t.Fatalf("base ref = %v", base["ref"])
	}

	const workers = 8
	var wg sync.WaitGroup
	results := make([]cmdResult, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := runMissisRaw(store, "new", "--json", "agent-"+strconv.Itoa(i))
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			results[i] = result
		}(i)
	}
	wg.Wait()
	for i, result := range results {
		if result.code != 0 {
			t.Fatalf("worker %d failed: %d %s", i, result.code, result.stderr)
		}
	}

	shown := mustJSON(t, runMissis(t, store, "show", "--json"))
	tickets := shown["tickets"].([]any)
	if len(tickets) != workers+1 {
		t.Fatalf("expected %d tickets, got %d", workers+1, len(tickets))
	}

	var setWG sync.WaitGroup
	setResults := make([]cmdResult, workers)
	for i := 0; i < workers; i++ {
		setWG.Add(1)
		go func(i int) {
			defer setWG.Done()
			result, err := runMissisRaw(store, "set", "--json", "#1/agent-"+strconv.Itoa(i), "value-"+strconv.Itoa(i))
			if err != nil {
				t.Errorf("set worker %d: %v", i, err)
				return
			}
			setResults[i] = result
		}(i)
	}
	setWG.Wait()
	for i, result := range setResults {
		if result.code != 0 {
			t.Fatalf("set worker %d failed: %d stdout=%s stderr=%s", i, result.code, result.stdout, result.stderr)
		}
	}
	assertNoDuplicateEventIDs(t, store)
	projection := mustJSON(t, runMissis(t, store, "show", "--json", "#1"))
	parts := projection["parts"].(map[string]any)
	for i := 0; i < workers; i++ {
		key := "agent-" + strconv.Itoa(i)
		if _, ok := parts[key]; !ok {
			t.Fatalf("missing part %s: %v", key, parts)
		}
	}
	health := runMissis(t, store, "show", "--health", "--json")
	if health.code != 0 {
		t.Fatalf("health after concurrent sets failed: %d stdout=%s stderr=%s", health.code, health.stdout, health.stderr)
	}
}

func TestConcurrentSetHealthStress(t *testing.T) {
	t.Parallel()
	store := filepath.Join(t.TempDir(), "missis.db")
	preserveStoreOnFailure(t, store)
	base := newTicket(t, store, "stress")
	if base["ref"] != "#1" {
		t.Fatalf("base ref = %v", base["ref"])
	}

	const iterations = 5
	const workers = 4
	for iter := 0; iter < iterations; iter++ {
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ref := "#1/" + strconv.Itoa(iter) + "-" + strconv.Itoa(i)
				result, err := runMissisRaw(store, "set", "--json", ref, "value")
				if err != nil {
					errs <- err
					return
				}
				if result.code != 0 {
					errs <- fmt.Errorf("worker %d failed: code=%d stdout=%s stderr=%s", i, result.code, result.stdout, result.stderr)
				}
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}

		health := runMissis(t, store, "show", "--health", "--json")
		if health.code != 0 {
			t.Fatalf("health after stress iteration %d failed: %d stdout=%s stderr=%s", iter, health.code, health.stdout, health.stderr)
		}
	}
	assertNoDuplicateEventIDs(t, store)
}
