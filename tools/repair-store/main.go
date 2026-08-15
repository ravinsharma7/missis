package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ravinsharma7/missis/implementation/store"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "show gaps without changing the store")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: repair-store [--dry-run] <missis.db>")
		os.Exit(2)
	}
	db, err := store.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	defer db.Close()

	if *dryRun {
		gaps, err := db.SequenceGaps()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(8)
		}
		if len(gaps) == 0 {
			fmt.Println("no sequence gaps")
			return
		}
		for _, gap := range gaps {
			fmt.Printf("%s:%s missing %v\n", gap.StreamKind, gap.StreamEntity, gap.Missing)
		}
		return
	}

	if err := db.RepairSequenceGaps(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	if err := db.CheckConsistency(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	fmt.Println("store repaired")
}
