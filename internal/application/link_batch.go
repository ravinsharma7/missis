package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

type preparedLinkBatchItem struct {
	item     missis.LinkBatchItem
	from     model.Ref
	target   model.Ref
	moveFrom *model.Ref
}

type linkTripleKey struct {
	from     string
	relation string
	to       string
}

// ApplyLinkBatch validates and appends a set of additive link changes in one
// transaction. It is intentionally separate from SetLink: the SDK's existing
// SetLink method keeps its evidence semantics and continues to allow another
// active assertion of the same triple.
func (s *Service) ApplyLinkBatch(ctx context.Context, req missis.RequestContext, opts missis.LinkBatchOptions) (missis.LinkBatchResult, error) {
	var err error
	ctx, err = s.withIdempotencyRequest(ctx, req, "apply-link-batch", opts)
	if err != nil {
		return missis.LinkBatchResult{}, err
	}
	req, now := s.normalize(req)
	if len(opts.Items) == 0 {
		return missis.LinkBatchResult{}, invalidInput("link batch requires at least one item")
	}

	prepared := make([]preparedLinkBatchItem, 0, len(opts.Items))
	for _, item := range opts.Items {
		if item.Source == "" || item.Target == "" {
			return missis.LinkBatchResult{}, invalidInput("link batch items require source and target")
		}
		if !model.ValidRelation(item.Relation) {
			return missis.LinkBatchResult{}, validation("unsupported relation: %s", item.Relation)
		}
		from, err := s.resolveAnyRef(ctx, item.Source, req.EffectiveAt)
		if err != nil {
			return missis.LinkBatchResult{}, err
		}
		sourceExists, err := s.refExists(ctx, from)
		if err != nil {
			return missis.LinkBatchResult{}, err
		}
		if !sourceExists {
			return missis.LinkBatchResult{}, validation("link source does not exist: %s", item.Source)
		}
		target, err := s.resolveAnyRef(ctx, item.Target, req.EffectiveAt)
		if err != nil {
			return missis.LinkBatchResult{}, err
		}
		exists, err := s.refExists(ctx, target)
		if err != nil {
			return missis.LinkBatchResult{}, err
		}
		if !exists {
			return missis.LinkBatchResult{}, validation("link target does not exist: %s", item.Target)
		}
		stream := model.Ref{Kind: from.Kind, Entity: from.Entity}
		if err := s.validateLinkSchema(ctx, stream, target.Kind, item.Relation, req.EffectiveAt, now); err != nil {
			return missis.LinkBatchResult{}, err
		}

		var moveFrom *model.Ref
		if item.MoveFrom != "" {
			if item.Relation != model.RelationHasHome {
				return missis.LinkBatchResult{}, invalidInput("link batch moves are only supported for has-home")
			}
			fromText := item.MoveFrom
			if !strings.Contains(fromText, ":") {
				fromText = "project:" + fromText
			}
			resolved, resolveErr := s.resolveAnyRef(ctx, fromText, req.EffectiveAt)
			if resolveErr != nil {
				return missis.LinkBatchResult{}, resolveErr
			}
			oldExists, existsErr := s.refExists(ctx, resolved)
			if existsErr != nil {
				return missis.LinkBatchResult{}, existsErr
			}
			if !oldExists {
				return missis.LinkBatchResult{}, validation("link move source does not exist: %s", item.MoveFrom)
			}
			moveFrom = &resolved
		}
		if item.Relation == model.RelationHasHome && (from.Kind != model.KindTicket || target.Kind != model.KindProject) {
			return missis.LinkBatchResult{}, validation("has-home requires a ticket source and a project target")
		}
		if moveFrom != nil && (moveFrom.Kind != model.KindProject || moveFrom.Entity == target.Entity) {
			return missis.LinkBatchResult{}, validation("has-home move requires different project source and destination")
		}
		prepared = append(prepared, preparedLinkBatchItem{item: item, from: from, target: target, moveFrom: moveFrom})
	}

	linkEvents, err := s.LoadLinkEvents(ctx)
	if err != nil {
		return missis.LinkBatchResult{}, keepStorage(err)
	}
	viewsBySource := make(map[string][]model.LinkView)
	loadViews := func(ref model.Ref) ([]model.LinkView, error) {
		key := model.CanonicalRefKey(ref)
		if views, ok := viewsBySource[key]; ok {
			return views, nil
		}
		views, viewErr := model.LinksForRef(linkEvents, ref, req.EffectiveAt, now)
		if viewErr != nil {
			return nil, viewErr
		}
		viewsBySource[key] = views
		return views, nil
	}
	activeAssertions := func(views []model.LinkView, relation string, target model.Ref) []model.LinkAssertionView {
		for _, view := range views {
			if view.Direction == "asserted" && view.Relation == relation &&
				view.To.Kind == target.Kind && view.To.Entity == target.Entity {
				return view.Assertions
			}
		}
		return nil
	}
	activeHomeTargets := func(views []model.LinkView, source model.Ref, except *model.Ref) []model.Ref {
		var targets []model.Ref
		for _, view := range views {
			if view.Direction != "asserted" || view.Relation != model.RelationHasHome || view.To.Kind != model.KindProject {
				continue
			}
			if except != nil && view.To.Entity == except.Entity {
				continue
			}
			targets = append(targets, view.To)
		}
		return targets
	}

	result := missis.LinkBatchResult{}
	var events []model.Event
	var preconditions []store.Precondition
	planned := make(map[linkTripleKey]bool)
	preparedForEvent := make(map[linkTripleKey]preparedLinkBatchItem)
	for _, item := range prepared {
		views, viewErr := loadViews(item.from)
		if viewErr != nil {
			return missis.LinkBatchResult{}, viewErr
		}
		triple := linkTripleKey{from: model.CanonicalRefKey(item.from), relation: item.item.Relation, to: model.CanonicalRefKey(item.target)}
		if planned[triple] {
			result.Skipped = append(result.Skipped, item.item.Target)
			continue
		}
		current := activeAssertions(views, item.item.Relation, item.target)
		if len(current) > 0 {
			result.Skipped = append(result.Skipped, item.item.Target)
			continue
		}

		if item.item.Relation == model.RelationHasHome {
			otherHomes := activeHomeTargets(views, item.from, item.moveFrom)
			if len(otherHomes) > 0 {
				return missis.LinkBatchResult{}, validation("ticket already has a home project: project:%s; retract it before assigning a new one", otherHomes[0].Entity)
			}
		}
		if item.moveFrom != nil {
			oldViews, oldErr := loadViews(item.from)
			if oldErr != nil {
				return missis.LinkBatchResult{}, oldErr
			}
			oldAssertions := activeAssertions(oldViews, item.item.Relation, *item.moveFrom)
			if len(oldAssertions) == 0 {
				return missis.LinkBatchResult{}, validation("no active %s assertion from %s to %s; nothing to move", item.item.Relation, item.item.Source, item.item.MoveFrom)
			}
			for _, assertion := range oldAssertions {
				events = append(events, s.linkEventFor(model.OpRetractLink, item.from, *item.moveFrom, item.item.Relation, item.item.Reason, parseActor(req.Actor), now, req.EffectiveAt, assertion.CreatedBy))
				preconditions = append(preconditions, store.Precondition{Link: &store.LinkPrecondition{
					From: item.from, Relation: item.item.Relation, To: *item.moveFrom, ExpectedCurrentEvent: assertion.CreatedBy,
				}})
			}
		}

		preconditions = append(preconditions, store.Precondition{Link: &store.LinkPrecondition{
			From: item.from, Relation: item.item.Relation, To: item.target,
		}})
		events = append(events, s.linkEventFor(model.OpAssertLink, item.from, item.target, item.item.Relation, item.item.Reason, parseActor(req.Actor), now, req.EffectiveAt, ""))
		planned[triple] = true
		preparedForEvent[triple] = item
	}

	if len(events) == 0 {
		return result, nil
	}
	outcome, err := s.AppendBatch(ctx, events, req.IdempotencyKey, preconditions, &result)
	if err != nil {
		return missis.LinkBatchResult{}, keepStorage(err)
	}
	if outcome.Replayed {
		return result, nil
	}
	for _, event := range outcome.Events {
		if event.Operation != model.OpAssertLink || event.Value.Ref == nil {
			continue
		}
		triple := linkTripleKey{from: model.CanonicalRefKey(event.Stream), relation: event.Value.Text, to: model.CanonicalRefKey(*event.Value.Ref)}
		item, ok := preparedForEvent[triple]
		if !ok {
			continue
		}
		operation := "link"
		value := item.item.Relation + ":" + item.item.Target
		if item.moveFrom != nil {
			operation = "move-link"
			value = fmt.Sprintf("%s:%s->%s", item.item.Relation, item.item.MoveFrom, item.item.Target)
		}
		result.Added = append(result.Added, missis.SetResult{
			Ref: item.item.Source, Event: "@e" + fmt.Sprint(event.AliasSeq), Operation: operation, Value: value,
		})
	}
	if req.IdempotencyKey != "" {
		if err := s.UpdateIdempotencyResultContext(ctx, req.IdempotencyKey, &result); err != nil {
			return missis.LinkBatchResult{}, keepStorage(err)
		}
	}
	return result, nil
}

// SetLinkIfAbsent is the CLI's non-additive default. It uses the batch path's
// absence precondition, so two concurrent CLI writers cannot both create the
// same active triple. The public SDK SetLink method remains additive.
func (s *Service) SetLinkIfAbsent(ctx context.Context, req missis.RequestContext, opts missis.LinkOptions) (missis.SetResult, error) {
	if !opts.Add || opts.Retract {
		return s.SetLink(ctx, req, opts)
	}
	result, err := s.ApplyLinkBatch(ctx, req, missis.LinkBatchOptions{Items: []missis.LinkBatchItem{{
		Source: strings.TrimSuffix(opts.Ref, "/links"), Relation: opts.Relation, Target: opts.Target, Reason: opts.Reason,
	}}})
	if err != nil {
		return missis.SetResult{}, err
	}
	if len(result.Added) == 0 {
		return missis.SetResult{Ref: opts.Ref, Operation: "noop", Value: opts.Relation + ":" + opts.Target, Warning: "link already active; no event appended"}, nil
	}
	return result.Added[0], nil
}
