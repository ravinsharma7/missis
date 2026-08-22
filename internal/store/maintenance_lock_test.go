package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestMaintenanceLeasesCoordinateSharedAndExclusiveAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missis.db")
	shared, err := AcquireSharedLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()

	secondShared, err := AcquireSharedLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer secondShared.Close()
	if _, err := AcquireExclusiveLease(path); !errors.Is(err, ErrMaintenanceBusy) {
		t.Fatalf("exclusive lease error = %v, want ErrMaintenanceBusy", err)
	}

	other, err := AcquireExclusiveLease(filepath.Join(t.TempDir(), "other.db"))
	if err != nil {
		t.Fatalf("different store should not be blocked: %v", err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}

	if err := secondShared.Close(); err != nil {
		t.Fatal(err)
	}
	if err := shared.Close(); err != nil {
		t.Fatal(err)
	}
	exclusive, err := AcquireExclusiveLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := exclusive.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenVariantsShareInitializationAndLeaseOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missis.db")
	opened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireExclusiveLease(path); !errors.Is(err, ErrMaintenanceBusy) {
		t.Fatalf("Open did not retain shared lease: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}

	lease, err := AcquireExclusiveLease(path)
	if err != nil {
		t.Fatal(err)
	}
	opened, err = OpenWithLease(path, lease, nil)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	released, err := AcquireExclusiveLease(path)
	if err != nil {
		t.Fatalf("OpenWithLease did not release owned lease: %v", err)
	}
	if err := released.Close(); err != nil {
		t.Fatal(err)
	}

	snapshot, err := OpenSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	exclusive, err := AcquireExclusiveLease(path)
	if err != nil {
		_ = snapshot.Close()
		t.Fatal(err)
	}
	if err := exclusive.Close(); err != nil {
		_ = snapshot.Close()
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
}
