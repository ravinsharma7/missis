package application

import (
	"context"
	"strconv"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// createTicket owns the shared ticket creation contract used by both the
// normal and markdown-import workflows: idempotency lookup, the base create
// events, optional home-project validation, one append transaction, and the
// persisted result. Callers only supply workflow-specific ticket parts.
func (s *Service) createTicket(
	ctx context.Context,
	req missis.RequestContext,
	title, project string,
	extra func(stream model.Ref, actor model.ActorRef, now, effectiveAt time.Time, batchID model.BatchID) []model.Event,
) (missis.NewTicketResult, error) {
	req, now := s.normalize(req)
	ticketID := model.TicketID(missis.NewID("ticket"))
	result := &missis.NewTicketResult{}
	if req.IdempotencyKey != "" {
		replayed, lookupErr := s.LookupIdempotencyContext(ctx, req.IdempotencyKey, result)
		if lookupErr == nil && replayed && result.ID != "" {
			ticketID = model.TicketID(result.ID)
		}
	}

	batchID := model.BatchID(missis.NewID("batch"))
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	actor := parseActor(req.Actor)
	events := []model.Event{
		missis.NewEvent(stream, model.OpCreateEntity, stream, model.Value{}, actor, now, req.EffectiveAt, batchID, ""),
		missis.PartEvent(stream, "title", title, model.ValueKindText, actor, now, req.EffectiveAt, batchID),
		missis.PartEvent(stream, "status", "open", model.ValueKindStatus, actor, now, req.EffectiveAt, batchID),
	}
	if extra != nil {
		events = append(events, extra(stream, actor, now, req.EffectiveAt, batchID)...)
	}
	if project != "" {
		projectEvents, err := s.LoadStreamEvents(ctx, model.Ref{Kind: model.KindProject, Entity: project})
		if err != nil {
			return missis.NewTicketResult{}, keepStorage(err)
		}
		if len(projectEvents) == 0 {
			return missis.NewTicketResult{}, validation("project does not exist: project:%s; create it with: missis new --kind project --id %s", project, project)
		}
		projectRef := model.Ref{Kind: model.KindProject, Entity: project}
		events = append(events, missis.NewEvent(
			stream, model.OpAssertLink, stream,
			model.Value{Text: model.RelationHasHome, Ref: &projectRef},
			actor, now, req.EffectiveAt, batchID, "",
		))
	}

	outcome, alias, err := s.AppendTicketBatch(ctx, events, req.IdempotencyKey, result)
	if err != nil {
		return missis.NewTicketResult{}, keepStorage(err)
	}
	if outcome.Replayed {
		return *result, nil
	}
	if result.Ref == "" {
		result = &missis.NewTicketResult{
			Ref:        "#" + strconv.FormatUint(alias, 10),
			ID:         string(ticketID),
			Title:      title,
			Status:     "open",
			Project:    stringPtrOrNil(project),
			RecordedAt: now.Format(time.RFC3339),
		}
	}
	if req.IdempotencyKey != "" {
		if err := s.UpdateIdempotencyResultContext(ctx, req.IdempotencyKey, result); err != nil {
			return missis.NewTicketResult{}, keepStorage(err)
		}
	}
	return *result, nil
}
