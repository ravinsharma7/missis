package tooling

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/peerconfig"
	"github.com/ravinsharma7/missis/internal/store"
)

func TestInspectPeersReportsVerifiedAndDifferentStoreEvidence(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "peer.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	storeID, err := s.StoreID()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	set := peerconfig.SetV1{Version: peerconfig.VersionV1, Peers: []peerconfig.BindingV1{{Handle: "peer", Adapter: peerconfig.AdapterLiveV1, ExpectedStoreID: storeID, SQLitePath: path}}}
	report := InspectPeers(context.Background(), set)
	if report.Status != "ok" || len(report.Peers) != 1 || report.Peers[0].Classification != "verified" || report.Peers[0].Claim == nil || report.Peers[0].Claim.StoreID != storeID {
		t.Fatalf("verified report = %#v", report)
	}
	set.Peers[0].ExpectedStoreID = "store:v1:sha256:" + strings.Repeat("0", 64)
	report = InspectPeers(context.Background(), set)
	if report.Status != "failed" || report.Peers[0].Classification != "different-store" || report.Peers[0].Claim.StoreID != storeID || !strings.Contains(report.Peers[0].Recourse, storeID) {
		t.Fatalf("different-store report = %#v", report)
	}
}
