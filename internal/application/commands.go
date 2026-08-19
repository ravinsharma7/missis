package application

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/schema"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// ----- creation workflows -----

func (s *Service) NewTicket(ctx context.Context, req missis.RequestContext, opts missis.NewTicketOptions) (missis.NewTicketResult, error) {
	req, now := s.normalize(req)
	ticketID := model.TicketID(missis.NewID("ticket"))
	result := &missis.NewTicketResult{}
	if req.IdempotencyKey != "" {
		replayed, lookupErr := s.LookupIdempotency(req.IdempotencyKey, result)
		if lookupErr == nil && replayed && result.ID != "" {
			ticketID = model.TicketID(result.ID)
		}
	}
	batchID := model.BatchID(missis.NewID("batch"))
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	actor := parseActor(req.Actor)
	events := []model.Event{
		missis.NewEvent(stream, model.OpCreateEntity, model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, model.Value{}, actor, now, req.EffectiveAt, batchID, ""),
		missis.PartEvent(stream, "title", opts.Title, model.ValueKindText, actor, now, req.EffectiveAt, batchID),
		missis.PartEvent(stream, "status", "open", model.ValueKindStatus, actor, now, req.EffectiveAt, batchID),
	}
	if opts.Priority != "" {
		events = append(events, missis.PartEvent(stream, "priority", opts.Priority, model.ValueKindPriority, actor, now, req.EffectiveAt, batchID))
	}
	if len(opts.Types) > 0 {
		events = append(events, missis.PartEvent(stream, "type", opts.Types, model.ValueKindList, actor, now, req.EffectiveAt, batchID))
	}
	if len(opts.Tags) > 0 {
		events = append(events, missis.PartEvent(stream, "tag", opts.Tags, model.ValueKindList, actor, now, req.EffectiveAt, batchID))
	}
	if opts.Project != "" {
		projectEvents, err := s.LoadStreamEvents(ctx, model.Ref{Kind: model.KindProject, Entity: opts.Project})
		if err != nil {
			return missis.NewTicketResult{}, keepStorage(err)
		}
		if len(projectEvents) == 0 {
			return missis.NewTicketResult{}, validation("project does not exist: project:%s; create it with: missis new --kind project --id %s", opts.Project, opts.Project)
		}
		projectRef := model.Ref{Kind: model.KindProject, Entity: opts.Project}
		events = append(events, missis.NewEvent(
			stream, model.OpAssertLink,
			model.Ref{Kind: model.KindTicket, Entity: string(ticketID)},
			model.Value{Text: "has-home", Ref: &projectRef},
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
			Title:      opts.Title,
			Status:     "open",
			Project:    stringPtrOrNil(opts.Project),
			RecordedAt: now.Format(time.RFC3339),
		}
	}
	if req.IdempotencyKey != "" {
		if err := s.UpdateIdempotencyResult(req.IdempotencyKey, result); err != nil {
			return missis.NewTicketResult{}, keepStorage(err)
		}
	}
	return *result, nil
}

func (s *Service) NewEntity(ctx context.Context, req missis.RequestContext, opts missis.EntityOptions) (missis.EntityResult, error) {
	req, now := s.normalize(req)
	if opts.Kind != "project" && opts.Kind != "group" {
		return missis.EntityResult{}, invalidInput("invalid kind: %s", opts.Kind)
	}
	if opts.ID == "" {
		return missis.EntityResult{}, invalidInput("--id is required for project or group")
	}
	if err := model.ValidatePathSegments([]string{opts.ID}); err != nil {
		return missis.EntityResult{}, validation("%v", err)
	}
	existing, err := s.LoadEvents(ctx)
	if err != nil {
		return missis.EntityResult{}, keepStorage(err)
	}
	kind := model.Kind(opts.Kind)
	for _, event := range existing {
		if event.Target.Kind == kind && event.Target.Entity == opts.ID {
			return missis.EntityResult{}, validation("%s already exists: %s", opts.Kind, opts.ID)
		}
	}
	stream := model.Ref{Kind: kind, Entity: opts.ID}
	event := model.Event{
		ID:          model.EventID(missis.NewID("event")),
		Stream:      stream,
		Operation:   model.OpCreateEntity,
		Target:      model.Ref{Kind: kind, Entity: opts.ID},
		Value:       model.Value{Kind: model.ValueKindText, Text: opts.Title},
		RecordedAt:  now,
		EffectiveAt: req.EffectiveAt,
		Actor:       parseActor(req.Actor),
	}
	result := &missis.EntityResult{
		Ref:        opts.Kind + ":" + opts.ID,
		ID:         opts.Kind + ":" + opts.ID,
		Title:      opts.Title,
		Status:     "open",
		RecordedAt: now.Format(time.RFC3339),
	}
	if _, err := s.AppendBatch(ctx, []model.Event{event}, req.IdempotencyKey, nil, result); err != nil {
		return missis.EntityResult{}, keepStorage(err)
	}
	return *result, nil
}

func (s *Service) ImportMarkdown(ctx context.Context, req missis.RequestContext, opts missis.ImportOptions) (missis.NewTicketResult, error) {
	req, now := s.normalize(req)
	parts, err := model.ParseMarkdownParts(opts.Content)
	if err != nil {
		return missis.NewTicketResult{}, validation("%v", err)
	}
	title := opts.Title
	if title == "" {
		for i, part := range parts {
			if len(part.Path) == 1 && part.Path[0] != "preamble" {
				title = part.Path[0]
				parts = append(parts[:i], parts[i+1:]...)
				break
			}
		}
	}
	if title == "" {
		title = artifactTitle(opts.Artifact)
	}
	ticketID := model.TicketID(missis.NewID("ticket"))
	result := &missis.NewTicketResult{}
	if req.IdempotencyKey != "" {
		replayed, lookupErr := s.LookupIdempotency(req.IdempotencyKey, result)
		if lookupErr == nil && replayed && result.ID != "" {
			ticketID = model.TicketID(result.ID)
		}
	}
	batchID := model.BatchID(missis.NewID("batch"))
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	actor := parseActor(req.Actor)
	events := []model.Event{
		missis.NewEvent(stream, model.OpCreateEntity, model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, model.Value{}, actor, now, req.EffectiveAt, batchID, ""),
		missis.PartEvent(stream, "title", title, model.ValueKindText, actor, now, req.EffectiveAt, batchID),
		missis.PartEvent(stream, "status", "open", model.ValueKindStatus, actor, now, req.EffectiveAt, batchID),
	}
	events = append(events, buildImportEvents(stream, parts, actor, now, req.EffectiveAt, batchID, opts.Artifact)...)
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
			Project:    stringPtrOrNil(opts.Project),
			RecordedAt: now.Format(time.RFC3339),
		}
	}
	if req.IdempotencyKey != "" {
		if err := s.UpdateIdempotencyResult(req.IdempotencyKey, result); err != nil {
			return missis.NewTicketResult{}, keepStorage(err)
		}
	}
	return *result, nil
}

