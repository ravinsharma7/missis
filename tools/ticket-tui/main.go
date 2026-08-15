package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ravinsharma7/missis/implementation/model"
	"github.com/ravinsharma7/missis/implementation/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

type detailState struct {
	summary store.TicketSummary
	lines   []string
	offset  int
}

type tuiModel struct {
	client     *missis.Client
	summaries  []store.TicketSummary
	selected   int
	view       string
	detail     *detailState
	compareA   *store.TicketSummary
	compareB   *store.TicketSummary
	message    string
	err        error
	width      int
	height     int
	listOffset int
	editing    bool
	input      string
}

func newModel() (*tuiModel, error) {
	client, err := missis.Open("")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	summaries, err := client.ListTickets(now)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &tuiModel{
		client:    client,
		summaries: summaries,
		view:      "list",
	}, nil
}

func (m tuiModel) Init() tea.Cmd {
	return nil
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.view == "list" || m.view == "detail" || m.view == "compare" || m.view == "input" {
				if m.view != "list" {
					m.view = "list"
					m.detail = nil
					m.compareA = nil
					m.compareB = nil
					m.message = ""
					return m, nil
				}
				return m, tea.Quit
			}
		}
	}

	switch m.view {
	case "list":
		return m.updateList(msg)
	case "detail":
		return m.updateDetail(msg)
	case "compare":
		return m.updateCompare(msg)
	case "input":
		return m.updateInput(msg)
	default:
		return m, nil
	}
}

func (m tuiModel) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.clampListOffset()
		}
	case "down", "j":
		if m.selected < len(m.summaries)-1 {
			m.selected++
			m.clampListOffset()
		}
	case "pgup":
		if m.height > 0 {
			m.listOffset -= maxInt(1, m.height-3)
		}
		m.clampListOffset()
	case "pgdown":
		if m.height > 0 {
			m.listOffset += maxInt(1, m.height-3)
		}
		m.clampListOffset()
	case "enter", " ":
		if len(m.summaries) == 0 {
			return m, nil
		}
		summary := m.summaries[m.selected]
		lines, err := ticketLines(m.client, summary)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.detail = &detailState{summary: summary, lines: lines}
		m.view = "detail"
	case "c":
		if len(m.summaries) == 0 {
			return m, nil
		}
		m.compareA = &m.summaries[m.selected]
		m.message = "compare A: " + m.compareA.Ref
	case "v":
		if m.compareA == nil {
			m.message = "press c first to select compare A"
			return m, nil
		}
		if len(m.summaries) == 0 {
			return m, nil
		}
		m.compareB = &m.summaries[m.selected]
		m.view = "compare"
		m.message = ""
	case "e":
		if len(m.summaries) == 0 {
			return m, nil
		}
		summary := m.summaries[m.selected]
		dst, err := exportTicket(m.client, summary)
		if err != nil {
			m.err = err
		} else {
			m.message = "exported " + summary.Ref + " -> " + dst
		}
	}
	return m, nil
}

func (m tuiModel) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.detail.offset > 0 {
			m.detail.offset--
		}
	case "down", "j":
		if m.detail.offset < len(m.detail.lines)-1 {
			m.detail.offset++
		}
	case "pgup":
		if m.height > 0 {
			m.detail.offset -= maxInt(1, m.height-4)
		}
		if m.detail.offset < 0 {
			m.detail.offset = 0
		}
	case "pgdown":
		if m.height > 0 {
			m.detail.offset += maxInt(1, m.height-4)
		}
		if m.detail.offset > len(m.detail.lines)-1 {
			m.detail.offset = len(m.detail.lines) - 1
		}
	case "g", "home":
		m.detail.offset = 0
	case "G", "end":
		if len(m.detail.lines) > 0 {
			m.detail.offset = len(m.detail.lines) - 1
		}
	case "b", "esc":
		m.view = "list"
		m.detail = nil
		m.message = ""
	case "e":
		dst, err := exportTicket(m.client, m.detail.summary)
		if err != nil {
			m.err = err
		} else {
			m.message = "exported " + m.detail.summary.Ref + " -> " + dst
		}
	case "t":
		m.editing = true
		m.input = m.detail.summary.Title
		m.view = "input"
	}
	return m, nil
}

