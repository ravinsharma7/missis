package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ravinsharma7/missis/implementation/store"
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