func (s *Service) ReimportMarkdown(ctx context.Context, req missis.RequestContext, opts missis.ImportOptions) (missis.ImportResult, error) {
	req, now := s.normalize(req)
	parts, err := model.ParseMarkdownParts(opts.Content)
	if err != nil {
		return missis.ImportResult{}, validation("%v", err)
	}
	for i, part := range parts {
		if len(part.Path) == 1 && part.Path[0] != "preamble" {
			parts = append(parts[:i], parts[i+1:]...)
			break
		}
	}
	ticketID, partPath, err := s.resolveTicketRef(ctx, opts.Ref, req.EffectiveAt)
	if err != nil {
		return missis.ImportResult{}, err
	}
	if len(partPath) != 0 {
		return missis.ImportResult{}, invalidInput("import target must be a ticket reference")
	}
	batchID := model.BatchID(missis.NewID("batch"))
	actor := parseActor(req.Actor)
	proj, err := s.CurrentProjection(ctx, ticketID, req.EffectiveAt)
	if err != nil {
		return missis.ImportResult{}, keepStorage(err)
	}
	events, err := buildReimportEvents(proj, ticketID, parts, actor, now, req.EffectiveAt, batchID, opts.Artifact)
	if err != nil {
		return missis.ImportResult{}, validation("%v", err)
	}
	if len(events) == 0 {
		return missis.ImportResult{Ref: opts.Ref, Operation: "import", Value: 0}, nil
	}
	// All-or-nothing: validate every proposed part before appending anything.
	if err := s.validateImportEvents(ctx, model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, events, req.EffectiveAt, now); err != nil {
		return missis.ImportResult{}, err
	}
	result := missis.ImportResult{Ref: opts.Ref, Operation: "import", Value: len(events)}
	outcome, err := s.AppendBatch(ctx, events, req.IdempotencyKey, nil, &result)
	if err != nil {
		return missis.ImportResult{}, keepStorage(err)
	}
	if len(outcome.Events) > 0 {
		last := outcome.Events[len(outcome.Events)-1]
		result.Event = "@e" + strconv.FormatUint(last.AliasSeq, 10)
	}
	return result, nil
}

// ----- read workflows -----

func (s *Service) ListTicketSummaries(ctx context.Context, effectiveAt time.Time) ([]missis.TicketSummary, error) {
	if effectiveAt.IsZero() {
		effectiveAt = s.now()
	}
	items, err := s.Store().ListTickets(effectiveAt)
	if err != nil {
		return nil, keepStorage(err)
	}
	out := make([]missis.TicketSummary, 0, len(items))
	for _, item := range items {
		out = append(out, missis.TicketSummary{
			Ref:        item.Ref,
			ID:         string(item.ID),
			Title:      item.Title,
			Status:     item.Status,
			RecordedAt: item.RecordedAt,
		})
	}
	return out, nil
}

func (s *Service) ShowTicket(ctx context.Context, ref string, opts missis.ShowOptions) (missis.TicketProjection, error) {
	if opts.EffectiveAt.IsZero() {
		opts.EffectiveAt = s.now()
	}
	if opts.KnownAt.IsZero() {
		opts.KnownAt = opts.EffectiveAt
	}
	summary, err := s.findTicketSummary(ctx, ref)
	if err != nil {
		return missis.TicketProjection{}, err
	}
	proj, err := s.BitemporalProjection(ctx, model.TicketID(summary.ID), opts.EffectiveAt, opts.KnownAt)
	if err != nil {
		return missis.TicketProjection{}, keepStorage(err)
	}
	parts := partsFromProjection(proj)
	if err := s.decorateParts(ctx, model.Ref{Kind: model.KindTicket, Entity: summary.ID}, parts, opts.EffectiveAt, opts.KnownAt); err != nil {
		return missis.TicketProjection{}, err
	}
	title, status := projectionTitleStatus(proj)
	return missis.TicketProjection{
		Ref:        summary.Ref,
		ID:         summary.ID,
		Title:      title,
		Status:     status,
		RecordedAt: summary.RecordedAt,
		Parts:      parts,
	}, nil
}

func (s *Service) ShowEntity(ctx context.Context, ref string, opts missis.ShowOptions) (missis.TicketProjection, error) {
	if opts.EffectiveAt.IsZero() {
		opts.EffectiveAt = s.now()
	}
	if opts.KnownAt.IsZero() {
		opts.KnownAt = opts.EffectiveAt
	}
	if !strings.HasPrefix(ref, "project:") && !strings.HasPrefix(ref, "group:") {
		return missis.TicketProjection{}, invalidInput("ShowEntity requires a project or group reference")
	}
	return s.showScope(ctx, ref, opts)
}

func (s *Service) showScope(ctx context.Context, ref string, opts missis.ShowOptions) (missis.TicketProjection, error) {
	stream, _, err := s.resolveStreamRef(ctx, ref, opts.EffectiveAt)
	if err != nil {
		return missis.TicketProjection{}, err
	}
	events, err := s.LoadStreamEvents(ctx, stream)
	if err != nil {
		return missis.TicketProjection{}, keepStorage(err)
	}
	proj, err := s.BitemporalStreamProjection(ctx, stream, opts.EffectiveAt, opts.KnownAt)
	if err != nil {
		return missis.TicketProjection{}, keepStorage(err)
	}
	title, status := projectionTitleStatus(proj)
	var recordedAt time.Time
	for _, event := range events {
		if event.Operation != model.OpCreateEntity {
			continue
		}
		if title == "" {
			title = event.Value.Text
		}
		if recordedAt.IsZero() {
			recordedAt = event.RecordedAt
		}
	}
	return missis.TicketProjection{
		Ref:        ref,
		ID:         stream.Entity,
		Title:      title,
		Status:     status,
		RecordedAt: recordedAt,
		Parts:      partsFromProjection(proj),
	}, nil
}

func partsFromProjection(proj *model.Projection) map[string]missis.PartView {
	parts := make(map[string]missis.PartView, len(proj.Paths))
	for path, partID := range proj.Paths {
		part := proj.Parts[partID]
		if part == nil {
			continue
		}
		parts[path] = missis.PartView{
			ID:          string(part.ID),
			Path:        path,
			Value:       valueOrNilFromPart(part),
			ValueKind:   string(part.ValueKind),
			ParentID:    parentIDOrNil(part),
			CreatedBy:   string(part.CreatedBy),
			Name:        part.Name,
			DisplayName: part.DisplayName,
		}
	}
	return parts
}

func (s *Service) ShowHistory(ctx context.Context, ref string, opts missis.HistoryOptions) ([]missis.EventView, error) {
	if opts.EffectiveAt.IsZero() {
		opts.EffectiveAt = s.now()
	}
	if opts.KnownAt.IsZero() {
		opts.KnownAt = opts.EffectiveAt
	}
	var events []model.Event
	if strings.HasPrefix(ref, "project:") || strings.HasPrefix(ref, "group:") {
		stream, _, err := s.resolveStreamRef(ctx, ref, opts.EffectiveAt)
		if err != nil {
			return nil, err
		}
		events, err = s.LoadStreamEvents(ctx, stream)
		if err != nil {
			return nil, keepStorage(err)
		}
	} else {
		summary, err := s.findTicketSummary(ctx, ref)
		if err != nil {
			return nil, err
		}
		events, err = s.LoadTicketEvents(ctx, model.TicketID(summary.ID))
		if err != nil {
			return nil, keepStorage(err)
		}
	}
	return historyViews(events, opts), nil
}

