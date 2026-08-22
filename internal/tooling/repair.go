package tooling

import (
	"fmt"
	"io"

	"github.com/ravinsharma7/missis/internal/store"
)

func RunRepair(args []string, stdout, stderr io.Writer) int {
	return RunRepairWithName(args, stdout, stderr, "repair-store")
}

func RunRepairWithName(args []string, stdout, stderr io.Writer, commandName string) int {
	stdout, stderr = commandWriters(stdout, stderr)
	if len(args) != 1 {
		fmt.Fprintf(stderr, "usage: %s <missis.db>\n", commandName)
		fmt.Fprintln(stderr, "verifies store consistency and reports sequence gaps; in-place repair is disabled")
		return 2
	}

	lease, err := store.AcquireExclusiveLease(args[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 8
	}
	db, err := store.OpenWithLease(args[0], lease, nil)
	if err != nil {
		_ = lease.Close()
		fmt.Fprintln(stderr, err)
		return 8
	}
	defer db.Close()

	if err := db.CheckConsistency(); err != nil {
		fmt.Fprintln(stderr, err)
		return 8
	}
	gaps, err := db.SequenceGaps()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 8
	}
	if len(gaps) == 0 {
		fmt.Fprintln(stdout, "store consistent; no sequence gaps")
		return 0
	}
	for _, gap := range gaps {
		fmt.Fprintf(stdout, "%s:%s missing %v\n", gap.StreamKind, gap.StreamEntity, gap.Missing)
	}
	fmt.Fprintln(stderr, "in-place sequence repair is disabled: accepted events are immutable; restore from a backup or create a new store with a repair receipt")
	return 8
}