func (m tuiModel) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "enter":
		if m.detail == nil {
			m.view = "detail"
			m.editing = false
			return m, nil
		}
		title := strings.TrimSpace(m.input)
		if title == "" {
			m.view = "detail"
			m.editing = false
			return m, nil
		}
		if err := setTicketTitle(m.client, m.detail.summary, title); err != nil {
			m.err = err
			m.view = "detail"
			m.editing = false
			return m, nil
		}
		m.detail.summary.Title = title
		lines, err := ticketLines(m.client, m.detail.summary)
		if err != nil {
			m.err = err
		} else {
			m.detail.lines = lines
		}
		m.view = "detail"
		m.editing = false
		m.message = "title updated"
	case "esc":
		m.view = "detail"
		m.editing = false
	case "backspace":
		runes := []rune(m.input)
		if len(runes) > 0 {
			m.input = string(runes[:len(runes)-1])
		}
	default:
		if len([]rune(key.String())) == 1 && !strings.Contains(key.String(), "ctrl") {
			m.input += key.String()
		}
	}
	return m, nil
}

func (m tuiModel) updateCompare(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "b" || key.String() == "esc" {
		m.view = "list"
		m.compareA = nil
		m.compareB = nil
		m.message = ""
	}
	return m, nil
}

func (m tuiModel) View() string {
	if m.err != nil {
		return errorStyle.Render(m.err.Error()) + "\npress q to quit"
	}
	var body string
	switch m.view {
	case "list":
		body = m.viewList()
	case "detail":
		body = m.viewDetail()
	case "compare":
		body = m.viewCompare()
	case "input":
		body = "Edit title: " + m.input + "▌"
	default:
		body = "unknown view"
	}
	help := helpStyle.Render(m.helpForView())
	if m.message != "" {
		help += "\n" + m.message
	}
	return lipgloss.JoinVertical(lipgloss.Top, body, help)
}

func (m tuiModel) helpForView() string {
	switch m.view {
	case "list":
		return "j/k move | enter open | c compare A | v compare B | e export | pgup/pgdn page | q quit"
	case "detail":
		return "j/k scroll | pgup/pgdn page | g/G top/end | t edit title | e export | b back | q back"
	case "compare":
		return "b back | q quit"
	case "input":
		return "enter save | esc cancel | backspace delete"
	default:
		return "q quit"
	}
}

