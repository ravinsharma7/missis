package main

import (
	"errors"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestInspectTicketsReportsShowErrors(t *testing.T) {
	summaries := []missis.TicketSummary{{Ref: "#7", Title: "unreadable"}}
	wantErr := errors.New("projection unavailable")
	_, violations, inspectionErrors := inspectTickets(summaries, time.Now().UTC(), func(string, missis.ShowOptions) (missis.TicketProjection, error) {
		return missis.TicketProjection{}, wantErr
	})
	if len(violations) != 0 {
		t.Fatalf("violations = %v, want none when inspection fails", violations)
	}
	if len(inspectionErrors) != 1 {
		t.Fatalf("inspection errors = %v, want one error", inspectionErrors)
	}
	if !errors.Is(inspectionErrors[0], wantErr) {
		t.Fatalf("inspection error = %v", inspectionErrors[0])
	}
}

func TestInspectTicketsCountsDoneTickets(t *testing.T) {
	summaries := []missis.TicketSummary{{Ref: "#8", Title: "done"}}
	doneCount, violations, inspectionErrors := inspectTickets(summaries, time.Now().UTC(), func(string, missis.ShowOptions) (missis.TicketProjection, error) {
		return missis.TicketProjection{Status: "done"}, nil
	})
	if doneCount != 1 || len(violations) != 0 || len(inspectionErrors) != 0 {
		t.Fatalf("result = done=%d violations=%v errors=%v", doneCount, violations, inspectionErrors)
	}
}
