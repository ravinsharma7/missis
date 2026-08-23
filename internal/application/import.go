package application

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// buildReimportEvents computes the minimal event set to bring a ticket's
// imported parts in line with new Markdown content. It returns an error when
// an existing imported part would disappear from the source.
func buildReimportEvents(proj *model.Projection, ticketID model.TicketID, parts []model.MarkdownPart, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID, artifact string) ([]model.Event, []string, error) {
	sortParts(parts)
	pathToID := make(map[string]model.PartID, len(proj.Paths))
	for path, id := range proj.Paths {
		pathToID[path] = id
	}
	matched := make(map[model.PartID]bool)
	events := make([]model.Event, 0, len(parts))
	diagnostics := make([]string, 0)
	orderByParent := make(map[string]int)

	for _, part := range parts {
		pathKey := strings.Join(part.Path, "/")
		var (
			partID model.PartID
			ok     bool
		)
		if strings.TrimSpace(part.ID) != "" {
			candidate := model.PartID(part.ID)
			if existingPart := proj.Parts[candidate]; existingPart != nil {
				partID = candidate
				ok = true
			} else {
				if pathID, pathExists := pathToID[pathKey]; pathExists {
					partID = pathID
					ok = true
				} else {
					partID = model.PartID(missis.NewID("part"))
				}
				diagnostics = append(diagnostics, fmt.Sprintf("identity_unresolved at line %d: source Part identity %q was not found; using %q", part.StartLine, part.ID, partID))
			}
			if pathID, pathExists := pathToID[pathKey]; pathExists && pathID != partID {
				return nil, nil, fmt.Errorf("explicit Markdown Part identity %q conflicts with current Part %q at path %s", partID, pathID, pathKey)
			}
			if matched[partID] {
				return nil, nil, fmt.Errorf("explicit Markdown Part identity %q is repeated", partID)
			}
		} else if existingID, pathExists := pathToID[pathKey]; pathExists {
			partID = existingID
			ok = true
		} else {
			for id, existing := range proj.Parts {
				if sourceMatchesArtifact(existing, artifact, part.StartLine, part.EndLine) {
					partID = id
					ok = true
					break
				}
			}
		}

		if partID == "" {
			partID = model.PartID(missis.NewID("part"))
			ok = false
		}
		if !ok || proj.Parts[partID] == nil {
			parentRef := parentRefForPath(part.Path, pathToID)
			event := importPartEvent(ticketID, partID, part, parentRef, actor, recordedAt, effectiveAt, batchID, artifact, model.OpCreatePart, model.ValueKindMarkdown)
			event.Value.OrderKey = model.OrderKeyForIndex(orderByParent[parentPathKey(part.Path)])
			orderByParent[parentPathKey(part.Path)]++
			events = append(events, event)
			pathToID[pathKey] = partID
			continue
		}

		matched[partID] = true
		existing := proj.Parts[partID]
		existingPath := currentPathForPart(proj, partID)
		desiredOrderKey := model.OrderKeyForIndex(orderByParent[parentPathKey(part.Path)])
		orderByParent[parentPathKey(part.Path)]++
		if !equalPaths(existingPath, part.Path) {
			if parentPathsDiffer(existingPath, part.Path) {
				parentRef := parentRefForPath(part.Path, pathToID)
				event := importPartEvent(ticketID, partID, part, parentRef, actor, recordedAt, effectiveAt, batchID, artifact, model.OpMovePart, "")
				event.Value.OrderKey = desiredOrderKey
				events = append(events, event)
			}
			if len(existingPath) == 0 || existingPath[len(existingPath)-1] != part.Path[len(part.Path)-1] {
				events = append(events, importPartEvent(ticketID, partID, part, nil, actor, recordedAt, effectiveAt, batchID, artifact, model.OpRenamePart, model.ValueKindText))
			}
			pathToID[pathKey] = partID
		} else if existing != nil && existing.OrderKey != "" && existing.OrderKey != desiredOrderKey {
			parentRef := parentRefForPath(part.Path, pathToID)
			event := importPartEvent(ticketID, partID, part, parentRef, actor, recordedAt, effectiveAt, batchID, artifact, model.OpMovePart, "")
			event.Value.OrderKey = desiredOrderKey
			events = append(events, event)
		}

		desired := markdownPartValue(part, model.ValueKindMarkdown)
		if !sameMarkdownValue(existing, desired) {
			events = append(events, importPartEvent(ticketID, partID, part, nil, actor, recordedAt, effectiveAt, batchID, artifact, model.OpSetValue, model.ValueKindMarkdown))
		}
	}

	for id, existing := range proj.Parts {
		if !matched[id] && existing != nil && sourceHasArtifact(existing, artifact) {
			path := currentPathForPart(proj, id)
			return nil, nil, fmt.Errorf("existing imported part missing from source: %s", strings.Join(path, "/"))
		}
	}
	return events, diagnostics, nil
}