func (m tuiModel) viewList() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("missis tickets"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("%-8s %-10s %s\n", "REF", "STATUS", "TITLE"))
	visible := m.height - 3
	if visible < 1 {
		visible = 1
	}
	start := m.listOffset
	end := start + visible
	if end > len(m.summaries) {
		end = len(m.summaries)
	}
	for i := start; i < end; i++ {
		summary := m.summaries[i]
		line := fmt.Sprintf("%-8s %-10s %s", summary.Ref, summary.Status, summary.Title)
		if i == m.selected {
			line = "> " + line
		} else {
			line = "  " + line
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *tuiModel) clampListOffset() {
	if m.listOffset < 0 {
		m.listOffset = 0
	}
	if m.height <= 0 {
		return
	}
	visible := m.height - 3
	if visible < 1 {
		visible = 1
	}
	maxOffset := len(m.summaries) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.listOffset > maxOffset {
		m.listOffset = maxOffset
	}
	if m.selected < m.listOffset {
		m.listOffset = m.selected
	}
	if m.selected >= m.listOffset+visible {
		m.listOffset = m.selected - visible + 1
	}
}

func (m tuiModel) viewDetail() string {
	if m.detail == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.detail.summary.Ref + "  " + m.detail.summary.Title))
	b.WriteString("\n\n")
	start := m.detail.offset
	end := start + m.height - 6
	if end < start {
		end = start
	}
	if end > len(m.detail.lines) {
		end = len(m.detail.lines)
	}
	width := m.width - 2
	if width < 20 {
		width = 20
	}
	visibleLines := wrapIndentedLines(m.detail.lines[start:end], width)
	for _, line := range visibleLines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m tuiModel) viewCompare() string {
	if m.compareA == nil || m.compareB == nil {
		return "select two tickets"
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("compare"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("A: %s  %s  %s\n", m.compareA.Ref, m.compareA.Status, m.compareA.Title))
	b.WriteString(fmt.Sprintf("B: %s  %s  %s\n", m.compareB.Ref, m.compareB.Status, m.compareB.Title))
	if m.compareA.ID == m.compareB.ID {
		b.WriteString("\n(same ticket)\n")
	}
	b.WriteString("\n")
	a := ticketSummaryParts(m.client, *m.compareA)
	bb := ticketSummaryParts(m.client, *m.compareB)
	paths := make(map[string]bool)
	for path := range a {
		paths[path] = true
	}
	for path := range bb {
		paths[path] = true
	}
	sortedPaths := make([]string, 0, len(paths))
	for path := range paths {
		sortedPaths = append(sortedPaths, path)
	}
	sort.Strings(sortedPaths)
	for _, path := range sortedPaths {
		b.WriteString(fmt.Sprintf("%s\n  A: %s\n  B: %s\n", path, renderMarkdownValue(a[path]), renderMarkdownValue(bb[path])))
	}
	return b.String()
}

func ticketLines(client *missis.Client, summary store.TicketSummary) ([]string, error) {
	now := time.Now().UTC()
	proj, err := client.BitemporalProjection(summary.ID, now, now)
	if err != nil {
		return nil, err
	}
	lines := []string{
		"status: " + summary.Status,
	}
	pathByID := make(map[model.PartID]string)
	for path, id := range proj.Paths {
		pathByID[id] = path
	}
	var roots []model.PartID
	for id, part := range proj.Parts {
		if part.ParentID != nil {
			continue
		}
		if pathByID[id] == "title" || pathByID[id] == "status" {
			continue
		}
		roots = append(roots, id)
	}
	sort.Slice(roots, func(i, j int) bool {
		return pathByID[roots[i]] < pathByID[roots[j]]
	})
	for _, id := range roots {
		lines = append(lines, partSubtree(proj, id, 0, pathByID)...)
	}
	return lines, nil
}

func partSubtree(proj *model.Projection, id model.PartID, depth int, pathByID map[model.PartID]string) []string {
	part := proj.Parts[id]
	if part == nil {
		return nil
	}
	path := pathByID[id]
	if path == "" {
		path = part.Name
	}
	name := part.DisplayName
	if name == "" {
		name = part.Name
	}
	indent := strings.Repeat("  ", depth)
	lines := []string{indent + "▸ " + name}
	if part.Value != nil {
		value := renderMarkdownValue(valueText(*part.Value))
		for _, valueLine := range strings.Split(value, "\n") {
			lines = append(lines, indent+"   "+valueLine)
		}
	}
	var children []model.PartID
	for childID, child := range proj.Parts {
		if child.ParentID != nil && *child.ParentID == id {
			children = append(children, childID)
		}
	}
	sort.Slice(children, func(i, j int) bool {
		return pathByID[children[i]] < pathByID[children[j]]
	})
	for _, childID := range children {
		lines = append(lines, partSubtree(proj, childID, depth+1, pathByID)...)
	}
	return lines
}

func renderMarkdownValue(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "### "):
			lines[i] = "▸ " + strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
		case strings.HasPrefix(trimmed, "## "):
			lines[i] = "▸▸ " + strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
		case strings.HasPrefix(trimmed, "# "):
			lines[i] = "▸▸▸ " + strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		case strings.HasPrefix(trimmed, "- "):
			lines[i] = "  • " + strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		case strings.HasPrefix(trimmed, "* "):
			lines[i] = "  • " + strings.TrimSpace(strings.TrimPrefix(trimmed, "* "))
		case strings.HasPrefix(trimmed, "> "):
			lines[i] = "  " + strings.TrimSpace(strings.TrimPrefix(trimmed, "> "))
		case strings.HasPrefix(trimmed, "```"):
			lines[i] = "  ─ code fence ─"
		default:
			lines[i] = trimmed
		}
	}
	return strings.Join(lines, "\n")
}