func historyViews(events []model.Event, opts missis.HistoryOptions) []missis.EventView {
	out := make([]missis.EventView, 0, len(events))
	for _, event := range events {
		if event.EffectiveAt.After(opts.EffectiveAt) || event.RecordedAt.After(opts.KnownAt) {
			continue
		}
		if !opts.Since.IsZero() && event.RecordedAt.Before(opts.Since) {
			continue
		}
		if len(opts.PartPath) > 0 {
			if event.Target.Kind != model.KindPart || !equalPaths(event.Target.Path, opts.PartPath) {
				continue
			}
		}
		out = append(out, missis.EventView{
			ID:          string(event.ID),
			Alias:       "@e" + strconv.FormatUint(event.AliasSeq, 10),
			Sequence:    event.Sequence,
			Operation:   string(event.Operation),
			Target:      targetText(event.Target),
			Value:       valueText(event.Value),
			RecordedAt:  event.RecordedAt,
			EffectiveAt: event.EffectiveAt,
			Actor:       event.Actor.ID,
			Reason:      event.Reason,
		})
	}
	return out
}

func (s *Service) ShowEvent(ctx context.Context, alias string) (missis.EventView, error) {
	event, err := s.GetEventByAlias(ctx, alias)
	if err != nil {
		return missis.EventView{}, notFound("%v", err)
	}
	return missis.EventView{
		ID:          string(event.ID),
		Alias:       "@e" + strconv.FormatUint(event.AliasSeq, 10),
		Sequence:    event.Sequence,
		Operation:   string(event.Operation),
		Target:      targetText(event.Target),
		Value:       valueText(event.Value),
		RecordedAt:  event.RecordedAt,
		EffectiveAt: event.EffectiveAt,
		Actor:       event.Actor.ID,
		Reason:      event.Reason,
	}, nil
}

func (s *Service) ShowReferences(ctx context.Context, ref string, opts missis.ShowOptions) ([]missis.LinkView, error) {
	if opts.EffectiveAt.IsZero() {
		opts.EffectiveAt = s.now()
	}
	if opts.KnownAt.IsZero() {
		opts.KnownAt = opts.EffectiveAt
	}
	target, err := s.resolveAnyRef(ctx, ref, opts.EffectiveAt)
	if err != nil {
		return nil, err
	}
	events, err := s.LoadLinkEvents(ctx)
	if err != nil {
		return nil, keepStorage(err)
	}
	links, err := model.LinksForRef(events, target, opts.EffectiveAt, opts.KnownAt)
	if err != nil {
		return nil, keepStorage(err)
	}
	out := make([]missis.LinkView, 0, len(links))
	for _, link := range links {
		from, err := s.currentDisplayRef(ctx, link.From, opts.EffectiveAt)
		if err != nil {
			return nil, err
		}
		to, err := s.currentDisplayRef(ctx, link.To, opts.EffectiveAt)
		if err != nil {
			return nil, err
		}
		out = append(out, missis.LinkView{
			From:      targetText(from),
			Relation:  link.Relation,
			To:        targetText(to),
			Direction: link.Direction,
			Origin:    link.Origin,
			CreatedBy: string(link.CreatedBy),
		})
	}
	return out, nil
}

// currentDisplayRef resolves a part ref to its current path at the effective
// time. Non-part refs and parts whose stream or path is unavailable keep the
// stored ref so historical aliases remain visible.
func (s *Service) currentDisplayRef(ctx context.Context, ref model.Ref, effectiveAt time.Time) (model.Ref, error) {
	if ref.Kind != model.KindPart {
		return ref, nil
	}
	stream, err := s.findStreamForPart(ctx, model.PartID(ref.Entity))
	if err != nil {
		return ref, nil
	}
	proj, err := s.CurrentStreamProjection(ctx, stream, effectiveAt)
	if err != nil {
		return ref, nil
	}
	if path := currentPathForPart(proj, model.PartID(ref.Entity)); len(path) > 0 {
		ref.Path = path
	}
	return ref, nil
}

func (s *Service) ShowLineage(ctx context.Context, ref string, opts missis.LineageOptions) ([]missis.LineageEdge, error) {
	if opts.EffectiveAt.IsZero() {
		opts.EffectiveAt = s.now()
	}
	if opts.KnownAt.IsZero() {
		opts.KnownAt = opts.EffectiveAt
	}
	target, err := s.resolveAnyRef(ctx, ref, opts.EffectiveAt)
	if err != nil {
		return nil, err
	}
	events, err := s.LoadLinkEvents(ctx)
	if err != nil {
		return nil, keepStorage(err)
	}
	relationSet := make(map[string]bool)
	for _, relation := range opts.Relations {
		relation = strings.TrimSpace(relation)
		if relation == "" {
			continue
		}
		if !model.ValidRelation(relation) {
			return nil, validation("unsupported relation: %s", relation)
		}
		relationSet[relation] = true
	}
	graph, err := model.BuildLineageGraph(events, opts.EffectiveAt, opts.KnownAt)
	if err != nil {
		return nil, keepStorage(err)
	}
	edges, err := graph.Walk(target, opts.Direction, opts.Depth, relationSet)
	if err != nil {
		return nil, invalidInput("%v", err)
	}
	out := make([]missis.LineageEdge, 0, len(edges))
	for _, edge := range edges {
		out = append(out, missis.LineageEdge{
			From:      targetText(edge.From),
			Relation:  edge.Relation,
			To:        targetText(edge.To),
			Direction: edge.Direction,
			Depth:     edge.Depth,
			Origin:    edge.Origin,
			CreatedBy: string(edge.CreatedBy),
		})
	}
	return out, nil
}

func (s *Service) ResolveAnyRef(ctx context.Context, ref string, effectiveAt time.Time) (string, error) {
	resolved, err := s.resolveAnyRef(ctx, ref, effectiveAt)
	if err != nil {
		return "", err
	}
	return targetText(resolved), nil
}

func (s *Service) Search(ctx context.Context, opts missis.SearchOptions) ([]missis.TicketSummary, error) {
	return s.ListTicketsFiltered(ctx, missis.ListFilter{
		Project:     opts.Project,
		Group:       opts.Group,
		Status:      opts.Status,
		Type:        opts.Type,
		Tag:         opts.Tag,
		Query:       opts.Query,
		EffectiveAt: opts.EffectiveAt,
		KnownAt:     opts.KnownAt,
	})
}

func (s *Service) ListEntities(ctx context.Context, kind model.Kind, filter missis.ListFilter) ([]missis.EntitySummary, error) {
	if kind != model.KindProject && kind != model.KindGroup {
		return nil, invalidInput("kind must be project or group")
	}
	if filter.EffectiveAt.IsZero() {
		filter.EffectiveAt = s.now()
	}
	if filter.KnownAt.IsZero() {
		filter.KnownAt = filter.EffectiveAt
	}
	events, err := s.LoadEvents(ctx)
	if err != nil {
		return nil, keepStorage(err)
	}
	type entityMeta struct {
		title      string
		recordedAt time.Time
	}
	meta := make(map[string]entityMeta)
	var ids []string
	for _, event := range events {
		if event.Stream.Kind != kind || event.Operation != model.OpCreateEntity {
			continue
		}
		if _, ok := meta[event.Stream.Entity]; ok {
			continue
		}
		meta[event.Stream.Entity] = entityMeta{title: event.Value.Text, recordedAt: event.RecordedAt}
		ids = append(ids, event.Stream.Entity)
	}
	sort.Strings(ids)
	out := make([]missis.EntitySummary, 0, len(ids))
	for _, id := range ids {
		stream := model.Ref{Kind: kind, Entity: id}
		proj, err := s.BitemporalStreamProjection(ctx, stream, filter.EffectiveAt, filter.KnownAt)
		if err != nil {
			return nil, keepStorage(err)
		}
		title, status := projectionTitleStatus(proj)
		if title == "" {
			title = meta[id].title
		}
		summary := missis.EntitySummary{
			Ref:        string(kind) + ":" + id,
			ID:         string(kind) + ":" + id,
			Title:      title,
			Status:     status,
			RecordedAt: meta[id].recordedAt,
		}
		if filter.Status != "" && !csvContains(filter.Status, summary.Status) {
			continue
		}
		if filter.Query != "" {
			text := summary.Title + " " + projectionText(proj)
			if !matchesAllTokens(text, filter.Query) {
				continue
			}
		}
		out = append(out, summary)
	}
	return out, nil
}

