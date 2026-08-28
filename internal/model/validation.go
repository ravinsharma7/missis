package model

import (
	"fmt"
	"strings"
)

// ValidateAppend checks event append preconditions against the existing ledger.
func ValidateAppend(existing []Event, proposed Event) error {
	if proposed.ID == "" {
		return fmt.Errorf("event ID is required")
	}
	if proposed.Stream.Kind == "" || proposed.Stream.Entity == "" {
		return fmt.Errorf("event stream is required")
	}
	if proposed.Actor.ID == "" && proposed.Actor.Name == "" {
		return fmt.Errorf("actor is required")
	}
	if proposed.RecordedAt.IsZero() {
		return fmt.Errorf("recorded_at is required")
	}
	if proposed.EffectiveAt.IsZero() {
		return fmt.Errorf("effective_at is required")
	}
	if proposed.Target.Kind == "" || proposed.Target.Entity == "" {
		return fmt.Errorf("target reference is required")
	}
	descriptor, ok := LookupOperation(proposed.Operation)
	if !ok {
		return fmt.Errorf("unsupported operation: %s", proposed.Operation)
	}
	if descriptor.Validate != nil {
		if err := descriptor.Validate(proposed); err != nil {
			return err
		}
	}

	all := append(append([]Event(nil), existing...), proposed)
	proj, err := ProjectStream(all, proposed.Stream, proposed.EffectiveAt, MaxRecordedAt(all))
	if err != nil {
		return err
	}
	if proj == nil {
		return fmt.Errorf("projection failed")
	}
	return nil
}

// ValidatePathSegments validates the recommended part path syntax.
func ValidatePathSegments(segments []string) error {
	if len(segments) == 0 {
		return fmt.Errorf("path must not be empty")
	}
	for _, segment := range segments {
		if segment == "" {
			return fmt.Errorf("path segment must not be empty")
		}
		if !isLowerAlphaNumeric(segment[0]) {
			return fmt.Errorf("invalid path segment: %s", segment)
		}
		for _, r := range segment[1:] {
			if !isSegmentRune(r) {
				return fmt.Errorf("invalid path segment: %s", segment)
			}
		}
	}
	return nil
}

func isLowerAlphaNumeric(r byte) bool {
	return r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
}

func isSegmentRune(r rune) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	return strings.ContainsRune("._-", r)
}
