package model

import (
	"fmt"
	"strings"

	"github.com/ravinsharma7/missis/internal/artifact"
)

// ArtifactReferenceOccurrence is one semantically decoded artifact reference
// in accepted history. It is not a substring match and names the exact event
// field that made the object live.
type ArtifactReferenceOccurrence struct {
	Ref      string
	Managed  bool
	EventID  EventID
	Location string
}

// AcceptedArtifactReferences decodes every core field whose typed value can
// name either a managed CAS object or an unmanaged artifact-kind source
// identity. Adding a durable field with either meaning requires extending this
// algorithm and the format compatibility fixture together.
func AcceptedArtifactReferences(event Event) ([]ArtifactReferenceOccurrence, error) {
	var result []ArtifactReferenceOccurrence
	add := func(ref Ref, location string) error {
		if ref.Kind != KindArtifact {
			return nil
		}
		parsed, err := artifact.ParseRef(ref.Entity)
		if err != nil {
			// Pre-CAS imported source identities used the artifact kind with a
			// named source such as artifact:specs/report.md. Preserve and report
			// that explicit class, but never guess that it has managed bytes.
			if strings.HasPrefix(ref.Entity, "artifact:sha256:") {
				return fmt.Errorf("event %s %s: %w", event.ID, location, err)
			}
			result = append(result, ArtifactReferenceOccurrence{Ref: ref.Entity, Managed: false, EventID: event.ID, Location: location})
			return nil
		}
		result = append(result, ArtifactReferenceOccurrence{Ref: parsed.String(), Managed: true, EventID: event.ID, Location: location})
		return nil
	}
	var addValue func(Value, string) error
	addValue = func(value Value, location string) error {
		if value.Ref != nil {
			if err := add(*value.Ref, location+".ref"); err != nil {
				return err
			}
		}
		switch data := value.Data.(type) {
		case ArtifactDescriptor:
			return add(data.Ref, location+".data.ref")
		case *ArtifactDescriptor:
			if data != nil {
				return add(data.Ref, location+".data.ref")
			}
		case MediaDescriptor:
			if strings.HasPrefix(data.URI, "artifact:") {
				return add(Ref{Kind: KindArtifact, Entity: data.URI}, location+".data.uri")
			}
		case *MediaDescriptor:
			if data != nil && strings.HasPrefix(data.URI, "artifact:") {
				return add(Ref{Kind: KindArtifact, Entity: data.URI}, location+".data.uri")
			}
		case Evidence:
			if err := add(data.Ref, location+".data.ref"); err != nil {
				return err
			}
			for index, ref := range data.ClaimRefs {
				if err := add(ref, fmt.Sprintf("%s.data.claim_refs[%d]", location, index)); err != nil {
					return err
				}
			}
			for index, source := range data.Sources {
				if err := add(source.Ref, fmt.Sprintf("%s.data.sources[%d]", location, index)); err != nil {
					return err
				}
			}
			return add(data.ProducedBy, location+".data.produced_by")
		case VerificationResult:
			if err := add(data.Ref, location+".data.ref"); err != nil {
				return err
			}
			if err := add(data.Claim, location+".data.claim"); err != nil {
				return err
			}
			if err := add(data.Evaluator, location+".data.evaluator"); err != nil {
				return err
			}
			for index, ref := range data.Evidence {
				if err := add(ref, fmt.Sprintf("%s.data.evidence[%d]", location, index)); err != nil {
					return err
				}
			}
		case InlineSequence:
			for index, item := range data.Items {
				if err := addValue(Value{Kind: ValueKind(item.Kind), Data: item.Data}, fmt.Sprintf("%s.data.items[%d]", location, index)); err != nil {
					return err
				}
			}
		case *InlineSequence:
			if data != nil {
				return addValue(Value{Kind: ValueKindInlineSequence, Data: *data}, location)
			}
		}
		return nil
	}
	if err := add(event.Stream, "stream"); err != nil {
		return nil, err
	}
	if err := add(event.Target, "target"); err != nil {
		return nil, err
	}
	for index, source := range event.Sources {
		if err := add(source.Ref, fmt.Sprintf("sources[%d]", index)); err != nil {
			return nil, err
		}
	}
	for index, ref := range event.Inputs {
		if err := add(ref, fmt.Sprintf("inputs[%d]", index)); err != nil {
			return nil, err
		}
	}
	for index, ref := range event.Causes {
		if err := add(ref, fmt.Sprintf("causes[%d]", index)); err != nil {
			return nil, err
		}
	}
	for effectIndex, effect := range event.Effects {
		prefix := fmt.Sprintf("effects[%d]", effectIndex)
		if effect.Ref != nil {
			if err := add(*effect.Ref, prefix+".ref"); err != nil {
				return nil, err
			}
		}
		for evidenceIndex, ref := range effect.Evidence {
			if err := add(ref, fmt.Sprintf("%s.evidence[%d]", prefix, evidenceIndex)); err != nil {
				return nil, err
			}
		}
		if effect.Before != nil {
			if err := addValue(*effect.Before, prefix+".before"); err != nil {
				return nil, err
			}
		}
		if effect.After != nil {
			if err := addValue(*effect.After, prefix+".after"); err != nil {
				return nil, err
			}
		}
	}
	if err := addValue(event.Value, "value"); err != nil {
		return nil, err
	}
	return result, nil
}
