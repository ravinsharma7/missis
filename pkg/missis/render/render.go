package render

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TicketListText(items []store.TicketSummary) string {
	var b strings.Builder
	b.WriteString("REF\tSTATUS\tTITLE\tRECORDED_AT\n")
	for _, item := range items {
		title := item.Title
		if title == "" {
			title = "<no title>"
		}
		b.WriteString(fmt.Sprintf("%s\t%s\t%s\t%s\n", item.Ref, item.Status, title, item.RecordedAt.UTC().Format("2006-01-02T15:04:05Z")))
	}
	return b.String()
}

func TicketListJSON(items []store.TicketSummary) ([]byte, error) {
	type ticketJSON struct {
		Ref        string `json:"ref"`
		ID         string `json:"id"`
		Title      string `json:"title"`
		Status     string `json:"status"`
		RecordedAt string `json:"recorded_at"`
	}
	out := make([]ticketJSON, 0, len(items))
	for _, item := range items {
		out = append(out, ticketJSON{
			Ref:        item.Ref,
			ID:         string(item.ID),
			Title:      item.Title,
			Status:     item.Status,
			RecordedAt: item.RecordedAt.UTC().Format(time.RFC3339),
		})
	}
	return json.Marshal(map[string]any{"tickets": out})
}

func Markdown(title string, parts map[string]string) string {
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	for path, value := range parts {
		b.WriteString("## " + path + "\n\n")
		b.WriteString(value + "\n\n")
	}
	return b.String()
}

func TicketText(projection missis.TicketProjection) string {
	var b strings.Builder
	b.WriteString(projection.Ref + "  " + projection.Title + "\n")
	b.WriteString("status: " + projection.Status + "\n")
	paths := make([]string, 0, len(projection.Parts))
	for path := range projection.Parts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if path == "title" || path == "status" {
			continue
		}
		part := projection.Parts[path]
		if part.Value == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("%s: %v\n", path, part.Value))
	}
	return b.String()
}

type partJSON struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Value     any    `json:"value"`
	ValueKind string `json:"value_kind"`
	ParentID  any    `json:"parent_id"`
	CreatedBy string `json:"created_by"`
}

type ticketJSON struct {
	Ref        string              `json:"ref"`
	ID         string              `json:"id"`
	Title      string              `json:"title"`
	Status     string              `json:"status"`
	RecordedAt string              `json:"recorded_at"`
	Parts      map[string]partJSON `json:"parts"`
}

func TicketJSON(projection missis.TicketProjection) ([]byte, error) {
	parts := make(map[string]partJSON, len(projection.Parts))
	for path, part := range projection.Parts {
		parts[path] = partJSON{
			ID:        part.ID,
			Path:      path,
			Value:     part.Value,
			ValueKind: part.ValueKind,
			ParentID:  part.ParentID,
			CreatedBy: part.CreatedBy,
		}
	}
	return json.Marshal(ticketJSON{
		Ref:        projection.Ref,
		ID:         projection.ID,
		Title:      projection.Title,
		Status:     projection.Status,
		RecordedAt: projection.RecordedAt.UTC().Format(time.RFC3339),
		Parts:      parts,
	})
}

func HistoryText(events []missis.EventView) string {
	var b strings.Builder
	for _, event := range events {
		b.WriteString(fmt.Sprintf("%s %s %s %v\n", event.Alias, event.Operation, event.Target, event.Value))
	}
	return b.String()
}

type eventJSON struct {
	ID          string `json:"id"`
	Alias       string `json:"alias"`
	Sequence    uint64 `json:"sequence"`
	Operation   string `json:"operation"`
	Target      string `json:"target"`
	Value       any    `json:"value"`
	RecordedAt  string `json:"recorded_at"`
	EffectiveAt string `json:"effective_at"`
	Actor       string `json:"actor"`
	Reason      string `json:"reason,omitempty"`
}

func HistoryJSON(events []missis.EventView) ([]byte, error) {
	out := make([]eventJSON, 0, len(events))
	for _, event := range events {
		out = append(out, eventJSON{
			ID:          event.ID,
			Alias:       event.Alias,
			Sequence:    event.Sequence,
			Operation:   event.Operation,
			Target:      event.Target,
			Value:       event.Value,
			RecordedAt:  event.RecordedAt.UTC().Format(time.RFC3339),
			EffectiveAt: event.EffectiveAt.UTC().Format(time.RFC3339),
			Actor:       event.Actor,
			Reason:      event.Reason,
		})
	}
	return json.Marshal(map[string]any{"events": out})
}

func ReferencesText(links []missis.LinkView) string {
	var b strings.Builder
	for _, link := range links {
		b.WriteString(fmt.Sprintf("%s %s %s %s\n", link.Direction, link.Relation, link.From, link.To))
	}
	return b.String()
}

