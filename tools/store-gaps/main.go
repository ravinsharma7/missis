package main

import (
	"fmt"
	"os"
	"sort"

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

	events, err := db.LoadEvents()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}

	byStream := make(map[string][]uint64)
	for _, event := range events {
		key := string(event.Stream.Kind) + ":" + event.Stream.Entity
		byStream[key] = append(byStream[key], event.Sequence)
	}
	streams := make([]string, 0, len(byStream))
	for key := range byStream {
		streams = append(streams, key)
	}
	sort.Strings(streams)

	found := false
	for _, stream := range streams {
		sequences := byStream[stream]
		sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
		var missing []uint64
		expected := uint64(1)
		for _, sequence := range sequences {
			for expected < sequence {
				missing = append(missing, expected)
				expected++
			}
			expected = sequence + 1
		}
		if len(missing) > 0 {
			found = true
			fmt.Printf("%s missing %v\n", stream, missing)
		}
	}
	if !found {
		fmt.Println("no sequence gaps")
	}
}
