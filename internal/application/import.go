package application

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// buildImportEvents converts parsed Markdown parts into create-part events,
// matching the CLI's historical import behavior exactly.
func buildImportEvents(stream model.Ref, parts []model.MarkdownPart, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID, artifact string) []model.Event {
	events := make([]model.Event, 0, len(parts))
	partIDs := make(map[string]model.PartID)
	sortParts(parts)
	for _, part := range parts {
		partIDs[strings.Join(part.Path, "/")] = model.PartID(missis.NewID("part"))
	}
	for _, part := range parts {
		start, end := part.StartLine, part.EndLine
		source := model.SourceRef{
			Ref:       model.Ref{Kind: model.KindArtifact, Entity: artifact},
			MediaType: "text/markdown",
			Span:      &model.Span{StartLine: &start, EndLine: &end},
		}
		value := model.Value{}
		var parentRef *model.Ref
		if len(part.Path) > 1 {
			parentKey := strings.Join(part.Path[:len(part.Path)-1], "/")
			if parentID, ok := partIDs[parentKey]; ok {
				parentRef = &model.Ref{Kind: model.KindPart, Entity: string(parentID)}
			}
		}
		if part.Body != "" {
			value = model.Value{Kind: model.ValueKindMarkdown, Text: part.Body}
		}
		value.Ref = parentRef
		partID := partIDs[strings.Join(part.Path, "/")]
		events = append(events, model.Event{
			ID:          model.EventID(missis.NewID("event")),
			Stream:      stream,
			Operation:   model.OpCreatePart,
			Target:      model.Ref{Kind: model.KindPart, Entity: string(partID), Path: part.Path},
			Value:       value,
			RecordedAt:  recordedAt,
			EffectiveAt: effectiveAt,
			Actor:       actor,
			BatchID:     &batchID,
			Sources:     []model.SourceRef{source},
		})
	}
	return events
}

// buildReimportEvents computes the minimal event set to bring a ticket's
// imported parts in line with new Markdown content. It returns an error when
// an existing imported part would disappear from the source.
func buildReimportEvents(proj *model.Projection, ticketID model.TicketID, parts []model.MarkdownPart, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID, artifact string) ([]model.Event, error) {
	sortParts(parts)
	pathToID := make(map[string]model.PartID, len(proj.Paths))
	for path, id := range proj.Paths {
		pathToID[path] = id
	}
	matched := make(map[model.PartID]bool)
	events := make([]model.Event, 0, len(parts))

	for _, part := range parts {
		pathKey := strings.Join(part.Path, "/")
		partID, ok := pathToID[pathKey]
		if !ok {
			for id, existing := range proj.Parts {
				if sourceMatchesArtifact(existing, artifact, part.StartLine, part.EndLine) {
					partID = id
					break
				}
			}
		}

		if partID == "" {
			partID = model.PartID(missis.NewID("part"))
			parentRef := parentRefForPath(part.Path, pathToID)
			events = append(events, importPartEvent(ticketID, partID, part, parentRef, actor, recordedAt, effectiveAt, batchID, artifact, model.OpCreatePart, model.ValueKindMarkdown))
			pathToID[pathKey] = partID
			continue
		}

		matched[partID] = true
		existing := proj.Parts[partID]
		existingPath := currentPathForPart(proj, partID)
		if !equalPaths(existingPath, part.Path) {
			if parentPathsDiffer(existingPath, part.Path) {
				parentRef := parentRefForPath(part.Path, pathToID)
				events = append(events, importPartEvent(ticketID, partID, part, parentRef, actor, recordedAt, effectiveAt, batchID, artifact, model.OpMovePart, ""))
			}
			if len(existingPath) == 0 || existingPath[len(existingPath)-1] != part.Path[len(part.Path)-1] {
				events = append(events, importPartEvent(ticketID, partID, part, nil, actor, recordedAt, effectiveAt, batchID, artifact, model.OpRenamePart, model.ValueKindText))
			}
			pathToID[pathKey] = partID
		}

		currentBody := ""
		if existing != nil && existing.Value != nil {
			currentBody = existing.Value.Text
		}
		if part.Body != currentBody {
			events = append(events, importPartEvent(ticketID, partID, part, nil, actor, recordedAt, effectiveAt, batchID, artifact, model.OpSetValue, model.ValueKindMarkdown))
		}
	}

	for id, existing := range proj.Parts {
		if !matched[id] && existing != nil && sourceHasArtifact(existing, artifact) {
			path := currentPathForPart(proj, id)
			return nil, fmt.Errorf("existing imported part missing from source: %s", strings.Join(path, "/"))
		}
	}
	return events, nil
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
		if part.Body != "" {
			value = model.Value{Kind: valueKind, Text: part.Body}
		}
		value.Ref = parentRef
	case model.OpSetValue:
		value = model.Value{Kind: valueKind, Text: part.Body}
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

func sortParts(parts []model.MarkdownPart) {
	sort.Slice(parts, func(i, j int) bool {
		if len(parts[i].Path) != len(parts[j].Path) {
			return len(parts[i].Path) < len(parts[j].Path)
		}
		return strings.Join(parts[i].Path, "/") < strings.Join(parts[j].Path, "/")
	})
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
