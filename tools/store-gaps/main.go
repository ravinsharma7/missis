package main

import (
	"fmt"
	"os"

	"github.com/ravinsharma7/missis/implementation/store"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: store-gaps <missis.db>")
		os.Exit(2)
	}
	db, err := store.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	defer db.Close()

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
}