func (s *Service) ListTicketsFiltered(ctx context.Context, filter missis.ListFilter) ([]missis.TicketSummary, error) {
	if filter.EffectiveAt.IsZero() {
		filter.EffectiveAt = s.now()
	}
	if filter.KnownAt.IsZero() {
		filter.KnownAt = filter.EffectiveAt
	}
	summaries, err := s.ListTicketSummaries(ctx, filter.EffectiveAt)
	if err != nil {
		return nil, err
	}
	var linkEvents []model.Event
	if filter.Project != "" || filter.Group != "" {
		linkEvents, err = s.LoadLinkEvents(ctx)
		if err != nil {
			return nil, keepStorage(err)
		}
	}
	projectTicketIDs := make(map[model.TicketID]bool)
	if filter.Project != "" {
		for _, projectID := range strings.Split(filter.Project, ",") {
			links, err := model.LinksForRef(linkEvents, model.Ref{Kind: model.KindProject, Entity: strings.TrimSpace(projectID)}, filter.EffectiveAt, filter.KnownAt)
			if err != nil {
				return nil, keepStorage(err)
			}
			for _, link := range links {
				if link.Relation == "contains" && link.Direction == "asserted" && link.To.Kind == model.KindTicket {
					projectTicketIDs[model.TicketID(link.To.Entity)] = true
				}
				if link.Relation == "home-of" && link.Direction == "derived-inverse" && link.To.Kind == model.KindTicket {
					projectTicketIDs[model.TicketID(link.To.Entity)] = true
				}
			}
		}
	}
	if filter.Group != "" {
		for _, groupID := range strings.Split(filter.Group, ",") {
			groupRef := model.Ref{Kind: model.KindGroup, Entity: strings.TrimSpace(groupID)}
			groupLinks, err := model.LinksForRef(linkEvents, groupRef, filter.EffectiveAt, filter.KnownAt)
			if err != nil {
				return nil, keepStorage(err)
			}
			projectIDs := make(map[string]bool)
			for _, link := range groupLinks {
				if link.Direction == "asserted" && link.Relation == "contains" && link.To.Kind == model.KindTicket {
					projectTicketIDs[model.TicketID(link.To.Entity)] = true
				}
				if link.Direction == "asserted" && (link.Relation == "contains" || link.Relation == "governs") && link.To.Kind == model.KindProject {
					projectIDs[link.To.Entity] = true
				}
			}
			for projectID := range projectIDs {
				links, err := model.LinksForRef(linkEvents, model.Ref{Kind: model.KindProject, Entity: projectID}, filter.EffectiveAt, filter.KnownAt)
				if err != nil {
					return nil, keepStorage(err)
				}
				for _, link := range links {
					if link.Relation == "contains" && link.Direction == "asserted" && link.To.Kind == model.KindTicket {
						projectTicketIDs[model.TicketID(link.To.Entity)] = true
					}
					if link.Relation == "home-of" && link.Direction == "derived-inverse" && link.To.Kind == model.KindTicket {
						projectTicketIDs[model.TicketID(link.To.Entity)] = true
					}
				}
			}
		}
	}
	result := make([]missis.TicketSummary, 0, len(summaries))
	for _, summary := range summaries {
		if filter.Status != "" && !csvContains(filter.Status, summary.Status) {
			continue
		}
		if (filter.Project != "" || filter.Group != "") && !projectTicketIDs[model.TicketID(summary.ID)] {
			continue
		}
		proj, err := s.BitemporalProjection(ctx, model.TicketID(summary.ID), filter.EffectiveAt, filter.KnownAt)
		if err != nil {
			return nil, keepStorage(err)
		}
		if filter.Query != "" {
			text := summary.Title + " " + projectionText(proj)
			if !matchesAllTokens(text, filter.Query) {
				continue
			}
		}
		if filter.Type != "" && !partHasValue(proj, "type", filter.Type) {
			continue
		}
		if filter.Tag != "" && !partHasValue(proj, "tag", filter.Tag) {
			continue
		}
		result = append(result, summary)
	}
	return result, nil
}

// ----- mutation workflows -----

type setMode int

const (
	modeSet setMode = iota
	modeAdd
	modeRetract
	modeRetractSubtree
	modeRename
	modeMove
	modeSupersede
)

type setSpec struct {
	mode       setMode
	target     string
	value      string
	kind       model.ValueKind
	name       string
	parent     string
	supersedes string
	reason     string
}

func (s *Service) Set(ctx context.Context, req missis.RequestContext, mutation missis.Mutation) (missis.SetResult, error) {
	req, now := s.normalize(req)
	var spec setSpec
	switch m := mutation.(type) {
	case missis.SetValue:
		spec = setSpec{mode: modeSet, target: m.Target, value: m.Value, kind: m.Kind, reason: m.Reason}
	case missis.AddValue:
		spec = setSpec{mode: modeAdd, target: m.Target, value: m.Value, reason: m.Reason}
	case missis.RetractValue:
		spec = setSpec{mode: modeRetract, target: m.Target, reason: m.Reason}
	case missis.RetractSubtree:
		spec = setSpec{mode: modeRetractSubtree, target: m.Target, reason: m.Reason}
	case missis.RenamePart:
		spec = setSpec{mode: modeRename, target: m.Target, name: m.Name, reason: m.Reason}
	case missis.MovePart:
		spec = setSpec{mode: modeMove, target: m.Target, parent: m.Parent, reason: m.Reason}
	case missis.SupersedeEvent:
		spec = setSpec{mode: modeSupersede, target: m.Target, value: m.Value, kind: m.Kind, supersedes: m.Supersedes, reason: m.Reason}
	default:
		return missis.SetResult{}, invalidInput("unsupported mutation")
	}
	return s.applySet(ctx, req, now, parseActor(req.Actor), model.BatchID(missis.NewID("batch")), spec)
}

