package application

import (
	"context"
	"errors"
	"os"
	"runtime"

	"github.com/ravinsharma7/missis/internal/peerconfig"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// LocalPeer is an operator-bound opaque authority. Its path is available only
// to protected operator tooling, never through resolver insight or accepted
// references.
type LocalPeer struct {
	binding peerconfig.BindingV1
	clock   missis.Clock
}

func NewLocalPeer(binding peerconfig.BindingV1, clock missis.Clock) *LocalPeer {
	if clock == nil {
		clock = realClock{}
	}
	return &LocalPeer{binding: binding, clock: clock}
}

func (p *LocalPeer) Handle() string { return p.binding.Handle }
func (p *LocalPeer) Path() string   { return p.binding.SQLitePath }
func (p *LocalPeer) ExpectedExternalStoreID() string {
	return p.binding.ExpectedStoreID
}

func (p *LocalPeer) OpenExternalResolutionSnapshot(ctx context.Context) (missis.ExternalAuthoritySnapshot, error) {
	if runtime.GOOS != "linux" {
		return nil, &missis.ExternalAuthorityError{Code: "peer-platform-unsupported", OperatorAction: "use the confirmed Linux profile or complete #112 platform evidence"}
	}
	snapshot, err := store.OpenVerifiedReadSnapshot(ctx, p.binding.SQLitePath)
	if err != nil {
		return nil, localPeerAccessError(err)
	}
	return &externalResolutionSnapshot{store: snapshot, clock: p.clock}, nil
}

func localPeerAccessError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &missis.ExternalAuthorityError{Code: "peer-timeout", Retryable: true, OperatorAction: "increase the explicit timeout or inspect peer load"}
	}
	if errors.Is(err, context.Canceled) {
		return &missis.ExternalAuthorityError{Code: "peer-cancelled", Retryable: true, OperatorAction: "retry when the caller is ready"}
	}
	if errors.Is(err, os.ErrNotExist) {
		return &missis.ExternalAuthorityError{Code: "peer-not-found", Retryable: true, OperatorAction: "correct or restore the configured database and coordination path"}
	}
	if errors.Is(err, os.ErrPermission) {
		return &missis.ExternalAuthorityError{Code: "peer-permission-denied", OperatorAction: "grant the operator read and coordination permission"}
	}
	var migration *store.StoreMigrationRequiredError
	if errors.As(err, &migration) {
		return &missis.ExternalAuthorityError{Code: "peer-migration-required", OperatorAction: migration.Error()}
	}
	if errors.Is(err, store.ErrIncompatibleStoreFormat) {
		return &missis.ExternalAuthorityError{Code: "peer-format-unsupported", OperatorAction: "use a compatible binary or explicit migration workflow"}
	}
	if errors.Is(err, store.ErrMaintenanceBusy) || errors.Is(err, store.ErrMaintenanceLock) {
		return &missis.ExternalAuthorityError{Code: "coordination-unavailable", Retryable: true, OperatorAction: "restore the existing coordination lock or retry after maintenance"}
	}
	return &missis.ExternalAuthorityError{Code: "peer-integrity-failed", OperatorAction: "run non-mutating verification and restore or quarantine the peer"}
}
