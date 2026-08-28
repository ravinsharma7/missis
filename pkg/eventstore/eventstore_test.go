package eventstore

import (
	"errors"
	"testing"
	"time"
)

func TestValidateEventRejectsMissingNeutralIdentity(t *testing.T) {
	event := Event{
		ID:          "event:1",
		Stream:      Ref{Kind: "run", ID: "run:1"},
		Type:        "spy.run.started",
		Subject:     Ref{Kind: "run", ID: "run:1"},
		Payload:     []byte(`{}`),
		RecordedAt:  time.Now().UTC(),
		EffectiveAt: time.Now().UTC(),
		Actor:       Actor{Kind: "facility"},
	}
	if err := ValidateEvent(event); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("error = %v, want invalid event", err)
	}
}
