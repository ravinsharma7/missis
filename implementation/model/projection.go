package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// MaxRecordedAt returns the latest RecordedAt in events, or the zero time when
// there are no events.
func MaxRecordedAt(events []Event) time.Time {
	var max time.Time
	for _, event := range events {
		if event.RecordedAt.After(max) {
			max = event.RecordedAt
		}
	}
	return max
}

// CurrentProjection projects events as they are currently known at effectiveAt.
func CurrentProjection(events []Event, ticketID TicketID, effectiveAt time.Time) (*Projection, error) {
	return ProjectTicket(events, ticketID, effectiveAt, MaxRecordedAt(events))
}

// BitemporalProjection projects events using separate effective and known time.
func BitemporalProjection(events []Event, ticketID TicketID, effectiveAt, knownAt time.Time) (*Projection, error) {
	return ProjectTicket(events, ticketID, effectiveAt, knownAt)
}

// ProjectTicket folds events for one ticket into a projection.
func ProjectTicket(events []Event, ticketID TicketID, effectiveAt, knownAt time.Time) (*Projection, error) {
	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		if event.Stream.Kind != KindTicket || event.Stream.Entity != string(ticketID) {
			continue
		}
		if event.EffectiveAt.After(effectiveAt) {
			continue
		}
		if event.RecordedAt.After(knownAt) {
			continue
		}
		filtered = append(filtered, event)
	}

	sortEvents(filtered)

	superseded := make(map[EventID]bool)
	for _, event := range filtered {
		for _, oldID := range event.Supersedes {
			superseded[oldID] = true
		}
	}

	proj := &Projection{
		TicketID:    ticketID,
		Parts:       make(map[PartID]*Part),
		Links:       make(map[LinkID]*Link),
		Paths:       make(map[string]PartID),
		EffectiveAt: effectiveAt,
		KnownAt:     knownAt,
	}

	for _, event := range filtered {
		if superseded[event.ID] {
			continue
		}
		if err := applyEvent(proj, event); err != nil {
			return nil, err
		}
	}

	if err := rebuildPaths(proj); err != nil {
		return nil, err
	}
	return proj, nil
}

// ResolvePartPath resolves a logical part path in a projection.
func ResolvePartPath(proj *Projection, ticketID TicketID, path []string) (ResolvedPartPath, error) {
	if proj == nil {
		return ResolvedPartPath{}, fmt.Errorf("projection is nil")
	}
	if proj.TicketID != ticketID {
		return ResolvedPartPath{}, fmt.Errorf("projection ticket mismatch")
	}
	key := strings.Join(path, "/")
	partID, ok := proj.Paths[key]
	if !ok {
		return ResolvedPartPath{}, fmt.Errorf("part path not found: %s", key)
	}
	return ResolvedPartPath{
		PartID:      partID,
		TicketID:    ticketID,
		Segments:    append([]string(nil), path...),
		EffectiveAt: proj.EffectiveAt,
		KnownAt:     proj.KnownAt,
	}, nil
}

func sortEvents(events []Event) {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Sequence != events[j].Sequence {
			return events[i].Sequence < events[j].Sequence
		}
		if !events[i].RecordedAt.Equal(events[j].RecordedAt) {
			return events[i].RecordedAt.Before(events[j].RecordedAt)
		}
		return events[i].ID < events[j].ID
	})
}

