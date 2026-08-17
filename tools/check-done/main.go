package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/pkg/missis"
)

// check-done verifies the ticket lifecycle rule: a ticket marked done must
// not carry an outstanding follow-up in a "next" part. Follow-ups must be
// converted into tickets (or the part retracted). Run with an optional store
// path argument; otherwise the default store discovery applies.
func main() {
	storeFlag := ""
	if len(os.Args) > 1 {
		storeFlag = os.Args[1]
	}
	client, err := missis.Open(storeFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	defer client.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	summaries, err := client.ListTicketSummaries(ctx, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}

	var violations []string
	doneCount := 0
	for _, summary := range summaries {
		proj, err := client.ShowTicket(ctx, summary.Ref, missis.ShowOptions{EffectiveAt: now})
		if err != nil {
			continue
		}
		if proj.Status != "done" {
			continue
		}
		doneCount++
		if next, ok := proj.Parts["next"]; ok {
			if text, ok := next.Value.(string); ok && strings.TrimSpace(text) != "" {
				violations = append(violations, fmt.Sprintf("%s (%s) is done but has a next part: %s", summary.Ref, summary.Title, text))
			}
		}
	}

	if len(violations) > 0 {
		for _, v := range violations {
			fmt.Println(v)
		}
		fmt.Fprintln(os.Stderr, "done tickets must not carry follow-up 'next' parts; convert follow-ups to tickets or retract the part")
		os.Exit(1)
	}
	fmt.Printf("check-done: %d done ticket(s), no outstanding follow-ups\n", doneCount)
}
