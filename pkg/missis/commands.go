package missis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/implementation/model"
)

type NewTicketOptions struct {
	Title       string
	Project     string
	Priority    string
	Types       []string
	Tags        []string
	Actor       string
	EffectiveAt time.Time
	Idempotency string
}

type ShowOptions struct {
	EffectiveAt time.Time
	KnownAt     time.Time
}

type SetPartOptions struct {
	Ref       string
	Value     string
	Add       bool
	Retract   bool
	Recursive bool
	Name      string
	Parent    string
	Reason    string
	Actor     string
}

type LineageOptions struct {
	Direction string
	Depth     int
	Relations []string
}

type SearchOptions struct {
	Query       string
	Status      string
	Project     string
	Group       string
	Type        string
	Tag         string
	EffectiveAt time.Time
}

func (c *Client) NewTicket(ctx context.Context, opts NewTicketOptions) (TicketSummary, error) {
	now := time.Now().UTC()
	if opts.EffectiveAt.IsZero() {
		opts.EffectiveAt = now
	}
	ticketID := model.TicketID(NewID("ticket"))
	batchID := model.BatchID(NewID("batch"))
	stream := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	actor := model.ActorRef{Kind: "human", ID: opts.Actor, Name: opts.Actor}
	if actor.ID == "" {
		actor.ID = "human/local"
		actor.Name = "human/local"
	}
	events := []model.Event{
		NewEvent(stream, model.OpCreateEntity, model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}, model.Value{}, actor, now, opts.EffectiveAt, batchID, ""),
		PartEvent(stream, "title", opts.Title, model.ValueKindText, actor, now, opts.EffectiveAt, batchID),
		PartEvent(stream, "status", "open", model.ValueKindStatus, actor, now, opts.EffectiveAt, batchID),
	}
	if opts.Priority != "" {
		events = append(events, PartEvent(stream, "priority", opts.Priority, model.ValueKindPriority, actor, now, opts.EffectiveAt, batchID))
	}
	if len(opts.Types) > 0 {
		events = append(events, PartEvent(stream, "type", opts.Types, model.ValueKindList, actor, now, opts.EffectiveAt, batchID))
	}
	if len(opts.Tags) > 0 {
		events = append(events, PartEvent(stream, "tag", opts.Tags, model.ValueKindList, actor, now, opts.EffectiveAt, batchID))
	}
	outcome, alias, err := c.AppendTicketBatch(ctx, events, opts.Idempotency, nil)
	if err != nil {
		return TicketSummary{}, err
	}
	_ = outcome
	return TicketSummary{
		Ref:        "#" + strconv.FormatUint(alias, 10),
		ID:         string(ticketID),
		Title:      opts.Title,
		Status:     "open",
		RecordedAt: now,
	}, nil
}

func (c *Client) ListTicketSummaries(ctx context.Context, effectiveAt time.Time) ([]TicketSummary, error) {
	items, err := c.Store().ListTickets(effectiveAt)
	if err != nil {
		return nil, err
	}
	out := make([]TicketSummary, 0, len(items))
	for _, item := range items {
		out = append(out, TicketSummary{
			Ref:        item.Ref,
			ID:         string(item.ID),
			Title:      item.Title,
			Status:     item.Status,
			RecordedAt: item.RecordedAt,
		})
	}
	return out, nil
}

func (c *Client) ShowTicket(ctx context.Context, ref string, opts ShowOptions) (TicketProjection, error) {
	summaries, err := c.ListTicketSummaries(ctx, opts.EffectiveAt)
	if err != nil {
		return TicketProjection{}, err
	}
	var summary TicketSummary
	found := false
	for _, item := range summaries {
		if item.Ref == ref {
			summary = item
			found = true
			break
		}
	}
	if !found {
		return TicketProjection{}, fmt.Errorf("ticket not found: %s", ref)
	}
	proj, err := c.BitemporalProjection(ctx, model.TicketID(summary.ID), opts.EffectiveAt, opts.KnownAt)
	if err != nil {
		return TicketProjection{}, err
	}
	parts := make(map[string]PartView)
	for path, partID := range proj.Paths {
		part := proj.Parts[partID]
		if part == nil {
			continue
		}
		var value any
		if part.Value != nil {
			value = valueText(*part.Value)
		}
		parts[path] = PartView{
			ID:        string(part.ID),
			Path:      path,
			Value:     value,
			ValueKind: string(part.ValueKind),
			CreatedBy: string(part.CreatedBy),
		}
	}
	return TicketProjection{
		Ref:        summary.Ref,
		ID:         summary.ID,
		Title:      summary.Title,
		Status:     summary.Status,
		RecordedAt: summary.RecordedAt,
		Parts:      parts,
	}, nil
}