func (s *Service) applySet(ctx context.Context, req missis.RequestContext, now time.Time, actor model.ActorRef, batchID model.BatchID, spec setSpec) (missis.SetResult, error) {
	var (
		partID         model.PartID
		currentPath    []string
		creationEvents []model.Event
		partExisted    bool
		stream         model.Ref
	)
	requiresExisting := spec.mode == modeRetract || spec.mode == modeRetractSubtree ||
		spec.mode == modeRename || spec.mode == modeMove
	if requiresExisting {
		var err error
		stream, partID, currentPath, err = s.resolvePartRef(ctx, spec.target, req.EffectiveAt)
		if err != nil {
			return missis.SetResult{}, err
		}
		partExisted = true
	} else {
		var err error
		stream, currentPath, err = s.resolveStreamRef(ctx, spec.target, req.EffectiveAt)
		if err != nil {
			return missis.SetResult{}, err
		}
		if len(currentPath) == 0 {
			return missis.SetResult{}, invalidInput("part reference required")
		}
		creationEvents, partID, partExisted, err = s.ensurePartPath(ctx, stream, currentPath, actor, now, req.EffectiveAt, batchID)
		if err != nil {
			return missis.SetResult{}, keepStorage(err)
		}
	}
	target := model.Ref{Kind: model.KindPart, Entity: string(partID), Path: currentPath}
	event := model.Event{
		Stream:      stream,
		Target:      target,
		RecordedAt:  now,
		EffectiveAt: req.EffectiveAt,
		Actor:       actor,
		Reason:      spec.reason,
		BatchID:     &batchID,
	}
	// Declaration writes on scope entities are validated eagerly so a
	// malformed declaration can never land in the store.
	if (stream.Kind == model.KindProject || stream.Kind == model.KindGroup) &&
		len(currentPath) > 0 && currentPath[0] == "schema" {
		if _, _, err := schema.ParseDeclarationPath(currentPath[1:]); err != nil {
			return missis.SetResult{}, validation("%v", err)
		}
		if _, err := schema.ParseKind(spec.value); err != nil {
			return missis.SetResult{}, validation("%v", err)
		}
	}
	switch spec.mode {
	case modeRetractSubtree:
		event.Operation = model.OpRetractSubtree
	case modeRetract:
		event.Operation = model.OpRetractValue
	case modeRename:
		if err := model.ValidatePathSegments([]string{spec.name}); err != nil {
			return missis.SetResult{}, validation("%v", err)
		}
		event.Operation = model.OpRenamePart
		event.Value = model.Value{Kind: model.ValueKindText, Text: spec.name}
	case modeMove:
		parentRef, err := s.resolveParentRef(ctx, spec.parent, req.EffectiveAt)
		if err != nil {
			return missis.SetResult{}, err
		}
		event.Operation = model.OpMovePart
		event.Value = model.Value{Ref: &parentRef}
	case modeSupersede:
		oldEvent, err := s.GetEventByAlias(ctx, spec.supersedes)
		if err != nil {
			return missis.SetResult{}, notFound("%v", err)
		}
		event.Supersedes = append(event.Supersedes, oldEvent.ID)
		event.Operation = model.OpSupersedeEvent
		valueKind, err := s.resolveWriteKind(ctx, stream, currentPath, spec.kind, model.Value{Kind: spec.kind, Text: spec.value}, nil, req.EffectiveAt, now)
		if err != nil {
			return missis.SetResult{}, err
		}
		event.Value = model.Value{Kind: valueKind, Text: spec.value}
		if valueKind == model.ValueKindList || valueKind == model.ValueKindJSON {
			event.Value.Data = spec.value
		}
	default:
		if spec.mode == modeAdd {
			valueKind, err := s.resolveWriteKind(ctx, stream, currentPath, model.ValueKindList, model.Value{Kind: model.ValueKindList, Text: spec.value}, []string{spec.value}, req.EffectiveAt, now)
			if err != nil {
				return missis.SetResult{}, err
			}
			event.Operation = model.OpAddValue
			event.Value = model.Value{Kind: valueKind, Text: spec.value}
			event.Value.Data = spec.value
		} else {
			valueKind, err := s.resolveWriteKind(ctx, stream, currentPath, spec.kind, model.Value{Kind: spec.kind, Text: spec.value}, nil, req.EffectiveAt, now)
			if err != nil {
				return missis.SetResult{}, err
			}
			event.Operation = model.OpSetValue
			event.Value = model.Value{Kind: valueKind, Text: spec.value}
			if valueKind == model.ValueKindList || valueKind == model.ValueKindJSON {
				event.Value.Data = spec.value
			}
		}
	}
	if req.Because != "" {
		causeRef, err := s.parseReference(ctx, req.Because, req.EffectiveAt)
		if err != nil {
			return missis.SetResult{}, err
		}
		event.Causes = append(event.Causes, causeRef)
	}
	if stream.Kind == model.KindTicket &&
		spec.mode != modeRetract && spec.mode != modeRetractSubtree &&
		spec.mode != modeRename && spec.mode != modeMove {
		if err := validateStatusSet(currentPath, spec.value, spec.reason); err != nil {
			return missis.SetResult{}, err
		}
	}
	var preconditions []store.Precondition
	if req.IfCurrent != "" {
		currentEvent, err := s.GetEventByAlias(ctx, req.IfCurrent)
		if err != nil {
			return missis.SetResult{}, notFound("%v", err)
		}
		if !partExisted {
			return missis.SetResult{}, conflict(fmt.Errorf("expected current event on new part"))
		}
		preconditions = append(preconditions, store.Precondition{
			TargetEntity:         string(partID),
			ExpectedCurrentEvent: currentEvent.ID,
		})
	}
	result := missis.SetResult{
		Ref:       spec.target,
		Operation: string(event.Operation),
		Value:     valueOrNil(event.Value),
	}
	outcome, err := s.AppendBatch(ctx, append(creationEvents, event), req.IdempotencyKey, preconditions, &result)
	if err != nil {
		return missis.SetResult{}, keepStorage(err)
	}
	if len(outcome.Events) > 0 {
		last := outcome.Events[len(outcome.Events)-1]
		result.Event = "@e" + strconv.FormatUint(last.AliasSeq, 10)
	}
	return result, nil
}

