package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/peerconfig"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestLocalPeerAccessErrorsHaveStableRecourse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		input     error
		code      string
		retryable bool
	}{
		{name: "timeout", input: context.DeadlineExceeded, code: "peer-timeout", retryable: true},
		{name: "cancelled", input: context.Canceled, code: "peer-cancelled", retryable: true},
		{name: "not found", input: os.ErrNotExist, code: "peer-not-found", retryable: true},
		{name: "permission", input: os.ErrPermission, code: "peer-permission-denied"},
		{name: "migration", input: &store.StoreMigrationRequiredError{Found: 4, Target: store.CurrentStoreFormatRevision, Path: "/peer.db"}, code: "peer-migration-required"},
		{name: "format", input: store.ErrIncompatibleStoreFormat, code: "peer-format-unsupported"},
		{name: "coordination busy", input: store.ErrMaintenanceBusy, code: "coordination-unavailable", retryable: true},
		{name: "coordination lock", input: store.ErrMaintenanceLock, code: "coordination-unavailable", retryable: true},
		{name: "integrity", input: errors.New("chain mismatch"), code: "peer-integrity-failed"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var access *missis.ExternalAuthorityError
			if err := localPeerAccessError(test.input); !errors.As(err, &access) {
				t.Fatalf("error type = %T", err)
			} else if access.Code != test.code || access.Retryable != test.retryable || access.OperatorAction == "" {
				t.Fatalf("access error = %#v", access)
			}
		})
	}
}

func TestLocalPeerRejectsUnconfirmedPlatform(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux is the confirmed local-peer profile")
	}
	peer := NewLocalPeer(peerconfig.BindingV1{Handle: "unsupported", SQLitePath: "unused.db"}, nil)
	_, err := peer.OpenExternalResolutionSnapshot(context.Background())
	var access *missis.ExternalAuthorityError
	if !errors.As(err, &access) || access.Code != "peer-platform-unsupported" || access.OperatorAction == "" {
		t.Fatalf("unsupported-platform error = %#v", err)
	}
}

func requireConfirmedLocalPeerPlatform(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("local-peer filesystem profile is not confirmed on %s", runtime.GOOS)
	}
}

func TestLocalPeerResolvesAfterMoveWithoutChangingReference(t *testing.T) {
	requireConfirmedLocalPeerPlatform(t)
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 8, 0, 0, 0, time.UTC)
	root := t.TempDir()
	original := filepath.Join(root, "original", "peer.db")
	target, err := OpenPathWithClockAndArtifactRoot(original, fixedClock{t: now}, filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := target.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "moved"})
	if err != nil {
		t.Fatal(err)
	}
	storeID, err := target.StoreIDContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	ref := missis.ExternalReferenceV1{Version: missis.ExternalReferenceVersionV1, StoreID: storeID, Namespace: "missis", Kind: "ticket", EntityID: ticket.ID}
	before, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	movedDir := filepath.Join(root, "moved")
	if err := os.MkdirAll(movedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(movedDir, "peer.db")
	for _, suffix := range []string{"", ".maintenance.lock", "-wal", "-shm"} {
		from := original + suffix
		if _, err := os.Stat(from); err == nil {
			if err := os.Rename(from, moved+suffix); err != nil {
				t.Fatal(err)
			}
		}
	}
	peer := NewLocalPeer(peerconfig.BindingV1{Handle: "moved", Adapter: peerconfig.AdapterLiveV1, ExpectedStoreID: storeID, SQLitePath: moved}, fixedClock{t: now})
	resolved, err := missis.NewPeerResolver(peer).Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AuthorityState != missis.ExternalAuthorityVerified || resolved.IdentityState != missis.ExternalIdentityMatched {
		t.Fatalf("resolution = %#v", resolved)
	}
	after, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("reference changed after move: before=%s after=%s", before, after)
	}
}