func markdownDiagnosticMessages(parsed []model.MarkdownDiagnostic, extra []string) []string {
	messages := make([]string, 0, len(parsed)+len(extra))
	for _, diagnostic := range parsed {
		messages = append(messages, fmt.Sprintf("%s at line %d: %s", diagnostic.Code, diagnostic.Line, diagnostic.Message))
	}
	return append(messages, extra...)
}

func importPartEvent(ticketID model.TicketID, partID model.PartID, part model.MarkdownPart, parentRef *model.Ref, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID, artifact string, operation model.Operation, valueKind model.ValueKind) model.Event {
	start, end := part.StartLine, part.EndLine
	source := model.SourceRef{
		Ref:       model.Ref{Kind: model.KindArtifact, Entity: artifact},
		MediaType: "text/markdown",
		Span:      &model.Span{StartLine: &start, EndLine: &end},
	}
	value := model.Value{}
	switch operation {
	case model.OpCreatePart:
		value = markdownPartValue(part, valueKind)
		value.Ref = parentRef
	case model.OpSetValue:
		value = markdownPartValue(part, valueKind)
	case model.OpRenamePart:
		value = model.Value{Kind: valueKind, Text: part.Path[len(part.Path)-1]}
	case model.OpMovePart:
		value = model.Value{Ref: parentRef}
	}
	return model.Event{
		ID:          model.EventID(missis.NewID("event")),
		Stream:      model.Ref{Kind: model.KindTicket, Entity: string(ticketID)},
		Operation:   operation,
		Target:      model.Ref{Kind: model.KindPart, Entity: string(partID), Path: part.Path},
		Value:       value,
		RecordedAt:  recordedAt,
		EffectiveAt: effectiveAt,
		Actor:       actor,
		BatchID:     &batchID,
		Sources:     []model.SourceRef{source},
	}
}

func markdownPartValue(part model.MarkdownPart, fallbackKind model.ValueKind) model.Value {
	if part.Inline != nil {
		return model.Value{Kind: model.ValueKindInlineSequence, Data: *part.Inline}
	}
	value := model.Value{Kind: fallbackKind}
	if part.Body != "" {
		value.Text = part.Body
	}
	return value
}

func sameMarkdownValue(part *model.Part, desired model.Value) bool {
	if part == nil || part.Value == nil {
		return desired.Kind == model.ValueKindMarkdown && desired.Text == "" && desired.Data == nil
	}
	current := *part.Value
	// Containment metadata belongs to the create/move event, not the
	// Markdown value. Ignore it when deciding whether a reimport changed the
	// content payload.
	current.Ref = nil
	current.OrderKey = ""
	current.Retracted = false
	desired.Ref = nil
	desired.OrderKey = ""
	desired.Retracted = false
	return reflect.DeepEqual(current, desired)
}

func sortParts(parts []model.MarkdownPart) {
	sort.SliceStable(parts, func(i, j int) bool { return len(parts[i].Path) < len(parts[j].Path) })
}

func parentPathKey(path []string) string {
	if len(path) <= 1 {
		return ""
	}
	return strings.Join(path[:len(path)-1], "/")
}

func sourceMatchesArtifact(part *model.Part, artifact string, startLine, endLine int) bool {
	if part == nil {
		return false
	}
	for _, source := range part.Sources {
		if source.Ref.Entity != artifact || source.Span == nil {
			continue
		}
		sourceStart := 0
		sourceEnd := 0
		if source.Span.StartLine != nil {
			sourceStart = *source.Span.StartLine
		}
		if source.Span.EndLine != nil {
			sourceEnd = *source.Span.EndLine
		}
		if startLine <= sourceEnd && endLine >= sourceStart {
			return true
		}
	}
	return false
}

func sourceHasArtifact(part *model.Part, artifact string) bool {
	if part == nil {
		return false
	}
	for _, source := range part.Sources {
		if source.Ref.Entity == artifact {
			return true
		}
	}
	return false
}

func parentRefForPath(path []string, pathToID map[string]model.PartID) *model.Ref {
	if len(path) <= 1 {
		return nil
	}
	parentKey := strings.Join(path[:len(path)-1], "/")
	parentID, ok := pathToID[parentKey]
	if !ok {
		return nil
	}
	return &model.Ref{Kind: model.KindPart, Entity: string(parentID)}
}

func equalPaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func parentPathsDiffer(a, b []string) bool {
	if len(a) <= 1 || len(b) <= 1 {
		return len(a) != len(b)
	}
	return !equalPaths(a[:len(a)-1], b[:len(b)-1])
}

func currentPathForPart(proj *model.Projection, partID model.PartID) []string {
	for path, id := range proj.Paths {
		if id == partID {
			return strings.Split(path, "/")
		}
	}
	return nil
}
