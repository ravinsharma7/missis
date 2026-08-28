package missis

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	testExternalStoreID     = "store:v1:sha256:120311f35ac84b69682c5b5be1dbe7ab96994ef4a8db9d43473d8d0f1f379867"
	testDifferentExternalID = "store:v1:sha256:220311f35ac84b69682c5b5be1dbe7ab96994ef4a8db9d43473d8d0f1f379867"
)

func validExternalReference() ExternalReferenceV1 {
	return ExternalReferenceV1{
		Version:   ExternalReferenceVersionV1,
		StoreID:   testExternalStoreID,
		Namespace: "missis",
		Kind:      "ticket",
		EntityID:  "ticket:target",
	}
}

func TestParseExternalReferenceRejectsLocatorAndUnknownFields(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"version":"external-ref-v1","store_id":"` + testExternalStoreID + `","namespace":"missis","kind":"ticket","entity_id":"ticket:target","locator":"file:///tmp/target.db"}`)
	if _, err := ParseExternalReferenceV1(raw); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("parse error = %v, want unknown-field rejection", err)
	}
}

func TestExternalReferenceIdentityExcludesObservationAndDisplay(t *testing.T) {
	t.Parallel()
	first := validExternalReference()
	first.DisplayHint = "spy-testing#42"
	revision := uint64(7)
	first.Observation = &ExternalReferenceObservedV1{StreamRevision: &revision, CurrentEventID: "event:old"}
	second := validExternalReference()
	second.DisplayHint = "renamed-project#99"
	newRevision := uint64(12)
	second.Observation = &ExternalReferenceObservedV1{StreamRevision: &newRevision, CurrentEventID: "event:new"}
	firstKey, err := first.IdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := second.IdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	if firstKey != secondKey {
		t.Fatalf("identity changed with display/observation\nfirst=%q\nsecond=%q", firstKey, secondKey)
	}
}

type fakeExternalAuthority struct {
	claim      StoreIdentityClaimV1
	openErr    error
	claimErr   error
	resolved   ExternalResolutionV1
	resolveErr error
	called     atomic.Bool
	closed     atomic.Int32
}

func (f *fakeExternalAuthority) OpenExternalResolutionSnapshot(context.Context) (ExternalAuthoritySnapshot, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f, nil
}

func (f *fakeExternalAuthority) StoreIdentityClaimContext(context.Context) (StoreIdentityClaimV1, error) {
	return f.claim, f.claimErr
}

func (f *fakeExternalAuthority) ResolveExternalReferenceContext(context.Context, ExternalReferenceV1, ExternalResolutionQuery) (ExternalResolutionV1, error) {
	f.called.Store(true)
	return f.resolved, f.resolveErr
}

func (f *fakeExternalAuthority) Close() error {
	f.closed.Add(1)
	return nil
}

func fakeStoreClaim(storeID, genesis, head string, count int64) StoreIdentityClaimV1 {
	return NewStoreIdentityClaimV1(storeID, "test-hash-v1", genesis, head, count, 3)
}

func TestPeerResolverReturnsUnavailableWhenNoReachablePeerClaimsStore(t *testing.T) {
	t.Parallel()
	resolver := NewPeerResolver()
	resolved, err := resolver.Resolve(context.Background(), validExternalReference(), ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AuthorityState != ExternalAuthorityUnavailable || resolved.IdentityState != ExternalIdentityUnknown || resolved.Reference.EntityID != "ticket:target" {
		t.Fatalf("resolution = %#v", resolved)
	}
	if !strings.Contains(strings.Join(resolved.Warnings, " "), "no reachable peer") {
		t.Fatalf("resolution did not explain peer discovery state: %#v", resolved)
	}
}

func TestPeerResolverReportsDifferentStoreClaimBeforeResolution(t *testing.T) {
	t.Parallel()
	authority := &fakeExternalAuthority{claim: fakeStoreClaim(testDifferentExternalID, "genesis:different", "head:different", 4)}
	resolver := NewPeerResolver(authority)
	resolved, err := resolver.Resolve(context.Background(), validExternalReference(), ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.IdentityState != ExternalIdentityUnknown || resolved.AuthorityState != ExternalAuthorityUnavailable {
		t.Fatalf("resolution = %#v", resolved)
	}
	if len(resolved.PeerInsights) != 1 || resolved.PeerInsights[0].Classification != ExternalPeerDifferentStore ||
		resolved.PeerInsights[0].ExpectedStoreID != testExternalStoreID || resolved.PeerInsights[0].ClaimedStoreID != testDifferentExternalID ||
		len(resolved.PeerInsights[0].Differences) == 0 {
		t.Fatalf("peer insight = %#v", resolved.PeerInsights)
	}
	if authority.called.Load() {
		t.Fatal("different-store peer was allowed to resolve the reference")
	}
}

func TestPeerResolverHidesAuthorityFailureDetails(t *testing.T) {
	t.Parallel()
	authority := &fakeExternalAuthority{
		claim:      fakeStoreClaim(testExternalStoreID, "genesis:target", "head:target", 4),
		resolveErr: errors.New("open /private/path/target.db: denied"),
	}
	resolver := NewPeerResolver(authority)
	resolved, err := resolver.Resolve(context.Background(), validExternalReference(), ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AuthorityState != ExternalAuthorityDegraded || strings.Contains(strings.Join(resolved.Warnings, " "), "/private/path") {
		t.Fatalf("resolution leaked authority detail: %#v", resolved)
	}
}

func TestPeerResolverUsesOneAuthoritySnapshotAndClosesIt(t *testing.T) {
	t.Parallel()
	authority := &fakeExternalAuthority{
		claim: fakeStoreClaim(testExternalStoreID, "genesis:target", "head:snapshot", 2),
		resolved: ExternalResolutionV1{
			IdentityState:  ExternalIdentityMatched,
			StreamRevision: 2,
			CurrentEventID: "event:snapshot",
		},
	}
	resolved, err := NewPeerResolver(authority).Resolve(context.Background(), validExternalReference(), ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.PeerInsights) != 1 || resolved.PeerInsights[0].HeadDigest != "head:snapshot" || resolved.StreamRevision != 2 {
		t.Fatalf("snapshot resolution = %#v", resolved)
	}
	if authority.closed.Load() != 1 {
		t.Fatalf("snapshot close count = %d, want 1", authority.closed.Load())
	}
}

func TestPeerResolverRejectsSameIDWithDifferentImmutableEvidence(t *testing.T) {
	t.Parallel()
	first := &fakeExternalAuthority{claim: fakeStoreClaim(testExternalStoreID, "genesis:first", "head:first", 4)}
	second := &fakeExternalAuthority{claim: fakeStoreClaim(testExternalStoreID, "genesis:second", "head:second", 4)}
	resolved, err := NewPeerResolver(first, second).Resolve(context.Background(), validExternalReference(), ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AuthorityState != ExternalAuthorityDegraded || resolved.IdentityState != ExternalIdentityCollision {
		t.Fatalf("resolution = %#v", resolved)
	}
	for _, insight := range resolved.PeerInsights {
		if insight.Classification != ExternalPeerIdentityCollision {
			t.Fatalf("peer insight = %#v", resolved.PeerInsights)
		}
	}
	if first.called.Load() || second.called.Load() {
		t.Fatal("identity collision selected a peer")
	}
}

func TestPeerResolverRejectsDivergentSameIdentityReplicas(t *testing.T) {
	t.Parallel()
	first := &fakeExternalAuthority{claim: fakeStoreClaim(testExternalStoreID, "genesis:shared", "head:first", 4)}
	second := &fakeExternalAuthority{claim: fakeStoreClaim(testExternalStoreID, "genesis:shared", "head:second", 5)}
	resolved, err := NewPeerResolver(first, second).Resolve(context.Background(), validExternalReference(), ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AuthorityState != ExternalAuthorityDegraded || resolved.IdentityState != ExternalIdentityMatched {
		t.Fatalf("resolution = %#v", resolved)
	}
	for _, insight := range resolved.PeerInsights {
		if insight.Classification != ExternalPeerDivergentState {
			t.Fatalf("peer insight = %#v", resolved.PeerInsights)
		}
	}
	if first.called.Load() || second.called.Load() {
		t.Fatal("divergent replicas selected a peer")
	}
}

func TestStoreIdentityClaimRejectsChangedImmutableEvidence(t *testing.T) {
	t.Parallel()
	claim := fakeStoreClaim(testExternalStoreID, "genesis:target", "head:target", 4)
	claim.GenesisDigest = "genesis:changed"
	if err := claim.Validate(); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("validation error = %v, want digest mismatch", err)
	}
}

func TestPeerResolverAcceptsExactReplicaClaims(t *testing.T) {
	t.Parallel()
	claim := fakeStoreClaim(testExternalStoreID, "genesis:shared", "head:shared", 4)
	first := &fakeExternalAuthority{claim: claim, resolved: ExternalResolutionV1{IdentityState: ExternalIdentityMatched}}
	second := &fakeExternalAuthority{claim: claim, resolved: ExternalResolutionV1{IdentityState: ExternalIdentityMatched}}
	resolved, err := NewPeerResolver(first, second).Resolve(context.Background(), validExternalReference(), ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.called.Load() || second.called.Load() {
		t.Fatalf("deterministic peer selection first=%t second=%t", first.called.Load(), second.called.Load())
	}
	for _, insight := range resolved.PeerInsights {
		if insight.Classification != ExternalPeerExactReplica {
			t.Fatalf("peer insight = %#v", resolved.PeerInsights)
		}
	}
}
