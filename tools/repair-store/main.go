package main

import (
	"fmt"
	"os"

	"github.com/ravinsharma7/missis/implementation/store"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: repair-store <missis.db>")
		os.Exit(2)
	}
	db, err := store.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	defer db.Close()

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
