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
