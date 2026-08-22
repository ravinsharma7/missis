package model

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// FuzzProjectionWinnerRule exercises the bitemporal winner rule with random
// scalar event streams. Invariants are chosen so the oracle does not
// duplicate the folding logic:
//   - events recorded after knownAt must never affect a projection;
//   - events effective after effectiveAt must never affect a projection;
//   - the projection must be independent of input event order.
//
// Input layout: byte 0 selects 1-13 events; then per event four bytes:
// op (0=set, 1=retract), recorded hour (0-23), effective hour (0-23),
// value (0-2 -> open/doing/done).
func FuzzProjectionWinnerRule(f *testing.F) {
	// Seeds mirror the truth-table cases.
	f.Add([]byte{2, 0, 0, 0, 0, 0, 2, 2, 1})             // normal update
	f.Add([]byte{3, 0, 0, 0, 0, 0, 2, 2, 1, 0, 3, 1, 2}) // backdated update
	f.Add([]byte{2, 0, 0, 0, 0, 0, 0, 4, 1})             // future update
	f.Add([]byte{2, 0, 0, 2, 0, 0, 1, 2, 1})             // same effective time
	f.Add([]byte{2, 0, 1, 0, 0, 1, 5, 2, 0})             // retraction
	f.Add([]byte{2, 0, 1, 4, 1, 0, 0, 2, 0})             // out-of-order import

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 1 {
			return
		}
		n := int(data[0]%13) + 1
		if len(data) < 1+4*n {
			return
		}
		base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
		events := make([]Event, 0, n)
		for i := 0; i < n; i++ {
			op := OpSetValue
			if data[1+4*i]%2 == 1 {
				op = OpRetractValue
			}
			rec := base.Add(time.Duration(data[1+4*i+1]%24) * time.Hour)
			eff := base.Add(time.Duration(data[1+4*i+2]%24) * time.Hour)
			value := "open"
			switch data[1+4*i+3] % 3 {
			case 1:
				value = "doing"
			case 2:
				value = "done"
			}
			text := value
			if op == OpRetractValue {
				text = ""
			}
			events = append(events, bScalarEvent(fmt.Sprintf("e%d", i+1), uint64(i+1), op, rec, eff, text))
		}

		samples := []struct{ v, k time.Time }{
			{base, base},
			{base.Add(5 * time.Hour), base.Add(11 * time.Hour)},
			{base.Add(12 * time.Hour), base.Add(12 * time.Hour)},
			{base.Add(24 * time.Hour), base.Add(24 * time.Hour)},
		}
		for _, s := range samples {
			full, err := ProjectTicket(events, bitemporalTicket, s.v, s.k)
			if err != nil {
				return // invalid generated stream; the floor is no panic
			}

			var recFiltered, effFiltered []Event
			for _, e := range events {
				if !e.RecordedAt.After(s.k) {
					recFiltered = append(recFiltered, e)
				}
				if !e.EffectiveAt.After(s.v) {
					effFiltered = append(effFiltered, e)
				}
			}
			if recProj, err := ProjectTicket(recFiltered, bitemporalTicket, s.v, s.k); err == nil && !reflect.DeepEqual(full, recProj) {
				t.Fatalf("recorded-filter invariant broken at V=%v K=%v", s.v, s.k)
			}
			if effProj, err := ProjectTicket(effFiltered, bitemporalTicket, s.v, s.k); err == nil && !reflect.DeepEqual(full, effProj) {
				t.Fatalf("effective-filter invariant broken at V=%v K=%v", s.v, s.k)
			}
			perm := reversed(events)
			if permProj, err := ProjectTicket(perm, bitemporalTicket, s.v, s.k); err == nil && !reflect.DeepEqual(full, permProj) {
				t.Fatalf("permutation invariant broken at V=%v K=%v", s.v, s.k)
			}
		}
	})
}

func reversed(events []Event) []Event {
	out := make([]Event, len(events))
	copy(out, events)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