func (c *Client) SetPart(ctx context.Context, opts SetPartOptions) error {
	summaries, err := c.ListTicketSummaries(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	ref := opts.Ref
	if !strings.HasPrefix(ref, "#") {
		return fmt.Errorf("ticket ref required")
	}
	var summary TicketSummary
	for _, item := range summaries {
		if item.Ref == strings.SplitN(ref, "/", 2)[0] {
			summary = item
			break
		}
	}
	if summary.ID == "" {
		return fmt.Errorf("ticket not found: %s", ref)
	}
	path := strings.TrimPrefix(ref, summary.Ref)
	path = strings.TrimPrefix(path, "/")
	proj, err := c.CurrentProjection(ctx, model.TicketID(summary.ID), time.Now().UTC())
	if err != nil {
		return err
	}
	partID, ok := proj.Paths[path]
	if !ok {
		return fmt.Errorf("part not found: %s", path)
	}
	now := time.Now().UTC()
	stream := model.Ref{Kind: model.KindTicket, Entity: summary.ID}
	event := model.Event{
		ID:          model.EventID(NewID("event")),
		Stream:      stream,
		Operation:   model.OpSetValue,
		Target:      model.Ref{Kind: model.KindPart, Entity: string(partID), Path: []string{path}},
		Value:       model.Value{Kind: model.ValueKindText, Text: opts.Value},
		RecordedAt:  now,
		EffectiveAt: now,
		Actor:       model.ActorRef{Kind: "human", ID: opts.Actor, Name: opts.Actor},
		Reason:      opts.Reason,
	}
	_, err = c.AppendBatch(ctx, []model.Event{event}, "", nil, nil)
	return err
}

func (c *Client) ShowHistory(ctx context.Context, ref string, opts ShowOptions) ([]EventView, error) {
	summary, err := c.findTicketSummary(ctx, ref)
	if err != nil {
		return nil, err
	}
	events, err := c.LoadTicketEvents(ctx, model.TicketID(summary.ID))
	if err != nil {
		return nil, err
	}
	out := make([]EventView, 0, len(events))
	for _, event := range events {
		if opts.EffectiveAt.IsZero() {
			opts.EffectiveAt = time.Now().UTC()
		}
		if opts.KnownAt.IsZero() {
			opts.KnownAt = opts.EffectiveAt
		}
		if event.EffectiveAt.After(opts.EffectiveAt) || event.RecordedAt.After(opts.KnownAt) {
			continue
		}
		out = append(out, EventView{
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
	return out, nil
}

func (c *Client) ShowReferences(ctx context.Context, ref string) ([]LinkView, error) {
	target, err := c.resolveRef(ctx, ref)
	if err != nil {
		return nil, err
	}
	events, err := c.LoadLinkEvents(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	links, err := model.LinksForRef(events, target, now, now)
	if err != nil {
		return nil, err
	}
	out := make([]LinkView, 0, len(links))
	for _, link := range links {
		out = append(out, LinkView{
			From:      targetText(link.From),
			Relation:  link.Relation,
			To:        targetText(link.To),
			Direction: link.Direction,
			Origin:    link.Origin,
			CreatedBy: string(link.CreatedBy),
		})
	}
	return out, nil
}

func (c *Client) ShowLineage(ctx context.Context, ref string, opts LineageOptions) ([]LineageEdge, error) {
	target, err := c.resolveRef(ctx, ref)
	if err != nil {
		return nil, err
	}
	events, err := c.LoadLinkEvents(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	graph, err := model.BuildLineageGraph(events, now, now)
	if err != nil {
		return nil, err
	}
	relationSet := make(map[string]bool)
	for _, relation := range opts.Relations {
		relationSet[relation] = true
	}
	edges, err := graph.Walk(target, opts.Direction, opts.Depth, relationSet)
	if err != nil {
		return nil, err
	}
	out := make([]LineageEdge, 0, len(edges))
	for _, edge := range edges {
		out = append(out, LineageEdge{
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

func (c *Client) Search(ctx context.Context, opts SearchOptions) ([]TicketSummary, error) {
	summaries, err := c.ListTicketSummaries(ctx, opts.EffectiveAt)
	if err != nil {
		return nil, err
	}
	out := make([]TicketSummary, 0)
	for _, summary := range summaries {
		if opts.Status != "" && summary.Status != opts.Status {
			continue
		}
		if opts.Query != "" {
			proj, err := c.ShowTicket(ctx, summary.Ref, ShowOptions{EffectiveAt: opts.EffectiveAt, KnownAt: opts.EffectiveAt})
			if err != nil {
				continue
			}
			text := summary.Title
			for _, part := range proj.Parts {
				if s, ok := part.Value.(string); ok {
					text += " " + s
				}
			}
			if !strings.Contains(strings.ToLower(text), strings.ToLower(opts.Query)) {
				continue
			}
		}
		out = append(out, summary)
	}
	return out, nil
}

func (c *Client) RetractPart(ctx context.Context, opts SetPartOptions) error {
	summary, err := c.findTicketSummary(ctx, opts.Ref)
	if err != nil {
		return err
	}
	path := strings.TrimPrefix(opts.Ref, summary.Ref)
	path = strings.TrimPrefix(path, "/")
	proj, err := c.CurrentProjection(ctx, model.TicketID(summary.ID), time.Now().UTC())
	if err != nil {
		return err
	}
	partID, ok := proj.Paths[path]
	if !ok {
		return fmt.Errorf("part not found: %s", path)
	}
	now := time.Now().UTC()
	stream := model.Ref{Kind: model.KindTicket, Entity: summary.ID}
	operation := model.OpRetractValue
	if opts.Recursive {
		operation = model.OpRetractSubtree
	}
	event := model.Event{
		ID:          model.EventID(NewID("event")),
		Stream:      stream,
		Operation:   operation,
		Target:      model.Ref{Kind: model.KindPart, Entity: string(partID), Path: []string{path}},
		RecordedAt:  now,
		EffectiveAt: now,
		Actor:       model.ActorRef{Kind: "human", ID: opts.Actor, Name: opts.Actor},
		Reason:      opts.Reason,
	}
	_, err = c.AppendBatch(ctx, []model.Event{event}, "", nil, nil)
	return err
}

func (c *Client) findTicketSummary(ctx context.Context, ref string) (TicketSummary, error) {
	baseRef := strings.SplitN(ref, "/", 2)[0]
	summaries, err := c.ListTicketSummaries(ctx, time.Now().UTC())
	if err != nil {
		return TicketSummary{}, err
	}
	for _, summary := range summaries {
		if summary.Ref == baseRef {
			return summary, nil
		}
	}
	return TicketSummary{}, fmt.Errorf("ticket not found: %s", ref)
}

func (c *Client) resolveRef(ctx context.Context, ref string) (model.Ref, error) {
	if strings.HasPrefix(ref, "project:") {
		return model.Ref{Kind: model.KindProject, Entity: strings.TrimPrefix(ref, "project:")}, nil
	}
	if strings.HasPrefix(ref, "group:") {
		return model.Ref{Kind: model.KindGroup, Entity: strings.TrimPrefix(ref, "group:")}, nil
	}
	summary, err := c.findTicketSummary(ctx, ref)
	if err != nil {
		return model.Ref{}, err
	}
	return model.Ref{Kind: model.KindTicket, Entity: summary.ID}, nil
}

func targetText(ref model.Ref) string {
	if ref.Entity == "" {
		return string(ref.Kind)
	}
	return string(ref.Kind) + ":" + ref.Entity
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
