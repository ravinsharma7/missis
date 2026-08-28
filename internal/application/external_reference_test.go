package application

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func openExternalReferenceTestService(t *testing.T, name string, now time.Time) *Service {
	t.Helper()
	root := t.TempDir()
	svc, err := OpenPathWithClockAndArtifactRoot(
		filepath.Join(root, name+".db"),
		fixedClock{t: now},
		filepath.Join(root, name+"-artifacts"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func TestExternalReferenceResolvesByStoreIdentityAndDetectsStaleness(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 6, 0, 0, 0, time.UTC)
	source := openExternalReferenceTestService(t, "source", now)
	target := openExternalReferenceTestService(t, "target", now)
	sourceTicket, err := source.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "source alias one"})
	if err != nil {
		t.Fatal(err)
	}
	targetTicket, err := target.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "target alias one"})
	if err != nil {
		t.Fatal(err)
	}
	if sourceTicket.Ref != "#1" || targetTicket.Ref != "#1" || sourceTicket.ID == targetTicket.ID {
		t.Fatalf("alias collision fixture source=%+v target=%+v", sourceTicket, targetTicket)
	}
	targetStoreID, err := target.StoreIDContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolver := missis.NewPeerResolver(target)
	ref := missis.ExternalReferenceV1{
		Version:     missis.ExternalReferenceVersionV1,
		StoreID:     targetStoreID,
		Namespace:   "missis",
		Kind:        "ticket",
		EntityID:    targetTicket.ID,
		DisplayHint: "target#1",
	}
	first, err := resolver.Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if first.AuthorityState != missis.ExternalAuthorityVerified || first.IdentityState != missis.ExternalIdentityMatched || first.Lifecycle != missis.ExternalLifecycleActive || first.Freshness != missis.ExternalFreshnessCurrent {
		t.Fatalf("first resolution = %#v", first)
	}
	observedRevision := first.StreamRevision
	ref.Observation = &missis.ExternalReferenceObservedV1{
		StreamRevision: &observedRevision,
		CurrentEventID: first.CurrentEventID,
	}
	if _, err := target.Set(ctx, missis.RequestContext{Actor: "test"}, missis.SetValue{
		Target: targetTicket.Ref + "/status",
		Value:  "doing",
		Kind:   model.ValueKindStatus,
	}); err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Freshness != missis.ExternalFreshnessStale || second.StreamRevision <= first.StreamRevision || second.CurrentEventID == first.CurrentEventID {
		t.Fatalf("second resolution = %#v, first = %#v", second, first)
	}
}

func TestExternalPartReferenceReportsRetractionAndUnsupportedCheckpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 6, 30, 0, 0, time.UTC)
	target := openExternalReferenceTestService(t, "target", now)
	ticket, err := target.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "target"})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := target.ShowTicket(ctx, ticket.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	status, ok := projection.Parts["status"]
	if !ok {
		t.Fatalf("status part missing: %#v", projection.Parts)
	}
	storeID, err := target.StoreIDContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolver := missis.NewPeerResolver(target)
	ref := missis.ExternalReferenceV1{
		Version:     missis.ExternalReferenceVersionV1,
		StoreID:     storeID,
		Namespace:   "missis",
		Kind:        "part",
		EntityID:    ticket.ID,
		SubentityID: status.ID,
	}
	active, err := resolver.Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if active.IdentityState != missis.ExternalIdentityMatched || active.Lifecycle != missis.ExternalLifecycleActive {
		t.Fatalf("active resolution = %#v", active)
	}
	if _, err := target.Set(ctx, missis.RequestContext{Actor: "test"}, missis.RetractValue{Target: ticket.Ref + "/status", Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	retracted, err := resolver.Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if retracted.IdentityState != missis.ExternalIdentityMatched || retracted.Lifecycle != missis.ExternalLifecycleRetracted {
		t.Fatalf("retracted resolution = %#v", retracted)
	}
	ref.Pin = &missis.ExternalReferencePinV1{CheckpointDigest: "sha256:checkpoint"}
	unsupported, err := resolver.Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if unsupported.AuthorityState != missis.ExternalAuthorityDegraded || unsupported.IdentityState != missis.ExternalIdentityUnsupported || unsupported.Freshness != missis.ExternalFreshnessUnverified {
		t.Fatalf("checkpoint resolution = %#v", unsupported)
	}
}

func TestPeerResolverDistinguishesExactReplicaFromDivergentCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 7, 0, 0, 0, time.UTC)
	root := t.TempDir()
	targetPath := filepath.Join(root, "target.db")
	target, err := OpenPathWithClockAndArtifactRoot(targetPath, fixedClock{t: now}, filepath.Join(root, "target-artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	ticket, err := target.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "replicated"})
	if err != nil {
		t.Fatal(err)
	}
	replicaPath := filepath.Join(root, "replica.db")
	if err := target.store.BackupContext(ctx, replicaPath); err != nil {
		t.Fatal(err)
	}
	replica, err := OpenPathWithClockAndArtifactRoot(replicaPath, fixedClock{t: now}, filepath.Join(root, "replica-artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replica.Close() })
	storeID, err := target.StoreIDContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ref := missis.ExternalReferenceV1{Version: missis.ExternalReferenceVersionV1, StoreID: storeID, Namespace: "missis", Kind: "ticket", EntityID: ticket.ID}
	resolver := missis.NewPeerResolver(target, replica)
	exact, err := resolver.Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if exact.AuthorityState != missis.ExternalAuthorityVerified || len(exact.PeerInsights) != 2 {
		t.Fatalf("exact resolution = %#v", exact)
	}
	for _, insight := range exact.PeerInsights {
		if insight.Classification != missis.ExternalPeerExactReplica {
			t.Fatalf("exact peer insights = %#v", exact.PeerInsights)
		}
	}
	if _, err := target.Set(ctx, missis.RequestContext{Actor: "test"}, missis.SetValue{Target: ticket.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus}); err != nil {
		t.Fatal(err)
	}
	divergent, err := resolver.Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if divergent.AuthorityState != missis.ExternalAuthorityDegraded || divergent.Failure == nil || divergent.Failure.Code != "divergent-state-unverified" {
		t.Fatalf("divergent resolution = %#v", divergent)
	}
}
