package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

type recordingDiag struct {
	mu  sync.Mutex
	got []map[string]any
}

func (r *recordingDiag) Emit(event string, fields map[string]any) {
	rec := make(map[string]any, len(fields)+1)
	for k, v := range fields {
		rec[k] = v
	}
	rec["event"] = event
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, rec)
}

func (r *recordingDiag) snapshot() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.got...)
}

func diagTestEvents() []model.Event {
	ticketID := model.TicketID("ticket:diag")
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	now := time.Now().UTC()
	return []model.Event{
		{ID: model.EventID("event:diag:1"), Stream: stream, Sequence: 1, Operation: model.OpCreateEntity, Target: model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
		{ID: model.EventID("event:diag:2"), Stream: stream, Sequence: 2, Operation: model.OpCreatePart, Target: model.Ref{Kind: model.KindPart, Entity: "part:diag:title", Path: []string{"title"}}, Value: model.Value{Kind: model.ValueKindText, Text: "Diag"}, RecordedAt: now, EffectiveAt: now, Actor: model.ActorRef{Kind: "test", ID: "test"}},
	}
}

func TestAppendConflictDiagnostics(t *testing.T) {
	t.Parallel()
	rec := &recordingDiag{}
	s, err := OpenWithDiag(filepath.Join(t.TempDir(), "missis.db"), rec)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	events := diagTestEvents()
	if _, _, err := s.AppendTicketBatch(events, "", nil); err != nil {
		t.Fatal(err)
	}

	bad := append([]model.Event(nil), events...)
	bad[1].Value = model.Value{Kind: model.ValueKindText, Text: "Changed"}
	if _, _, err := s.AppendTicketBatch(bad, "", nil); err == nil {
		t.Fatal("expected duplicate-ID batch with different content to be rejected")
	}

	var conflict map[string]any
	for _, r := range rec.snapshot() {
		if r["event"] == "append-replay" && r["decision"] == "conflict" {
			conflict = r
			break
		}
	}
	if conflict == nil {
		t.Fatal("missing conflict diagnostic")
	}
	if conflict["event_id"] != "event:diag:2" {
		t.Fatalf("event_id = %v, want event:diag:2", conflict["event_id"])
	}
	proposed, ok := conflict["proposed_json"].(string)
	if !ok || !strings.Contains(proposed, "Changed") {
		t.Fatalf("proposed_json missing changed value: %#v", conflict["proposed_json"])
	}
	stored, ok := conflict["stored_json"].(string)
	if !ok || !strings.Contains(stored, "Diag") {
		t.Fatalf("stored_json missing original value: %#v", conflict["stored_json"])
	}
	if conflict["batch_size"] != 2 {
		t.Fatalf("batch_size = %v, want 2", conflict["batch_size"])
	}
}

func TestAppendReplayDiagnostics(t *testing.T) {
	t.Parallel()
	rec := &recordingDiag{}
	s, err := OpenWithDiag(filepath.Join(t.TempDir(), "missis.db"), rec)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	events := diagTestEvents()
	if _, _, err := s.AppendTicketBatch(events, "", nil); err != nil {
		t.Fatal(err)
	}
	outcome, _, err := s.AppendTicketBatch(events, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Replayed {
		t.Fatal("second append must be a replay")
	}

	decisions := map[string]bool{}
	for _, r := range rec.snapshot() {
		if r["event"] == "append-replay" {
			decisions[r["decision"].(string)] = true
		}
	}
	if !decisions["absent"] {
		t.Fatalf("missing absent decision: %v", decisions)
	}
	if !decisions["replayed"] {
		t.Fatalf("missing replayed decision: %v", decisions)
	}
}

func TestAppendAttemptDiagnosticsOnRetry(t *testing.T) {
	t.Parallel()
	rec := &recordingDiag{}
	s, err := OpenWithDiag(filepath.Join(t.TempDir(), "missis.db"), rec)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	attempts := 0
	s.appendCommitHook = func() error {
		attempts++
		if attempts == 1 {
			return fmt.Errorf("database is locked")
		}
		return nil
	}
	if _, _, err := s.AppendTicketBatch(diagTestEvents(), "", nil); err != nil {
		t.Fatal(err)
	}

	var busy map[string]any
	for _, r := range rec.snapshot() {
		if r["event"] == "append-attempt" && r["err_kind"] == "busy" {
			busy = r
			break
		}
	}
	if busy == nil {
		t.Fatal("missing busy append-attempt diagnostic")
	}
	if busy["attempt"] != 0 {
		t.Fatalf("attempt = %v, want 0", busy["attempt"])
	}
	if busy["retryable"] != true {
		t.Fatalf("retryable = %v, want true", busy["retryable"])
	}
	if busy["sleep_ms"] != 100 {
		t.Fatalf("sleep_ms = %v, want 100", busy["sleep_ms"])
	}
}