func (s *Service) SetLink(ctx context.Context, req missis.RequestContext, opts missis.LinkOptions) (missis.SetResult, error) {
	req, now := s.normalize(req)
	if !opts.Add && !opts.Retract {
		return missis.SetResult{}, invalidInput("link mutation requires --add or --retract")
	}
	if !model.ValidRelation(opts.Relation) {
		return missis.SetResult{}, validation("unsupported relation: %s", opts.Relation)
	}
	scope := strings.HasPrefix(opts.Ref, "project:") || strings.HasPrefix(opts.Ref, "group:")
	baseRef := strings.TrimSuffix(opts.Ref, "/links")
	var fromRef, toRef model.Ref
	var stream model.Ref
	var err error
	if scope {
		fromRef, err = s.resolveAnyRef(ctx, baseRef, req.EffectiveAt)
		if err != nil {
			return missis.SetResult{}, err
		}
		stream = model.Ref{Kind: fromRef.Kind, Entity: fromRef.Entity}
	} else {
		ticketID, _, resolveErr := s.resolveTicketRef(ctx, opts.Ref, req.EffectiveAt)
		if resolveErr != nil {
			return missis.SetResult{}, resolveErr
		}
		fromRef, err = s.resolveAnyRef(ctx, baseRef, req.EffectiveAt)
		if err != nil {
			return missis.SetResult{}, err
		}
		stream = model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	}
	toRef, err = s.resolveAnyRef(ctx, opts.Target, req.EffectiveAt)
	if err != nil {
		return missis.SetResult{}, err
	}
	exists, err := s.refExists(ctx, toRef)
	if err != nil {
		return missis.SetResult{}, err
	}
	if !exists {
		msg := fmt.Sprintf("link target does not exist: %s", opts.Target)
		if toRef.Kind == model.KindProject || toRef.Kind == model.KindGroup {
			msg += fmt.Sprintf("; create it with: missis new --kind %s --id %s", toRef.Kind, toRef.Entity)
		}
		return missis.SetResult{}, validation("%s", msg)
	}
	if err := s.validateLinkSchema(ctx, stream, toRef.Kind, opts.Relation, req.EffectiveAt, now); err != nil {
		return missis.SetResult{}, err
	}
	if opts.Relation == "has-home" {
		if fromRef.Kind != model.KindTicket || toRef.Kind != model.KindProject {
			return missis.SetResult{}, validation("has-home requires a ticket source and a project target")
		}
		linkEvents, err := s.LoadLinkEvents(ctx)
		if err != nil {
			return missis.SetResult{}, keepStorage(err)
		}
		links, err := model.LinksForRef(linkEvents, fromRef, req.EffectiveAt, now)
		if err != nil {
			return missis.SetResult{}, err
		}
		for _, link := range links {
			if link.Relation == "has-home" && link.Direction == "asserted" && link.To.Kind == model.KindProject {
				if opts.Add {
					return missis.SetResult{}, validation("ticket already has a home project: project:%s; retract it before assigning a new one", link.To.Entity)
				}
			}
		}
	}
	operation := model.OpAssertLink
	if opts.Retract {
		operation = model.OpRetractLink
	}
	event := model.Event{
		Stream:      stream,
		Operation:   operation,
		Target:      fromRef,
		Value:       model.Value{Text: opts.Relation, Ref: &toRef},
		RecordedAt:  now,
		EffectiveAt: req.EffectiveAt,
		Actor:       parseActor(req.Actor),
		Reason:      opts.Reason,
	}
	result := missis.SetResult{
		Ref:       opts.Ref,
		Operation: string(operation),
		Value:     opts.Relation + ":" + opts.Target,
	}
	outcome, err := s.AppendBatch(ctx, []model.Event{event}, req.IdempotencyKey, nil, &result)
	if err != nil {
		return missis.SetResult{}, keepStorage(err)
	}
	if len(outcome.Events) > 0 {
		last := outcome.Events[len(outcome.Events)-1]
		result.Event = "@e" + strconv.FormatUint(last.AliasSeq, 10)
	}
	if opts.Retract && opts.Relation == "has-home" && len(outcome.Events) > 0 {
		linkEvents, err := s.LoadLinkEvents(ctx)
		if err != nil {
			return missis.SetResult{}, keepStorage(err)
		}
		links, err := model.LinksForRef(linkEvents, fromRef, req.EffectiveAt, now)
		if err != nil {
			return missis.SetResult{}, err
		}
		hasHome := false
		for _, link := range links {
			if link.Relation == "has-home" && link.Direction == "asserted" && link.To.Kind == model.KindProject {
				hasHome = true
				break
			}
		}
		if !hasHome {
			result.Warning = "ticket no longer has a home project (zero-home state is allowed; consider assigning one with: missis set <ref>/links --add has-home:project:<id>)"
		}
	}
	return result, nil
}

func (s *Service) MoveLink(ctx context.Context, req missis.RequestContext, opts missis.MoveLinkOptions) (missis.SetResult, error) {
	req, now := s.normalize(req)
	if opts.Relation != "has-home" && opts.Relation != "contains" && opts.Relation != "governs" {
		return missis.SetResult{}, validation("move-link supports membership relations only: has-home, contains, governs")
	}
	fromRef, err := s.resolveAnyRef(ctx, opts.From, req.EffectiveAt)
	if err != nil {
		return missis.SetResult{}, err
	}
	toRef, err := s.resolveAnyRef(ctx, opts.To, req.EffectiveAt)
	if err != nil {
		return missis.SetResult{}, err
	}
	targetRef, err := s.resolveAnyRef(ctx, opts.Target, req.EffectiveAt)
	if err != nil {
		return missis.SetResult{}, err
	}
	if fromRef.Kind == toRef.Kind && fromRef.Entity == toRef.Entity {
		return missis.SetResult{}, validation("move-link source and destination must differ")
	}
	for _, ref := range []model.Ref{fromRef, toRef, targetRef} {
		exists, err := s.refExists(ctx, ref)
		if err != nil {
			return missis.SetResult{}, err
		}
		if !exists {
			return missis.SetResult{}, validation("move-link target does not exist: %s", targetText(ref))
		}
	}
	var originR, originA, retractOther, assertOther model.Ref
	switch opts.Relation {
	case "has-home":
		if targetRef.Kind != model.KindTicket || fromRef.Kind != model.KindProject || toRef.Kind != model.KindProject {
			return missis.SetResult{}, validation("has-home move requires a ticket target and project source/destination")
		}
		originR, originA = targetRef, targetRef
		retractOther, assertOther = fromRef, toRef
	case "contains":
		if !isScopeRef(fromRef) || !isScopeRef(toRef) {
			return missis.SetResult{}, validation("contains move requires project/group source and destination")
		}
		originR, originA = fromRef, toRef
		retractOther, assertOther = targetRef, targetRef
	case "governs":
		if fromRef.Kind != model.KindGroup || toRef.Kind != model.KindGroup || targetRef.Kind != model.KindProject {
			return missis.SetResult{}, validation("governs move requires group source/destination and project target")
		}
		originR, originA = fromRef, toRef
		retractOther, assertOther = targetRef, targetRef
	}

	linkEvents, err := s.LoadLinkEvents(ctx)
	if err != nil {
		return missis.SetResult{}, keepStorage(err)
	}
	views, err := model.LinksForRef(linkEvents, originR, req.EffectiveAt, now)
	if err != nil {
		return missis.SetResult{}, err
	}
	var currentEventID model.EventID
	for _, view := range views {
		if view.Direction == "asserted" && view.Relation == opts.Relation &&
			view.To.Kind == retractOther.Kind && view.To.Entity == retractOther.Entity {
			currentEventID = view.CreatedBy
			break
		}
	}
	if currentEventID == "" {
		return missis.SetResult{}, validation("no active %s assertion from %s to %s; nothing to move", opts.Relation, opts.From, opts.Target)
	}
	expected := currentEventID
	if opts.IfCurrent != "" {
		ev, err := s.GetEventByAlias(ctx, opts.IfCurrent)
		if err != nil {
			return missis.SetResult{}, notFound("%v", err)
		}
		if ev.ID != currentEventID {
			return missis.SetResult{}, conflict(fmt.Errorf("current %s assertion changed; re-read and retry", opts.Relation))
		}
		expected = ev.ID
	}

	originRCopy, originACopy := originR, originA
	retractOtherCopy, assertOtherCopy := retractOther, assertOther
	retractEvent := model.Event{
		Stream:      originRCopy,
		Operation:   model.OpRetractLink,
		Target:      originRCopy,
		Value:       model.Value{Text: opts.Relation, Ref: &retractOtherCopy},
		RecordedAt:  now,
		EffectiveAt: req.EffectiveAt,
		Actor:       parseActor(req.Actor),
		Reason:      opts.Reason,
	}
	assertEvent := model.Event{
		Stream:      originACopy,
		Operation:   model.OpAssertLink,
		Target:      originACopy,
		Value:       model.Value{Text: opts.Relation, Ref: &assertOtherCopy},
		RecordedAt:  now,
		EffectiveAt: req.EffectiveAt,
		Actor:       parseActor(req.Actor),
		Reason:      opts.Reason,
	}
	preconditions := []store.Precondition{{
		Link: &store.LinkPrecondition{
			From:                 originRCopy,
			Relation:             opts.Relation,
			To:                   retractOtherCopy,
			ExpectedCurrentEvent: expected,
		},
	}}
	result := missis.SetResult{
		Ref:       opts.Target,
		Operation: "move-link",
		Value:     fmt.Sprintf("%s:%s->%s", opts.Relation, targetText(fromRef), targetText(toRef)),
	}
	outcome, err := s.AppendBatch(ctx, []model.Event{retractEvent, assertEvent}, req.IdempotencyKey, preconditions, &result)
	if err != nil {
		return missis.SetResult{}, keepStorage(err)
	}
	if len(outcome.Events) > 0 {
		last := outcome.Events[len(outcome.Events)-1]
		result.Event = "@e" + strconv.FormatUint(last.AliasSeq, 10)
	}
	return result, nil
}

