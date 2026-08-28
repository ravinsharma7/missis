package store

import (
	"context"
	"fmt"
	"strings"
)

// DurabilityProfile reports the effective settings of the write connection.
// It describes acknowledged SQLite commits, not backup or replication state.
type DurabilityProfile struct {
	Name                               string
	JournalMode                        string
	Synchronous                        string
	ProcessCrashRecovery               bool
	AcknowledgedCommitPowerLossDurable bool
}

// DurabilityProfileContext reads the active SQLite settings instead of
// repeating configuration constants, so health output detects configuration
// drift as well as documenting the intended profile.
func (s *Store) DurabilityProfileContext(ctx context.Context) (DurabilityProfile, error) {
	var journalMode string
	if err := s.writer.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		return DurabilityProfile{}, fmt.Errorf("read SQLite journal mode: %w", err)
	}
	var synchronous int
	if err := s.writer.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&synchronous); err != nil {
		return DurabilityProfile{}, fmt.Errorf("read SQLite synchronous mode: %w", err)
	}

	journalMode = strings.ToLower(journalMode)
	synchronousName := sqliteSynchronousName(synchronous)
	profile := DurabilityProfile{
		Name:                 journalMode + "-" + strings.ToLower(synchronousName),
		JournalMode:          journalMode,
		Synchronous:          synchronousName,
		ProcessCrashRecovery: journalMode == "wal" && synchronous >= 1,
	}
	// For the WAL profile used by Missis, SQLite documents FULL as the point
	// where each committed transaction is synced for power-loss durability.
	profile.AcknowledgedCommitPowerLossDurable = journalMode == "wal" && synchronous >= 2
	return profile, nil
}

func sqliteSynchronousName(value int) string {
	switch value {
	case 0:
		return "OFF"
	case 1:
		return "NORMAL"
	case 2:
		return "FULL"
	case 3:
		return "EXTRA"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", value)
	}
}