func TestResolutionSnapshotStaysCoherentAcrossAppend(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 8, 30, 0, 0, time.UTC)
	target := openExternalReferenceTestService(t, "snapshot-target", now)
	ticket, err := target.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	storeID, err := target.StoreIDContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := target.OpenExternalResolutionSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	claim, err := snapshot.StoreIdentityClaimContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claim.GenesisDigestScheme != "canonical-event-chain-v1" || claim.HeadIntegrityEpoch != "canonical-event-chain-v1" {
		t.Fatalf("claim does not identify format-7 integrity epochs: %#v", claim)
	}
	if _, err := target.Set(ctx, missis.RequestContext{Actor: "test"}, missis.SetValue{Target: ticket.Ref + "/status", Value: "doing", Kind: model.ValueKindStatus}); err != nil {
		t.Fatal(err)
	}
	ref := missis.ExternalReferenceV1{Version: missis.ExternalReferenceVersionV1, StoreID: storeID, Namespace: "missis", Kind: "ticket", EntityID: ticket.ID}
	resolved, err := snapshot.ResolveExternalReferenceContext(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if uint64(claim.EventCount) != resolved.StreamRevision {
		t.Fatalf("snapshot mixed state: claim_count=%d stream_revision=%d", claim.EventCount, resolved.StreamRevision)
	}
	fresh, err := missis.NewPeerResolver(target).Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.StreamRevision <= resolved.StreamRevision {
		t.Fatalf("fresh resolution did not observe append: snapshot=%d fresh=%d", resolved.StreamRevision, fresh.StreamRevision)
	}
}

func TestConfiguredExpectedStoreIDConstrainsLocalPeer(t *testing.T) {
	requireConfirmedLocalPeerPlatform(t)
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	target := openExternalReferenceTestService(t, "wrong-binding", now)
	ticket, err := target.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	storeID, err := target.StoreIDContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wrong := "store:v1:sha256:" + strings.Repeat("0", 64)
	peer := NewLocalPeer(peerconfig.BindingV1{Handle: "wrong", Adapter: peerconfig.AdapterLiveV1, ExpectedStoreID: wrong, SQLitePath: target.path}, fixedClock{t: now})
	ref := missis.ExternalReferenceV1{Version: missis.ExternalReferenceVersionV1, StoreID: storeID, Namespace: "missis", Kind: "ticket", EntityID: ticket.ID}
	resolved, err := missis.NewPeerResolver(peer).Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.IdentityState != missis.ExternalIdentityUnknown || len(resolved.PeerInsights) != 1 || resolved.PeerInsights[0].ConfiguredStoreID != wrong || resolved.PeerInsights[0].ClaimedStoreID != storeID {
		t.Fatalf("wrong binding resolution = %#v", resolved)
	}
}

func TestLocalPeerFoldsAcceptedEventsWithoutRepairingProjection(t *testing.T) {
	requireConfirmedLocalPeerPlatform(t)
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC)
	target := openExternalReferenceTestService(t, "projection-drift", now)
	ticket, err := target.NewTicket(ctx, missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "drift"})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := target.ShowTicket(ctx, ticket.Ref, missis.ShowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	status := projection.Parts["status"]
	driftDB, err := sql.Open("sqlite", target.path)
	if err != nil {
		t.Fatal(err)
	}
	defer driftDB.Close()
	if _, err := driftDB.Exec(`DELETE FROM parts_current WHERE ticket_id=? AND part_id=?`, ticket.ID, status.ID); err != nil {
		t.Fatal(err)
	}
	storeID, err := target.StoreIDContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	peer := NewLocalPeer(peerconfig.BindingV1{Handle: "drift", Adapter: peerconfig.AdapterLiveV1, ExpectedStoreID: storeID, SQLitePath: target.path}, fixedClock{t: now})
	ref := missis.ExternalReferenceV1{Version: missis.ExternalReferenceVersionV1, StoreID: storeID, Namespace: "missis", Kind: "part", EntityID: ticket.ID, SubentityID: status.ID}
	resolved, err := missis.NewPeerResolver(peer).Resolve(ctx, ref, missis.ExternalResolutionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.IdentityState != missis.ExternalIdentityMatched || resolved.Lifecycle != missis.ExternalLifecycleActive {
		t.Fatalf("drifted projection resolution = %#v", resolved)
	}
	var count int
	if err := driftDB.QueryRow(`SELECT COUNT(*) FROM parts_current WHERE ticket_id=? AND part_id=?`, ticket.ID, status.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("local peer repaired derived projection")
	}
}
