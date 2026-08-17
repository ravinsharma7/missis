package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ravinsharma7/missis/implementation/store"
)

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: repair-store <missis.db>")
		fmt.Fprintln(os.Stderr, "verifies store consistency and reports sequence gaps; in-place repair is disabled")
		os.Exit(2)
	}

	db, err := store.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	defer db.Close()

	if err := db.CheckConsistency(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	gaps, err := db.SequenceGaps()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	if len(gaps) == 0 {
		fmt.Println("store consistent; no sequence gaps")
		return
	}
	for _, gap := range gaps {
		fmt.Printf("%s:%s missing %v\n", gap.StreamKind, gap.StreamEntity, gap.Missing)
	}
	fmt.Fprintln(os.Stderr, "in-place sequence repair is disabled: accepted events are immutable; restore from a backup or create a new store with a repair receipt")
	os.Exit(8)
}
