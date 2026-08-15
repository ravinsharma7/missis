package render

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ravinsharma7/missis/implementation/store"
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
			RecordedAt: item.RecordedAt.UTC().Format("2006-01-02T15:04:05Z"),
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
		part := projection.Parts[path]
		if part.Value == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("%s: %v\n", path, part.Value))
	}
	return b.String()
}

func TicketJSON(projection missis.TicketProjection) ([]byte, error) {
	return json.Marshal(projection)
}

func HistoryText(events []missis.EventView) string {
	var b strings.Builder
	for _, event := range events {
		b.WriteString(fmt.Sprintf("%s %s %s\n", event.Alias, event.Operation, event.Target))
	}
	return b.String()
}

func HistoryJSON(events []missis.EventView) ([]byte, error) {
	return json.Marshal(map[string]any{"events": events})
}

func ReferencesText(links []missis.LinkView) string {
	var b strings.Builder
	for _, link := range links {
		b.WriteString(fmt.Sprintf("%s %s %s %s\n", link.Direction, link.Relation, link.From, link.To))
	}
	return b.String()
}

func ReferencesJSON(links []missis.LinkView) ([]byte, error) {
	return json.Marshal(map[string]any{"links": links})
}

func LineageText(edges []missis.LineageEdge) string {
	var b strings.Builder
	for _, edge := range edges {
		b.WriteString(fmt.Sprintf("%d %s %s %s %s\n", edge.Depth, edge.Direction, edge.From, edge.Relation, edge.To))
	}
	return b.String()
}

func LineageJSON(edges []missis.LineageEdge) ([]byte, error) {
	return json.Marshal(map[string]any{"edges": edges})
}
