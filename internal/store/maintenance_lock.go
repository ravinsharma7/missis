package store

import (
	"errors"
	"fmt"
	"github.com/ravinsharma7/missis/internal/artifact"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrMaintenanceBusy means another application client or maintenance command
// currently owns the store lease required by the requested operation.
var ErrMaintenanceBusy = errors.New("missis store is busy")

// ErrMaintenanceLock means the operating system could not provide the
// advisory lock. Callers must fail closed rather than treating the store as
// offline when coordination is unavailable.
var ErrMaintenanceLock = errors.New("missis store maintenance lock unavailable")

// Lease coordinates application access with offline maintenance. Shared
// leases are held by normal clients and backups; exclusive leases are held by
// migration and garbage collection.
type Lease struct {
	file   *os.File
	unlock func() error
	path   string
	mode   LeaseMode
	once   sync.Once
	err    error
}

// LeaseMode describes the access held by a lease. It is intentionally small:
// normal stores need shared access, while offline maintenance needs exclusive
// access. A lease is held for the lifetime of the owning resource, not per
// database operation.
type LeaseMode uint8

const (
	LeaseShared LeaseMode = iota + 1
	LeaseExclusive
)

func AcquireSharedLease(path string) (*Lease, error) {
	return acquireLease(path, false)
}

func AcquireExclusiveLease(path string) (*Lease, error) {
	return acquireLease(path, true)
}

func acquireLease(path string, exclusive bool) (*Lease, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%w: store path is empty", ErrMaintenanceLock)
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("%w: resolve store path: %v", ErrMaintenanceLock, err)
	}
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, wrapLeasePathError(dir, fmt.Errorf("%w: create store directory %q: %v", ErrMaintenanceLock, dir, err))
	}
	lockPath := absPath + ".maintenance.lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, wrapLeasePathError(lockPath, fmt.Errorf("%w: open %q: %v", ErrMaintenanceLock, lockPath, err))
	}
	unlock, busy, err := tryLockFile(file, exclusive)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("%w: %v", ErrMaintenanceLock, err)
	}
	if busy {
		_ = file.Close()
		return nil, fmt.Errorf("%w: %s; close active missis clients and retry", ErrMaintenanceBusy, absPath)
	}
	mode := LeaseShared
	if exclusive {
		mode = LeaseExclusive
	}
	return &Lease{file: file, unlock: unlock, path: absPath, mode: mode}, nil
}

func wrapLeasePathError(path string, err error) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "file name too long") || strings.Contains(text, "filename too long") || strings.Contains(text, "path too long") || strings.Contains(text, "name too long") {
		return fmt.Errorf("%w: %s: %v", artifact.ErrPathTooLong, path, err)
	}
	return err
}

// Path returns the canonical resource path protected by the lease.
func (l *Lease) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Mode returns the access level held by the lease.
func (l *Lease) Mode() LeaseMode {
	if l == nil {
		return 0
	}
	return l.mode
}

func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if l.unlock != nil {
			l.err = l.unlock()
		}
		if closeErr := l.file.Close(); l.err == nil {
			l.err = closeErr
		}
	})
	return l.err
}

// MaintenanceErrorCode is used by maintenance commands for stable JSON
// diagnostics without exposing platform-specific lock errors.
func MaintenanceErrorCode(err error) string {
	if errors.Is(err, ErrMaintenanceBusy) {
		return "maintenance_busy"
	}
	if errors.Is(err, ErrMaintenanceLock) {
		return "maintenance_lock_unavailable"
	}
	return "maintenance_error"
}
