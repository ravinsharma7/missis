package tooling

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/peerconfig"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

type PeerInspection struct {
	Handle          string                       `json:"handle"`
	Adapter         string                       `json:"adapter"`
	Path            string                       `json:"path"`
	ExpectedStoreID string                       `json:"expected_store_id"`
	Claim           *missis.StoreIdentityClaimV1 `json:"claim,omitempty"`
	Classification  string                       `json:"classification"`
	Retryable       bool                         `json:"retryable"`
	Recourse        string                       `json:"recourse,omitempty"`
}

type PeerInspectionReport struct {
	Version string           `json:"version"`
	Status  string           `json:"status"`
	Peers   []PeerInspection `json:"peers"`
}

func InspectPeers(ctx context.Context, set peerconfig.SetV1) PeerInspectionReport {
	report := PeerInspectionReport{Version: set.Version, Status: "ok"}
	for _, binding := range set.Peers {
		inspection := PeerInspection{Handle: binding.Handle, Adapter: binding.Adapter, Path: binding.SQLitePath, ExpectedStoreID: binding.ExpectedStoreID}
		if runtime.GOOS != "linux" {
			inspection.Classification = "peer-platform-unsupported"
			inspection.Recourse = "use the confirmed Linux local-peer profile or complete #112 platform evidence"
			report.Status = "failed"
			report.Peers = append(report.Peers, inspection)
			continue
		}
		peer := application.NewLocalPeer(binding, nil)
		snapshot, err := peer.OpenExternalResolutionSnapshot(ctx)
		if err != nil {
			inspection.Classification, inspection.Retryable, inspection.Recourse = classifyPeerOpenError(err)
			report.Status = "failed"
			report.Peers = append(report.Peers, inspection)
			continue
		}
		claim, err := snapshot.StoreIdentityClaimContext(ctx)
		closeErr := snapshot.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			inspection.Classification, inspection.Retryable, inspection.Recourse = classifyPeerOpenError(err)
			report.Status = "failed"
			report.Peers = append(report.Peers, inspection)
			continue
		}
		inspection.Claim = &claim
		if claim.StoreID != binding.ExpectedStoreID {
			inspection.Classification = "different-store"
			inspection.Recourse = fmt.Sprintf("configured store_id %s but database proves %s; correct the path or expected ID", binding.ExpectedStoreID, claim.StoreID)
			report.Status = "failed"
		} else {
			inspection.Classification = "verified"
		}
		report.Peers = append(report.Peers, inspection)
	}
	return report
}

func classifyPeerOpenError(err error) (string, bool, string) {
	var access *missis.ExternalAuthorityError
	if errors.As(err, &access) {
		return access.Code, access.Retryable, access.OperatorAction
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "peer-timeout", true, "increase the explicit timeout or inspect peer load"
	}
	if errors.Is(err, context.Canceled) {
		return "peer-cancelled", true, "retry when the caller is ready"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "peer-not-found", true, "correct or restore the configured database/coordination path"
	}
	if errors.Is(err, os.ErrPermission) {
		return "peer-permission-denied", false, "grant the operator read and coordination permission"
	}
	var migration *store.StoreMigrationRequiredError
	if errors.As(err, &migration) {
		return "peer-migration-required", false, migration.Error()
	}
	if errors.Is(err, store.ErrIncompatibleStoreFormat) {
		return "peer-format-unsupported", false, "use a compatible binary or explicit migration workflow"
	}
	if errors.Is(err, store.ErrMaintenanceBusy) || errors.Is(err, store.ErrMaintenanceLock) {
		return "coordination-unavailable", true, "restore the existing coordination lock or retry after maintenance"
	}
	return "peer-integrity-failed", false, "run non-mutating verification and restore or quarantine the peer"
}

func RunPeersWithName(args []string, stdout, stderr io.Writer, commandName string) int {
	if len(args) == 0 || args[0] != "inspect" {
		fmt.Fprintf(stderr, "usage: %s inspect --config FILE [--timeout D] [--json]\n", commandName)
		return 2
	}
	flags := flag.NewFlagSet(commandName+" inspect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var configPath string
	var timeout time.Duration
	var jsonOutput bool
	flags.StringVar(&configPath, "config", "", "strict local peer-set configuration")
	flags.DurationVar(&timeout, "timeout", 30*time.Second, "per-inspection timeout")
	flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || configPath == "" || timeout <= 0 {
		fmt.Fprintf(stderr, "usage: %s inspect --config FILE [--timeout D] [--json]\n", commandName)
		return 2
	}
	set, err := peerconfig.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	report := InspectPeers(ctx, set)
	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		for _, peer := range report.Peers {
			claimed := ""
			if peer.Claim != nil {
				claimed = peer.Claim.StoreID
			}
			fmt.Fprintf(stdout, "handle=%s adapter=%s path=%s classification=%s expected_store_id=%s claimed_store_id=%s retryable=%t recourse=%q\n", peer.Handle, peer.Adapter, peer.Path, peer.Classification, peer.ExpectedStoreID, claimed, peer.Retryable, peer.Recourse)
		}
	}
	if report.Status != "ok" {
		return 1
	}
	return 0
}
