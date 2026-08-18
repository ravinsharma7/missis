// Package idgen generates ULID-based identifiers with crypto/rand entropy.
//
// Ticket #65: identifiers must not depend on a seeded PRNG. oklog/ulid's
// Make() seeds a per-process math/rand with time.Now().UnixNano(); on Windows,
// coarse timers let concurrently spawned processes share that seed, producing
// identical random suffixes and (within the same millisecond) identical ULIDs.
// crypto/rand has no user-controllable seed, so separate processes draw
// independent entropy from the OS CSPRNG and cannot reproduce each other's
// identifiers.
package idgen

import (
	"crypto/rand"
	"io"

	"github.com/oklog/ulid/v2"
)

// entropy is the ULID randomness source. It is a var (not a const) so tests
// in this package can demonstrate the seeded-entropy failure mode; production
// code always uses crypto/rand.
var entropy io.Reader = rand.Reader

// New returns a ULID with the given prefix (e.g. "event:01M0..."), using
// crypto/rand entropy. The result is time-ordered and unique across processes
// in practice.
func New(prefix string) string {
	return prefix + ":" + ulid.MustNew(ulid.Now(), entropy).String()
}