func applyEvent(proj *Projection, event Event) error {
	if event.Operation == OpAssertLink || event.Operation == OpRetractLink {
		return applyLinkEvent(proj, event)
	}
	if event.Target.Kind == KindPart {
		partID := PartID(event.Target.Entity)
		part := proj.Parts[partID]

		switch event.Operation {
		case OpCreatePart:
			if part == nil {
				part = &Part{ID: partID, TicketID: proj.TicketID}
				proj.Parts[partID] = part
			}
			part.Name = lastSegment(event.Target.Path)
			if event.Value.Text != "" {
				part.DisplayName = event.Value.Text
			} else {
				part.DisplayName = part.Name
			}
			part.ParentID = parentIDFromRef(event.Value.Ref)
			part.CreatedBy = event.ID
			part.CurrentFrom = event.ID
			part.RetractedBy = nil
			part.Sources = append([]SourceRef(nil), event.Sources...)
			if hasValue(event.Value) {
				value := cloneValue(event.Value)
				part.Value = &value
				part.ValueKind = event.Value.Kind
			}

		case OpSetValue, OpSupersedeEvent:
			if part == nil {
				part = ensurePart(proj, event.Target, event)
			}
			value := cloneValue(event.Value)
			part.Value = &value
			part.ValueKind = event.Value.Kind
			part.CurrentFrom = event.ID
			part.RetractedBy = nil

		case OpAddValue:
			if part == nil {
				part = ensurePart(proj, event.Target, event)
			}
			if part.Value == nil || part.Value.Kind != ValueKindList {
				value := Value{Kind: ValueKindList, List: []string{}}
				part.Value = &value
			}
			part.Value.List = append(part.Value.List, event.Value.Text)
			part.ValueKind = ValueKindList
			part.CurrentFrom = event.ID
			part.RetractedBy = nil

		case OpRetractValue:
			if part == nil {
				part = ensurePart(proj, event.Target, event)
			}
			part.Value = nil
			part.RetractedBy = &event.ID

		case OpRenamePart:
			if part == nil {
				part = ensurePart(proj, event.Target, event)
			}
			if event.Value.Text != "" {
				part.Name = event.Value.Text
			}
			part.DisplayName = event.Value.Text
			part.CurrentFrom = event.ID

		case OpMovePart:
			if part == nil {
				part = ensurePart(proj, event.Target, event)
			}
			part.ParentID = parentIDFromRef(event.Value.Ref)
			part.CurrentFrom = event.ID

		case OpAttachChild:
			if part == nil {
				part = ensurePart(proj, event.Target, event)
			}
			part.ParentID = parentIDFromRef(event.Value.Ref)
			part.CurrentFrom = event.ID

		case OpDetachChild:
			if part == nil {
				part = ensurePart(proj, event.Target, event)
			}
			part.ParentID = nil
			part.CurrentFrom = event.ID

		case OpRetractSubtree:
			if part == nil {
				return nil
			}
			removeSubtree(proj, partID)

		case OpRestorePart:
			if part == nil {
				part = &Part{ID: partID, TicketID: proj.TicketID}
				proj.Parts[partID] = part
			}
			part.Name = lastSegment(event.Target.Path)
			if event.Value.Text != "" {
				part.DisplayName = event.Value.Text
			} else {
				part.DisplayName = part.Name
			}
			part.ParentID = parentIDFromRef(event.Value.Ref)
			part.CreatedBy = event.ID
			part.CurrentFrom = event.ID
			part.RetractedBy = nil
		}
	}

	return nil
}

func ensurePart(proj *Projection, target Ref, event Event) *Part {
	partID := PartID(target.Entity)
	if part := proj.Parts[partID]; part != nil {
		return part
	}
	part := &Part{
		ID:          partID,
		TicketID:    proj.TicketID,
		Name:        lastSegment(target.Path),
		DisplayName: lastSegment(target.Path),
		CreatedBy:   event.ID,
		CurrentFrom: event.ID,
	}
	proj.Parts[partID] = part
	return part
}

func removeSubtree(proj *Projection, root PartID) {
	descendants := collectDescendants(proj, root)
	for _, id := range descendants {
		delete(proj.Parts, id)
	}
	delete(proj.Parts, root)
}

func collectDescendants(proj *Projection, root PartID) []PartID {
	var result []PartID
	for id, part := range proj.Parts {
		if isDescendant(proj, id, root) {
			result = append(result, id)
			_ = part
		}
	}
	return result
}

func isDescendant(proj *Projection, id, ancestor PartID) bool {
	if id == ancestor {
		return false
	}
	current := id
	for {
		part, ok := proj.Parts[current]
		if !ok || part.ParentID == nil {
			return false
		}
		parent := *part.ParentID
		if parent == ancestor {
			return true
		}
		if parent == current {
			return false
		}
		current = parent
	}
}

func rebuildPaths(proj *Projection) error {
	proj.Paths = make(map[string]PartID)
	for id := range proj.Parts {
		path, err := pathForPart(proj, id, nil)
		if err != nil {
			return err
		}
		if _, exists := proj.Paths[path]; exists && proj.Paths[path] != id {
			return fmt.Errorf("path collision: %s", path)
		}
		proj.Paths[path] = id
	}
	return nil
}

func pathForPart(proj *Projection, id PartID, seen map[PartID]bool) (string, error) {
	if seen == nil {
		seen = make(map[PartID]bool)
	}
	if seen[id] {
		return "", fmt.Errorf("containment cycle at part %s", id)
	}
	seen[id] = true
	part, ok := proj.Parts[id]
	if !ok {
		return "", fmt.Errorf("part not found: %s", id)
	}
	name := part.Name
	if name == "" {
		name = part.DisplayName
	}
	if part.ParentID == nil {
		return name, nil
	}
	parentPath, err := pathForPart(proj, *part.ParentID, seen)
	if err != nil {
		return "", err
	}
	if parentPath == "" {
		return name, nil
	}
	return parentPath + "/" + name, nil
}

func parentIDFromRef(ref *Ref) *PartID {
	if ref == nil || ref.Kind != KindPart {
		return nil
	}
	id := PartID(ref.Entity)
	return &id
}

func lastSegment(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[len(path)-1]
}

func hasValue(value Value) bool {
	return value.Kind != "" || value.Text != "" || value.Data != nil || len(value.List) > 0 || value.Ref != nil
}

func cloneValue(value Value) Value {
	cloned := value
	cloned.Retracted = false
	return cloned
}
