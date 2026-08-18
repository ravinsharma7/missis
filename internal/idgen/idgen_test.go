package idgen

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

// TestSeededEntropyReproducesWindowsCollision pins the ticket #65 root cause:
// two entropy streams seeded from the same value (what ulid.Make() used to do
// via time.Now().UnixNano() on Windows coarse timers) emit identical ULIDs
// within the same millisecond. crypto/rand has no seed, which is why New()
// cannot collide across processes.
func TestSeededEntropyReproducesWindowsCollision(t *testing.T) {
	seed := time.Now().UnixNano()
	ms := ulid.Now()
	procA := ulid.Monotonic(rand.New(rand.NewSource(seed)), 0)
	procB := ulid.Monotonic(rand.New(rand.NewSource(seed)), 0)
	a, err := ulid.New(ms, procA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ulid.New(ms, procB)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("same-seed entropy produced different ULIDs (%s vs %s); test premise broken", a, b)
	}
}

// TestNewConcurrentUniqueness guards the fix: many goroutines generating IDs
// concurrently must never observe a duplicate.
func TestNewConcurrentUniqueness(t *testing.T) {
	const workers, perWorker = 32, 500
	var (
		mu   sync.Mutex
		seen = make(map[string]struct{}, workers*perWorker)
		wg   sync.WaitGroup
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := New("event")
				mu.Lock()
				if _, dup := seen[id]; dup {
					mu.Unlock()
					t.Errorf("duplicate id %q", id)
					return
				}
				seen[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}

// TestNewFormat ensures IDs keep the prefix:ULID shape consumers expect.
func TestNewFormat(t *testing.T) {
	const prefix = "event"
	id := New(prefix)
	if len(id) != len(prefix)+1+26 {
		t.Fatalf("id %q has unexpected length %d", id, len(id))
	}
	if _, err := ulid.Parse(id[len(prefix)+1:]); err != nil {
		t.Fatalf("id %q does not end in a parseable ULID: %v", id, err)
	}
}