type linkJSON struct {
	From      string `json:"from"`
	Relation  string `json:"relation"`
	To        string `json:"to"`
	Direction string `json:"direction"`
	Origin    string `json:"origin"`
	CreatedBy string `json:"created_by"`
}

func ReferencesJSON(links []missis.LinkView) ([]byte, error) {
	out := make([]linkJSON, 0, len(links))
	for _, link := range links {
		out = append(out, linkJSON{
			From:      link.From,
			Relation:  link.Relation,
			To:        link.To,
			Direction: link.Direction,
			Origin:    link.Origin,
			CreatedBy: link.CreatedBy,
		})
	}
	return json.Marshal(map[string]any{"links": out})
}

func LineageText(edges []missis.LineageEdge) string {
	var b strings.Builder
	for _, edge := range edges {
		b.WriteString(fmt.Sprintf("%d %s %s %s %s\n", edge.Depth, edge.Direction, edge.From, edge.Relation, edge.To))
	}
	return b.String()
}

type edgeJSON struct {
	From      string `json:"from"`
	Relation  string `json:"relation"`
	To        string `json:"to"`
	Direction string `json:"direction"`
	Depth     int    `json:"depth"`
	Origin    string `json:"origin"`
	CreatedBy string `json:"created_by"`
}

func LineageJSON(edges []missis.LineageEdge) ([]byte, error) {
	out := make([]edgeJSON, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edgeJSON{
			From:      edge.From,
			Relation:  edge.Relation,
			To:        edge.To,
			Direction: edge.Direction,
			Depth:     edge.Depth,
			Origin:    edge.Origin,
			CreatedBy: edge.CreatedBy,
		})
	}
	return json.Marshal(map[string]any{"edges": out})
}

type listTicketJSON struct {
	Ref        string `json:"ref"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	RecordedAt string `json:"recorded_at"`
}

func ShowList(items []missis.TicketSummary, format string) (string, error) {
	switch format {
	case "json":
		out := make([]listTicketJSON, 0, len(items))
		for _, item := range items {
			out = append(out, listTicketJSON{
				Ref:        item.Ref,
				ID:         item.ID,
				Title:      item.Title,
				Status:     item.Status,
				RecordedAt: item.RecordedAt.UTC().Format(time.RFC3339),
			})
		}
		data, err := json.Marshal(map[string]any{"tickets": out})
		return string(data), err
	default:
		var b strings.Builder
		b.WriteString("REF\tSTATUS\tTITLE\tRECORDED_AT\n")
		for _, item := range items {
			b.WriteString(fmt.Sprintf("%s\t%s\t%s\t%s\n", item.Ref, item.Status, item.Title, item.RecordedAt.UTC().Format(time.RFC3339)))
		}
		return b.String(), nil
	}
}

func ShowTicket(projection missis.TicketProjection, format string) (string, error) {
	if format == "json" {
		data, err := TicketJSON(projection)
		return string(data), err
	}
	return TicketText(projection), nil
}

// ShowMarkdown renders a ticket as Markdown with heading depth derived from
// the part path, matching the CLI's markdown export shape.
func ShowMarkdown(projection missis.TicketProjection, links []missis.LinkView) string {
	title := projection.Title
	if title == "" {
		title = projection.Ref
	}
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	paths := make([]string, 0, len(projection.Parts))
	for path := range projection.Parts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if path == "title" || path == "status" {
			continue
		}
		part := projection.Parts[path]
		depth := len(strings.Split(path, "/")) + 1
		if depth > 6 {
			depth = 6
		}
		heading := strings.Repeat("#", depth)
		last := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			last = path[idx+1:]
		}
		b.WriteString(heading + " " + last + "\n\n")
		if part.Value != nil {
			b.WriteString(fmt.Sprintf("%v\n\n", part.Value))
		}
	}
	if len(links) > 0 {
		b.WriteString("## Links\n\n")
		for _, link := range links {
			b.WriteString(fmt.Sprintf("- %s %s %s\n", link.Relation, link.From, link.To))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func ShowHistory(events []missis.EventView, format string) (string, error) {
	if format == "json" {
		data, err := HistoryJSON(events)
		return string(data), err
	}
	return HistoryText(events), nil
}

func ShowReferences(links []missis.LinkView, format string) (string, error) {
	if format == "json" {
		data, err := ReferencesJSON(links)
		return string(data), err
	}
	return ReferencesText(links), nil
}

func ShowLineage(edges []missis.LineageEdge, start, format string) (string, error) {
	if format == "json" {
		out := make([]edgeJSON, 0, len(edges))
		for _, edge := range edges {
			out = append(out, edgeJSON{
				From:      edge.From,
				Relation:  edge.Relation,
				To:        edge.To,
				Direction: edge.Direction,
				Depth:     edge.Depth,
				Origin:    edge.Origin,
				CreatedBy: edge.CreatedBy,
			})
		}
		data, err := json.Marshal(map[string]any{"start": start, "edges": out})
		return string(data), err
	}
	return LineageText(edges), nil
}
