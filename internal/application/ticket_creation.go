package application

import (
	"context"
	"strconv"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

type ticketCreationPlan struct {
	ticketID model.TicketID
	batchID  model.BatchID
	events   []model.Event
	result   *missis.NewTicketResult
	now      time.Time
}

// planTicketCreation builds the complete ticket event batch without writing
// it. Import workflows use the same plan and append it together with their
// plugin-proposed Parts and artifact metadata.
func (s *Service) planTicketCreation(
	ctx context.Context,
	req missis.RequestContext,
	title, project string,
	extra func(stream model.Ref, actor model.ActorRef, now, effectiveAt time.Time, batchID model.BatchID) []model.Event,
) (ticketCreationPlan, error) {
	req, now := s.normalize(req)
	ticketID := model.TicketID(missis.NewID("ticket"))
	result := &missis.NewTicketResult{}
	if req.IdempotencyKey != "" {
		replayed, lookupErr := s.LookupIdempotencyContext(ctx, req.IdempotencyKey, result)
		if lookupErr != nil {
			return ticketCreationPlan{}, keepStorage(lookupErr)
		}
		if replayed {
			if result.ID == "" {
				return ticketCreationPlan{}, validation("idempotency key already belongs to another operation: %s", req.IdempotencyKey)
			}
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
			return ticketCreationPlan{}, keepStorage(err)
		}
		if len(projectEvents) == 0 {
			return ticketCreationPlan{}, validation("project does not exist: project:%s; create it with: missis new --kind project --id %s", project, project)
		}
		projectRef := model.Ref{Kind: model.KindProject, Entity: project}
		events = append(events, missis.NewEvent(
			stream, model.OpAssertLink, stream,
			model.Value{Text: model.RelationHasHome, Ref: &projectRef},
			actor, now, req.EffectiveAt, batchID, "",
		))
	}
	return ticketCreationPlan{
		ticketID: ticketID,
		batchID:  batchID,
		events:   events,
		result:   result,
		now:      now,
	}, nil
}

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
	plan, err := s.planTicketCreation(ctx, req, title, project, extra)
	if err != nil {
		return missis.NewTicketResult{}, err
	}
	req, now := s.normalize(req)
	outcome, alias, err := s.AppendTicketBatch(ctx, plan.events, req.IdempotencyKey, plan.result)
	if err != nil {
		return missis.NewTicketResult{}, keepStorage(err)
	}
	if outcome.Replayed {
		return *plan.result, nil
	}
	if plan.result.Ref == "" {
		plan.result = &missis.NewTicketResult{
			Ref:        "#" + strconv.FormatUint(alias, 10),
			ID:         string(plan.ticketID),
			Title:      title,
			Status:     "open",
			Project:    stringPtrOrNil(project),
			RecordedAt: now.Format(time.RFC3339),
		}
	}
	if req.IdempotencyKey != "" {
		if err := s.UpdateIdempotencyResultContext(ctx, req.IdempotencyKey, plan.result); err != nil {
			return missis.NewTicketResult{}, keepStorage(err)
		}
	}
	return *plan.result, nil
}
