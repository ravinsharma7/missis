package tooling

import (
	"fmt"
	"io"

	"github.com/ravinsharma7/missis/internal/store"
)

func RunGaps(args []string, stdout, stderr io.Writer) int {
	return RunGapsWithName(args, stdout, stderr, "store-gaps")
}

func RunGapsWithName(args []string, stdout, stderr io.Writer, commandName string) int {
	stdout, stderr = commandWriters(stdout, stderr)
	if len(args) != 1 {
		fmt.Fprintf(stderr, "usage: %s <missis.db>\n", commandName)
		return 2
	}
	db, err := store.Open(args[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 8
	}
	defer db.Close()

	gaps, err := db.SequenceGaps()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 8
	}
	if len(gaps) == 0 {
		fmt.Fprintln(stdout, "no sequence gaps")
		return 0
	}
	for _, gap := range gaps {
		fmt.Fprintf(stdout, "%s:%s missing %v\n", gap.StreamKind, gap.StreamEntity, gap.Missing)
	}
	return 0
}