func isScopeRef(ref model.Ref) bool {
	return ref.Kind == model.KindProject || ref.Kind == model.KindGroup
}

func (s *Service) refExists(ctx context.Context, ref model.Ref) (bool, error) {
	switch ref.Kind {
	case model.KindProject, model.KindGroup:
		events, err := s.LoadStreamEvents(ctx, ref)
		if err != nil {
			return false, keepStorage(err)
		}
		return len(events) > 0, nil
	case model.KindTicket:
		events, err := s.LoadTicketEvents(ctx, model.TicketID(ref.Entity))
		if err != nil {
			return false, keepStorage(err)
		}
		return len(events) > 0, nil
	case model.KindPart, model.KindEvent:
		events, err := s.LoadEvents(ctx)
		if err != nil {
			return false, keepStorage(err)
		}
		for _, event := range events {
			if event.Target.Kind == ref.Kind && event.Target.Entity == ref.Entity {
				return true, nil
			}
			if ref.Kind == model.KindEvent && event.ID == model.EventID(ref.Entity) {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, nil
	}
}

// ----- shared helpers -----

func (s *Service) findTicketSummary(ctx context.Context, ref string) (missis.TicketSummary, error) {
	baseRef := strings.SplitN(ref, "/", 2)[0]
	summaries, err := s.ListTicketSummaries(ctx, s.now())
	if err != nil {
		return missis.TicketSummary{}, err
	}
	short := strings.TrimPrefix(baseRef, "#")
	for _, summary := range summaries {
		if summary.Ref == baseRef || summary.Ref == "#"+short || summary.ID == short || summary.ID == baseRef {
			return summary, nil
		}
	}
	return missis.TicketSummary{}, notFound("ticket not found: %s", ref)
}

func (s *Service) resolveTicketRef(ctx context.Context, ref string, effectiveAt time.Time) (model.TicketID, []string, error) {
	clean := strings.TrimPrefix(ref, "#")
	parts := strings.Split(clean, "/")
	short := parts[0]
	summaries, err := s.ListTickets(ctx, effectiveAt)
	if err != nil {
		return "", nil, keepStorage(err)
	}
	var ticketID model.TicketID
	for _, summary := range summaries {
		if summary.Ref == "#"+short || strconv.FormatUint(summary.Number, 10) == short || string(summary.ID) == short {
			ticketID = summary.ID
			break
		}
	}
	if ticketID == "" {
		if strings.HasPrefix(short, "ticket:") {
			ticketID = model.TicketID(short)
		} else {
			return "", nil, notFound("ticket not found: %s", short)
		}
	}
	var path []string
	if len(parts) > 1 {
		path = parts[1:]
	}
	return ticketID, path, nil
}

// resolveStreamRef resolves a ticket, project, or group reference with an
// optional part path into its stream ref and path.
func (s *Service) resolveStreamRef(ctx context.Context, ref string, effectiveAt time.Time) (model.Ref, []string, error) {
	parts := strings.Split(ref, "/")
	head := parts[0]
	if strings.HasPrefix(head, "project:") || strings.HasPrefix(head, "group:") {
		kind := model.KindProject
		if strings.HasPrefix(head, "group:") {
			kind = model.KindGroup
		}
		entity := strings.TrimPrefix(head, string(kind)+":")
		if entity == "" {
			return model.Ref{}, nil, notFound("unsupported reference: %s", ref)
		}
		var path []string
		if len(parts) > 1 {
			path = parts[1:]
		}
		return model.Ref{Kind: kind, Entity: entity}, path, nil
	}
	ticketID, path, err := s.resolveTicketRef(ctx, ref, effectiveAt)
	if err != nil {
		return model.Ref{}, nil, err
	}
	return model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, path, nil
}

func (s *Service) resolvePartRef(ctx context.Context, ref string, effectiveAt time.Time) (model.Ref, model.PartID, []string, error) {
	if strings.HasPrefix(ref, "part:") {
		partID := model.PartID(strings.TrimPrefix(ref, "part:"))
		stream, err := s.findStreamForPart(ctx, partID)
		if err != nil {
			return model.Ref{}, "", nil, err
		}
		proj, err := s.CurrentStreamProjection(ctx, stream, effectiveAt)
		if err != nil {
			return model.Ref{}, "", nil, keepStorage(err)
		}
		path := currentPathForPart(proj, partID)
		return stream, partID, path, nil
	}
	stream, path, err := s.resolveStreamRef(ctx, ref, effectiveAt)
	if err != nil {
		return model.Ref{}, "", nil, err
	}
	if len(path) == 0 {
		return model.Ref{}, "", nil, notFound("part reference required")
	}
	proj, err := s.CurrentStreamProjection(ctx, stream, effectiveAt)
	if err != nil {
		return model.Ref{}, "", nil, keepStorage(err)
	}
	key := strings.Join(path, "/")
	partID, ok := proj.Paths[key]
	if !ok {
		return model.Ref{}, "", nil, notFound("part path not found: %s", key)
	}
	return stream, partID, path, nil
}

func (s *Service) ensurePartPath(ctx context.Context, stream model.Ref, path []string, actor model.ActorRef, recordedAt, effectiveAt time.Time, batchID model.BatchID) ([]model.Event, model.PartID, bool, error) {
	proj, err := s.CurrentStreamProjection(ctx, stream, effectiveAt)
	if err != nil {
		return nil, "", false, keepStorage(err)
	}
	var (
		parentID    *model.PartID
		events      []model.Event
		partID      model.PartID
		existed     = true
		currentPath []string
	)
	for _, segment := range path {
		currentPath = append(currentPath, segment)
		key := strings.Join(currentPath, "/")
		if id, ok := proj.Paths[key]; ok {
			parentID = &id
			partID = id
			continue
		}
		existed = false
		newIDValue := model.PartID(missis.NewID("part"))
		target := model.Ref{Kind: model.KindPart, Entity: string(newIDValue), Path: append([]string(nil), currentPath...)}
		var parentRef *model.Ref
		if parentID != nil {
			parentRef = &model.Ref{Kind: model.KindPart, Entity: string(*parentID)}
		}
		event := model.Event{
			ID:          model.EventID(missis.NewID("event")),
			Stream:      stream,
			Operation:   model.OpCreatePart,
			Target:      target,
			RecordedAt:  recordedAt,
			EffectiveAt: effectiveAt,
			Actor:       actor,
			BatchID:     &batchID,
		}
		if parentRef != nil {
			event.Value = model.Value{Ref: parentRef}
		}
		events = append(events, event)
		parentID = &newIDValue
		partID = newIDValue
	}
	return events, partID, existed, nil
}

func (s *Service) findStreamForPart(ctx context.Context, partID model.PartID) (model.Ref, error) {
	events, err := s.LoadEvents(ctx)
	if err != nil {
		return model.Ref{}, keepStorage(err)
	}
	for _, event := range events {
		if event.Target.Kind == model.KindPart && event.Target.Entity == string(partID) {
			return event.Stream, nil
		}
	}
	return model.Ref{}, notFound("part not found: %s", partID)
}

func (s *Service) findTicketForPart(ctx context.Context, partID model.PartID) (model.TicketID, error) {
	stream, err := s.findStreamForPart(ctx, partID)
	if err != nil {
		return "", err
	}
	if stream.Kind != model.KindTicket {
		return "", notFound("part not in a ticket: %s", partID)
	}
	return model.TicketID(stream.Entity), nil
}

func (s *Service) resolveParentRef(ctx context.Context, ref string, effectiveAt time.Time) (model.Ref, error) {
	if strings.HasPrefix(ref, "part:") {
		return model.Ref{Kind: model.KindPart, Entity: strings.TrimPrefix(ref, "part:")}, nil
	}
	_, partID, _, err := s.resolvePartRef(ctx, ref, effectiveAt)
	if err != nil {
		return model.Ref{}, err
	}
	return model.Ref{Kind: model.KindPart, Entity: string(partID)}, nil
}

func (s *Service) parseReference(ctx context.Context, ref string, effectiveAt time.Time) (model.Ref, error) {
	if strings.HasPrefix(ref, "part:") {
		return model.Ref{Kind: model.KindPart, Entity: strings.TrimPrefix(ref, "part:")}, nil
	}
	if strings.HasPrefix(ref, "@") {
		event, err := s.GetEventByAlias(ctx, ref)
		if err != nil {
			return model.Ref{}, notFound("%v", err)
		}
		return model.Ref{Kind: model.KindEvent, Entity: string(event.ID)}, nil
	}
	_, partID, _, err := s.resolvePartRef(ctx, ref, effectiveAt)
	if err != nil {
		return model.Ref{}, err
	}
	return model.Ref{Kind: model.KindPart, Entity: string(partID)}, nil
}

func (s *Service) resolveAnyRef(ctx context.Context, ref string, effectiveAt time.Time) (model.Ref, error) {
	if strings.HasPrefix(ref, "part:") {
		return model.Ref{Kind: model.KindPart, Entity: strings.TrimPrefix(ref, "part:")}, nil
	}
	if strings.HasPrefix(ref, "ticket:") {
		return model.Ref{Kind: model.KindTicket, Entity: strings.TrimPrefix(ref, "ticket:")}, nil
	}
	if strings.HasPrefix(ref, "project:") {
		return model.Ref{Kind: model.KindProject, Entity: strings.TrimPrefix(ref, "project:")}, nil
	}
	if strings.HasPrefix(ref, "group:") {
		return model.Ref{Kind: model.KindGroup, Entity: strings.TrimPrefix(ref, "group:")}, nil
	}
	if strings.HasPrefix(ref, "@") {
		event, err := s.GetEventByAlias(ctx, ref)
		if err != nil {
			return model.Ref{}, notFound("%v", err)
		}
		return model.Ref{Kind: model.KindEvent, Entity: string(event.ID)}, nil
	}
	if strings.HasPrefix(ref, "#") {
		ticket, path, err := s.resolveTicketRef(ctx, ref, effectiveAt)
		if err != nil {
			return model.Ref{}, err
		}
		if len(path) == 0 {
			return model.Ref{Kind: model.KindTicket, Entity: string(ticket)}, nil
		}
		_, partID, _, err := s.resolvePartRef(ctx, ref, effectiveAt)
		if err != nil {
			return model.Ref{}, err
		}
		return model.Ref{Kind: model.KindPart, Entity: string(partID), Path: path}, nil
	}
	return model.Ref{}, notFound("unsupported reference: %s", ref)
}

func parseActor(value string) model.ActorRef {
	kind := "human"
	if idx := strings.IndexByte(value, '/'); idx > 0 {
		kind = value[:idx]
	}
	return model.ActorRef{Kind: kind, ID: value, Name: value}
}

func artifactTitle(artifact string) string {
	if artifact == "artifact:stdin" {
		return "stdin"
	}
	if strings.HasPrefix(artifact, "artifact:") {
		return filepath.Base(strings.TrimPrefix(artifact, "artifact:"))
	}
	return "stdin"
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func validateStatusSet(path []string, value, reason string) error {
	if len(path) == 0 || path[len(path)-1] != "status" {
		return nil
	}
	switch value {
	case "open", "doing", "done":
		return nil
	case "blocked":
		if strings.TrimSpace(reason) == "" {
			return validation("blocked status requires a reason")
		}
		return nil
	default:
		return validation("invalid status: %s", value)
	}
}

func csvContains(csv, value string) bool {
	for _, candidate := range strings.Split(csv, ",") {
		if strings.EqualFold(strings.TrimSpace(candidate), value) {
			return true
		}
	}
	return false
}

func projectionText(proj *model.Projection) string {
	var b strings.Builder
	for _, part := range proj.Parts {
		if part == nil || part.Value == nil {
			continue
		}
		b.WriteString(fmt.Sprint(valueText(*part.Value)))
		b.WriteByte(' ')
	}
	return b.String()
}

func projectionTitleStatus(proj *model.Projection) (string, string) {
	var title, status string
	if id, ok := proj.Paths["title"]; ok {
		if part := proj.Parts[id]; part != nil && part.Value != nil {
			title = part.Value.Text
		}
	}
	if id, ok := proj.Paths["status"]; ok {
		if part := proj.Parts[id]; part != nil && part.Value != nil {
			status = part.Value.Text
		}
	}
	return title, status
}

func partHasValue(proj *model.Projection, path, want string) bool {
	if want == "" {
		return true
	}
	partID, ok := proj.Paths[path]
	if !ok {
		return false
	}
	part := proj.Parts[partID]
	if part == nil || part.Value == nil {
		return false
	}
	if len(part.Value.List) > 0 {
		for _, value := range part.Value.List {
			for _, candidate := range strings.Split(want, ",") {
				if strings.EqualFold(value, strings.TrimSpace(candidate)) {
					return true
				}
			}
		}
	}
	for _, candidate := range strings.Split(want, ",") {
		if strings.EqualFold(part.Value.Text, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func matchesAllTokens(text, query string) bool {
	text = strings.ToLower(text)
	for _, token := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, token) {
			return false
		}
	}
	return true
}

func targetText(ref model.Ref) string {
	if len(ref.Path) > 0 {
		return strings.Join(ref.Path, "/")
	}
	entity := ref.Entity
	if strings.HasPrefix(entity, string(ref.Kind)+":") {
		return entity
	}
	return string(ref.Kind) + ":" + entity
}

func valueText(value model.Value) any {
	if value.Text != "" {
		return value.Text
	}
	if len(value.List) > 0 {
		return value.List
	}
	if value.Data != nil {
		return value.Data
	}
	if value.Ref != nil {
		return value.Ref
	}
	return nil
}

func valueOrNil(value model.Value) any {
	if value.Kind == "" && value.Text == "" && value.Data == nil && value.Ref == nil {
		return nil
	}
	return valueText(value)
}

func valueOrNilFromPart(part *model.Part) any {
	if part == nil || part.Value == nil {
		return nil
	}
	if part.Value.Text == "" && part.Value.Data == nil && len(part.Value.List) == 0 {
		return nil
	}
	return valueText(*part.Value)
}

func parentIDOrNil(part *model.Part) any {
	if part == nil || part.ParentID == nil {
		return nil
	}
	return string(*part.ParentID)
}