func ticketSummaryParts(client *missis.Client, summary store.TicketSummary) map[string]string {
	now := time.Now().UTC()
	proj, err := client.BitemporalProjection(summary.ID, now, now)
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	out := make(map[string]string)
	for path, partID := range proj.Paths {
		if path == "title" || path == "status" {
			continue
		}
		part := proj.Parts[partID]
		if part == nil || part.Value == nil {
			continue
		}
		out[path] = valueText(*part.Value)
	}
	return out
}

func valueText(value model.Value) string {
	if value.Text != "" {
		return value.Text
	}
	if len(value.List) > 0 {
		return strings.Join(value.List, ", ")
	}
	if value.Data != nil {
		return fmt.Sprint(value.Data)
	}
	if value.Ref != nil {
		return targetText(*value.Ref)
	}
	return ""
}

func targetText(ref model.Ref) string {
	if ref.Entity == "" {
		return string(ref.Kind)
	}
	return string(ref.Kind) + ":" + ref.Entity
}

func exportTicket(client *missis.Client, summary store.TicketSummary) (string, error) {
	dir := filepath.Join(".", ".missis.d", "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	parts := ticketSummaryParts(client, summary)
	var b strings.Builder
	b.WriteString("# " + summary.Title + "\n\n")
	b.WriteString("status: " + summary.Status + "\n\n")
	paths := make([]string, 0, len(parts))
	for path := range parts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		b.WriteString("## " + path + "\n\n")
		b.WriteString(parts[path] + "\n\n")
	}
	dst := filepath.Join(dir, summary.Ref[1:]+".md")
	return dst, os.WriteFile(dst, []byte(b.String()), 0o644)
}

func setTicketTitle(client *missis.Client, summary store.TicketSummary, title string) error {
	now := time.Now().UTC()
	proj, err := client.BitemporalProjection(summary.ID, now, now)
	if err != nil {
		return err
	}
	partID, ok := proj.Paths["title"]
	if !ok {
		return fmt.Errorf("title part not found")
	}
	stream := model.Ref{Kind: model.KindTicket, Entity: string(summary.ID)}
	event := model.Event{
		ID:          model.EventID(missis.NewID("event")),
		Stream:      stream,
		Operation:   model.OpSetValue,
		Target:      model.Ref{Kind: model.KindPart, Entity: string(partID), Path: []string{"title"}},
		Value:       model.Value{Kind: model.ValueKindText, Text: title},
		RecordedAt:  now,
		EffectiveAt: now,
		Actor:       model.ActorRef{Kind: "human", ID: "tui", Name: "tui"},
	}
	_, err = client.AppendBatch([]model.Event{event}, "", nil, nil)
	return err
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return strings.Split(text, "\n")
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if len([]rune(line)) <= width {
			out = append(out, line)
			continue
		}
		runes := []rune(line)
		for len(runes) > width {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		if len(runes) > 0 {
			out = append(out, string(runes))
		}
	}
	return out
}

func wrapIndentedLines(lines []string, width int) []string {
	var out []string
	for _, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		prefix := line[:indent]
		content := line[indent:]
		available := width - indent
		if available < 1 {
			available = 1
		}
		runes := []rune(content)
		if len(runes) <= available {
			out = append(out, line)
			continue
		}
		for len(runes) > available {
			out = append(out, prefix+string(runes[:available]))
			runes = runes[available:]
		}
		if len(runes) > 0 {
			out = append(out, prefix+string(runes))
		}
	}
	return out
}

func main() {
	m, err := newModel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
