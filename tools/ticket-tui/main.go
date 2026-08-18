package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/pkg/missis"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

const (
	defaultWidth       = 80
	defaultHeight      = 24
	refreshInterval    = 2 * time.Second
	listReservedRows   = 4
	detailReservedRows = 4
	statsReservedRows  = 4
)

type detailState struct {
	summary  missis.TicketSummary
	lines    []string
	offset   int
	showRefs bool
}

type refreshMsg struct{}

type tuiModel struct {
	client      *missis.Client
	summaries   []missis.TicketSummary
	selected    int
	view        string
	detail      *detailState
	compareA    *missis.TicketSummary
	compareB    *missis.TicketSummary
	message     string
	err         error
	width       int
	height      int
	listOffset  int
	statsOffset int
	editing     bool
	input       string
	projectCtx  string
	groupCtx    string
}

func newModel() (*tuiModel, error) {
	svc, err := application.Open("")
	if err != nil {
		return nil, err
	}
	client := missis.NewClient(svc)
	now := time.Now().UTC()
	summaries, err := client.ListTicketSummaries(context.Background(), now)
	if err != nil {
		client.Close()
		return nil, err
	}
	projectCtx, groupCtx := "none", "none"
	activePath := filepath.Join(".missis.d", "active.local.md")
	if _, statErr := os.Stat(activePath); statErr != nil {
		activePath = filepath.Join(".missis.d", "active.example.md")
	}
	if data, readErr := os.ReadFile(activePath); readErr == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "project:") {
				projectCtx = strings.TrimSpace(strings.TrimPrefix(line, "project:"))
			}
			if strings.HasPrefix(line, "group:") {
				groupCtx = strings.TrimSpace(strings.TrimPrefix(line, "group:"))
			}
		}
	}
	return &tuiModel{
		client:     client,
		summaries:  summaries,
		view:       "list",
		projectCtx: projectCtx,
		groupCtx:   groupCtx,
	}, nil
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshMsg{} })
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		m.refresh()
		return m, tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshMsg{} })
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampListOffset()
		if m.view == "detail" && m.detail != nil && m.client != nil {
			if !m.detail.showRefs {
				if lines, err := ticketLines(m.client, m.detail.summary, m.renderWidth()); err == nil {
					m.detail.lines = lines
				}
			}
			m.clampDetailOffset()
		}
		if m.view == "stats" {
			m.clampStatsOffset()
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.view == "list" || m.view == "detail" || m.view == "compare" || m.view == "input" || m.view == "stats" {
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
	case "stats":
		return m.updateStats(msg)
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
		lines, err := ticketLines(m.client, summary, m.renderWidth())
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
	case "r":
		m.refresh()
	case "s":
		m.view = "stats"
		m.statsOffset = 0
		m.message = ""
	}
	return m, nil
}

func (m tuiModel) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	rendered := m.renderedDetailLines()
	switch key.String() {
	case "up", "k":
		if m.detail.offset > 0 {
			m.detail.offset--
		}
	case "down", "j":
		if m.detail.offset < len(rendered)-1 {
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
		if m.detail.offset > len(rendered)-1 {
			m.detail.offset = len(rendered) - 1
		}
	case "g", "home":
		m.detail.offset = 0
	case "G", "end":
		if len(rendered) > 0 {
			m.detail.offset = len(rendered) - 1
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
	case "r":
		m.refresh()
	case "R":
		m.detail.showRefs = !m.detail.showRefs
		var err error
		if m.detail.showRefs {
			m.detail.lines, err = referenceLines(m.client, m.detail.summary)
		} else {
			m.detail.lines, err = ticketLines(m.client, m.detail.summary, m.renderWidth())
		}
		if err != nil {
			m.err = err
		} else {
			m.detail.offset = 0
		}
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
		lines, err := ticketLines(m.client, m.detail.summary, m.renderWidth())
		if err != nil {
			m.err = err
		} else {
			m.detail.lines = lines
			m.clampDetailOffset()
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

func (m tuiModel) updateStats(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	lines := m.statsLines()
	switch key.String() {
	case "up", "k":
		if m.statsOffset > 0 {
			m.statsOffset--
		}
	case "down", "j":
		if m.statsOffset < len(lines)-1 {
			m.statsOffset++
		}
	case "pgup":
		if m.height > 0 {
			m.statsOffset -= maxInt(1, m.height-statsReservedRows)
		}
		m.clampStatsOffset()
	case "pgdown":
		if m.height > 0 {
			m.statsOffset += maxInt(1, m.height-statsReservedRows)
		}
		m.clampStatsOffset()
	case "g", "home":
		m.statsOffset = 0
	case "G", "end":
		if len(lines) > 0 {
			m.statsOffset = len(lines) - 1
		}
	case "b", "esc":
		m.view = "list"
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
	case "stats":
		body = m.viewStats()
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
		return "j/k move | enter open | c/v compare | e export | r refresh | s stats | q quit"
	case "detail":
		return "j/k scroll | pgup/pgdn page | g/G top/end | r refresh | R refs | t edit title | e export | b back | q back"
	case "compare":
		return "b back | q quit"
	case "stats":
		return "j/k scroll | pgup/pgdn page | g/G top/end | b back | q back"
	case "input":
		return "enter save | esc cancel | backspace delete"
	default:
		return "q quit"
	}
}

func (m tuiModel) viewList() string {
	width, _ := m.effectiveSize()
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("missis tickets | project: %s | group: %s", m.projectCtx, m.groupCtx)))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("  %-6s %-10s %s\n", "REF", "STATUS", "TITLE"))
	visible := m.listVisibleRows()
	start := m.listOffset
	end := start + visible
	if end > len(m.summaries) {
		end = len(m.summaries)
	}
	// cursor (2) + ref (6) + gap (1) + status (10) + gap (1)
	titleWidth := width - 20
	if titleWidth < 1 {
		titleWidth = 1
	}
	for i := start; i < end; i++ {
		summary := m.summaries[i]
		ref := truncateCell(summary.Ref, 6)
		status := truncateCell(summary.Status, 10)
		title := summary.Title
		if title == "" {
			title = "<no title>"
		}
		title = truncateCell(title, titleWidth)
		cursor := "  "
		if i == m.selected {
			cursor = "> "
		}
		line := fmt.Sprintf("%s%-6s %-10s %s", cursor, ref, status, title)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *tuiModel) clampListOffset() {
	if m.listOffset < 0 {
		m.listOffset = 0
	}
	visible := m.listVisibleRows()
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

func (m tuiModel) effectiveSize() (width, height int) {
	width, height = m.width, m.height
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		height = defaultHeight
	}
	return width, height
}

// listVisibleRows leaves room for the title line, blank line, column header,
// and the help bar rendered by View().
func (m tuiModel) listVisibleRows() int {
	_, height := m.effectiveSize()
	visible := height - listReservedRows
	if visible < 1 {
		visible = 1
	}
	return visible
}

// visibleRange returns the half-open [start, end) window into content of
// length `length` that fits in `available` rows, anchored at `offset`. The
// result always satisfies 0 <= start <= end <= length, regardless of inputs.
func visibleRange(offset, length, available int) (start, end int) {
	if available < 1 {
		available = 1
	}
	if length < 0 {
		length = 0
	}
	start = offset
	if start < 0 {
		start = 0
	}
	if start > length {
		start = length
	}
	end = start + available
	if end > length {
		end = length
	}
	if end < start {
		end = start
	}
	return start, end
}

func truncateCell(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func (m tuiModel) viewDetail() string {
	if m.detail == nil {
		return ""
	}
	var b strings.Builder
	detailTitle := m.detail.summary.Title
	if detailTitle == "" {
		detailTitle = "<no title>"
	}
	viewLabel := ""
	if m.detail.showRefs {
		viewLabel = "  (references)"
	}
	b.WriteString(titleStyle.Render(m.detail.summary.Ref + "  " + detailTitle + viewLabel))
	b.WriteString("\n\n")
	if len(m.detail.lines) <= 1 {
		b.WriteString("<no parts>\n")
	}
	rendered := m.renderedDetailLines()
	_, height := m.effectiveSize()
	start, end := visibleRange(m.detail.offset, len(rendered), height-detailReservedRows)
	for _, line := range rendered[start:end] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *tuiModel) clampDetailOffset() {
	if m.detail == nil {
		return
	}
	if m.detail.offset < 0 {
		m.detail.offset = 0
	}
	rendered := m.renderedDetailLines()
	if len(rendered) > 0 && m.detail.offset >= len(rendered) {
		m.detail.offset = len(rendered) - 1
	}
}

func (m tuiModel) renderedDetailLines() []string {
	if m.detail == nil {
		return nil
	}
	return wrapIndentedLines(m.detail.lines, m.renderWidth())
}

func (m tuiModel) renderWidth() int {
	width, _ := m.effectiveSize()
	width -= 2
	if width < 20 {
		width = 20
	}
	return width
}

func (m tuiModel) viewCompare() string {
	if m.compareA == nil || m.compareB == nil {
		return "select two tickets"
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("compare"))
	b.WriteString("\n\n")
	titleA := m.compareA.Title
	if titleA == "" {
		titleA = "<no title>"
	}
	titleB := m.compareB.Title
	if titleB == "" {
		titleB = "<no title>"
	}
	b.WriteString(fmt.Sprintf("A: %s  %s  %s\n", m.compareA.Ref, m.compareA.Status, titleA))
	b.WriteString(fmt.Sprintf("B: %s  %s  %s\n", m.compareB.Ref, m.compareB.Status, titleB))
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

func (m *tuiModel) refresh() {
	now := time.Now().UTC()
	summaries, err := m.client.ListTicketSummaries(context.Background(), now)
	if err != nil {
		m.message = "refresh failed: " + err.Error()
		return
	}
	selectedID := ""
	if m.selected >= 0 && m.selected < len(m.summaries) {
		selectedID = m.summaries[m.selected].ID
	}
	m.summaries = summaries
	m.selected = 0
	for i := range m.summaries {
		if m.summaries[i].ID == selectedID {
			m.selected = i
			break
		}
	}
	if m.selected >= len(m.summaries) {
		m.selected = len(m.summaries) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	m.clampListOffset()
	// compareA/compareB hold pointers into the old slice; re-point them by ID
	// so a reload cannot leave them dangling.
	m.compareA = findSummary(m.summaries, m.compareA)
	m.compareB = findSummary(m.summaries, m.compareB)
	// Keep an open detail view live for the same ticket.
	if m.view == "detail" && m.detail != nil {
		if updated := findSummary(m.summaries, &m.detail.summary); updated != nil {
			m.detail.summary = *updated
			var lines []string
			var err error
			if m.detail.showRefs {
				lines, err = referenceLines(m.client, m.detail.summary)
			} else {
				lines, err = ticketLines(m.client, m.detail.summary, m.renderWidth())
			}
			if err == nil {
				m.detail.lines = lines
			}
			m.clampDetailOffset()
		} else {
			m.view = "list"
			m.detail = nil
		}
	}
}

func findSummary(summaries []missis.TicketSummary, want *missis.TicketSummary) *missis.TicketSummary {
	if want == nil {
		return nil
	}
	for i := range summaries {
		if summaries[i].ID == want.ID {
			return &summaries[i]
		}
	}
	return nil
}

func (m tuiModel) statsLines() []string {
	var lines []string
	lines = append(lines, titleStyle.Render("missis stats"))
	lines = append(lines, "")
	if len(m.summaries) == 0 {
		lines = append(lines, "no tickets")
		return lines
	}
	lines = append(lines, "status")
	statusOrder := []string{"open", "doing", "blocked", "done"}
	counts := make(map[string]int)
	for _, s := range m.summaries {
		st := s.Status
		if st == "" {
			st = "(none)"
		}
		counts[st]++
	}
	seen := make(map[string]bool)
	for _, st := range statusOrder {
		if counts[st] > 0 {
			lines = append(lines, fmt.Sprintf("  %-8s %d", st, counts[st]))
			seen[st] = true
		}
	}
	var rest []string
	for st := range counts {
		if !seen[st] {
			rest = append(rest, st)
		}
	}
	sort.Strings(rest)
	for _, st := range rest {
		lines = append(lines, fmt.Sprintf("  %-8s %d", st, counts[st]))
	}
	lines = append(lines, "")
	lines = append(lines, "age (since created)")
	type ageBucket struct {
		name string
		max  time.Duration
	}
	buckets := []ageBucket{
		{"<1d", 24 * time.Hour},
		{"1-7d", 7 * 24 * time.Hour},
		{"7-30d", 30 * 24 * time.Hour},
		{">30d", 0},
	}
	now := time.Now()
	ageCounts := make(map[string]int)
	for _, s := range m.summaries {
		age := now.Sub(s.RecordedAt)
		bucket := buckets[len(buckets)-1].name
		for _, bkt := range buckets {
			if bkt.max > 0 && age <= bkt.max {
				bucket = bkt.name
				break
			}
		}
		ageCounts[bucket]++
	}
	for _, bkt := range buckets {
		lines = append(lines, fmt.Sprintf("  %-8s %d", bkt.name, ageCounts[bkt.name]))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("total: %d", len(m.summaries)))
	return lines
}

func (m tuiModel) viewStats() string {
	lines := m.statsLines()
	_, height := m.effectiveSize()
	start, end := visibleRange(m.statsOffset, len(lines), height-statsReservedRows)
	var b strings.Builder
	for _, line := range lines[start:end] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *tuiModel) clampStatsOffset() {
	if m.statsOffset < 0 {
		m.statsOffset = 0
	}
	lines := m.statsLines()
	if len(lines) > 0 && m.statsOffset >= len(lines) {
		m.statsOffset = len(lines) - 1
	}
}

func ticketLines(client *missis.Client, summary missis.TicketSummary, width int) ([]string, error) {
	now := time.Now().UTC()
	proj, err := client.ShowTicket(context.Background(), summary.Ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return nil, err
	}
	lines := []string{
		"status: " + summary.Status,
	}
	var roots []string
	for path := range proj.Parts {
		if path == "title" || path == "status" {
			continue
		}
		if strings.Contains(path, "/") {
			continue
		}
		roots = append(roots, path)
	}
	sort.Strings(roots)
	for _, path := range roots {
		lines = append(lines, partSubtree(proj, path, 0, width)...)
	}
	return lines, nil
}

func referenceLines(client *missis.Client, summary missis.TicketSummary) ([]string, error) {
	now := time.Now().UTC()
	links, err := client.ShowReferences(context.Background(), summary.Ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return []string{"<no references>"}, nil
	}
	lines := make([]string, 0, len(links))
	for _, link := range links {
		lines = append(lines, fmt.Sprintf("%s %s %s", link.Direction, link.Relation, link.To))
	}
	return lines, nil
}

func partSubtree(proj missis.TicketProjection, path string, depth int, width int) []string {
	part, ok := proj.Parts[path]
	if !ok {
		return nil
	}
	name := part.DisplayName
	if name == "" {
		name = part.Name
	}
	if part.ValueKind == "markdown" {
		name = part.Name
		if name == "" {
			name = part.DisplayName
		}
	}
	if name == "" {
		name = path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			name = path[idx+1:]
		}
	}
	indent := strings.Repeat("  ", depth)
	lines := []string{indent + "▸ " + name}
	if part.Value != nil {
		value := valueText(part.Value)
		if part.ValueKind == "markdown" {
			available := width - depth*2 - 3
			if available < 20 {
				available = 20
			}
			value = renderMarkdownBody(value, available)
		} else {
			value = renderMarkdownValue(value)
		}
		for _, valueLine := range strings.Split(value, "\n") {
			lines = append(lines, indent+"   "+valueLine)
		}
	}
	var children []string
	prefix := path + "/"
	for candidate := range proj.Parts {
		if !strings.HasPrefix(candidate, prefix) {
			continue
		}
		if strings.Contains(candidate[len(prefix):], "/") {
			continue
		}
		children = append(children, candidate)
	}
	sort.Strings(children)
	for _, childPath := range children {
		lines = append(lines, partSubtree(proj, childPath, depth+1, width)...)
	}
	return lines
}

func renderMarkdownBody(markdown string, width int) string {
	if width < 20 {
		width = 20
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("notty"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return markdown
	}
	rendered, err := renderer.Render(markdown)
	if err != nil {
		return markdown
	}
	return ansi.Strip(strings.TrimRight(rendered, "\n"))
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
	return strings.Join(alignTableRows(lines), "\n")
}

func alignTableRows(lines []string) []string {
	type tableRow []string
	var (
		rows   []tableRow
		widths []int
		out    []string
	)
	flush := func() {
		for _, row := range rows {
			separator := true
			for _, cell := range row {
				trimmed := strings.Trim(cell, "-")
				if trimmed != "" {
					separator = false
					break
				}
			}
			var b strings.Builder
			b.WriteString("|")
			for ci, cell := range row {
				b.WriteString(" ")
				if separator {
					cell = strings.Repeat("-", maxInt(widths[ci], 3))
				}
				b.WriteString(cell)
				for k := len(cell); k < widths[ci]; k++ {
					b.WriteString(" ")
				}
				b.WriteString(" |")
			}
			out = append(out, b.String())
		}
		rows = nil
		widths = nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") && strings.Count(trimmed, "|") >= 3 {
			cells := strings.Split(trimmed, "|")
			row := make(tableRow, 0, len(cells)-2)
			for _, cell := range cells[1 : len(cells)-1] {
				row = append(row, strings.TrimSpace(cell))
			}
			rows = append(rows, row)
			for ci, cell := range row {
				if ci >= len(widths) {
					widths = append(widths, 0)
				}
				if len(cell) > widths[ci] {
					widths[ci] = len(cell)
				}
			}
			continue
		}
		if len(rows) > 0 {
			flush()
		}
		out = append(out, line)
	}
	if len(rows) > 0 {
		flush()
	}
	return out
}

func ticketSummaryParts(client *missis.Client, summary missis.TicketSummary) map[string]string {
	now := time.Now().UTC()
	proj, err := client.ShowTicket(context.Background(), summary.Ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	out := make(map[string]string)
	for path, part := range proj.Parts {
		if path == "title" || path == "status" {
			continue
		}
		if part.Value == nil {
			continue
		}
		out[path] = valueText(part.Value)
	}
	return out
}

func valueText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []string:
		return strings.Join(v, ", ")
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func exportTicket(client *missis.Client, summary missis.TicketSummary) (string, error) {
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

func setTicketTitle(client *missis.Client, summary missis.TicketSummary, title string) error {
	_, err := client.Set(context.Background(), missis.RequestContext{Actor: "tui"}, missis.SetValue{Target: summary.Ref + "/title", Value: title})
	return err
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func wrapIndentedLines(lines []string, width int) []string {
	var out []string
	var tablePrefix string
	var tableRows []string
	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}
		indent := len(tablePrefix) - len(strings.TrimLeft(tablePrefix, " "))
		prefix := tablePrefix[:indent]
		out = append(out, renderTable(tableRows, width-indent, prefix)...)
		tableRows = nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isTableLine(trimmed) {
			if len(tableRows) == 0 {
				tablePrefix = line
			}
			tableRows = append(tableRows, trimmed)
			continue
		}
		flushTable()
		out = append(out, wrapLine(line, width)...)
	}
	flushTable()
	return out
}

func wrapLine(line string, width int) []string {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	prefix := line[:indent]
	content := line[indent:]
	available := width - indent
	if available < 1 {
		available = 1
	}
	runes := []rune(content)
	if len(runes) <= available {
		return []string{line}
	}
	var out []string
	for len(runes) > available {
		out = append(out, prefix+string(runes[:available]))
		runes = runes[available:]
	}
	if len(runes) > 0 {
		out = append(out, prefix+string(runes))
	}
	return out
}

func isTableLine(line string) bool {
	return strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|") && strings.Count(line, "|") >= 3
}

func renderTable(rows []string, available int, prefix string) []string {
	if available < 1 {
		available = 1
	}
	var (
		cells   [][]string
		columns int
	)
	for _, row := range rows {
		parts := strings.Split(row, "|")
		rowCells := make([]string, 0, len(parts)-2)
		for _, cell := range parts[1 : len(parts)-1] {
			rowCells = append(rowCells, strings.TrimSpace(cell))
		}
		if len(rowCells) > columns {
			columns = len(rowCells)
		}
		cells = append(cells, rowCells)
	}
	if columns == 0 {
		return nil
	}
	widths := make([]int, columns)
	for _, row := range cells {
		for ci := 0; ci < columns; ci++ {
			cell := ""
			if ci < len(row) {
				cell = row[ci]
			}
			if l := len([]rune(cell)); l > widths[ci] {
				widths[ci] = l
			}
		}
	}
	// "| " + " | " between columns + " |"
	padding := 2 + 3*(columns-1) + 2
	budget := available - padding
	if budget < columns {
		budget = columns
	}
	for sumInts(widths) > budget {
		idx := maxIndex(widths)
		if widths[idx] <= 1 {
			break
		}
		widths[idx]--
	}
	for ci := range widths {
		if widths[ci] < 1 {
			widths[ci] = 1
		}
	}
	var out []string
	for _, row := range cells {
		if isSeparatorRow(row) {
			var b strings.Builder
			b.WriteString("|")
			for ci := 0; ci < columns; ci++ {
				b.WriteString(" ")
				b.WriteString(strings.Repeat("-", widths[ci]))
				b.WriteString(" |")
			}
			out = append(out, prefix+b.String())
			continue
		}
		wrapped := make([][]string, columns)
		maxLines := 0
		for ci := 0; ci < columns; ci++ {
			cell := ""
			if ci < len(row) {
				cell = row[ci]
			}
			lines := wrapCell(cell, widths[ci])
			wrapped[ci] = lines
			if len(lines) > maxLines {
				maxLines = len(lines)
			}
		}
		for li := 0; li < maxLines; li++ {
			var b strings.Builder
			b.WriteString("|")
			for ci := 0; ci < columns; ci++ {
				text := ""
				if li < len(wrapped[ci]) {
					text = wrapped[ci][li]
				}
				b.WriteString(" ")
				b.WriteString(text)
				for k := len([]rune(text)); k < widths[ci]; k++ {
					b.WriteString(" ")
				}
				b.WriteString(" |")
			}
			out = append(out, prefix+b.String())
		}
	}
	return out
}

func wrapCell(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var out []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		current := ""
		flush := func() {
			if current != "" {
				out = append(out, current)
				current = ""
			}
		}
		for _, word := range words {
			if current == "" {
				current = word
			} else if candidate := current + " " + word; len([]rune(candidate)) <= width {
				current = candidate
			} else {
				flush()
				current = word
			}
			for len([]rune(current)) > width {
				out = append(out, string([]rune(current)[:width]))
				current = string([]rune(current)[width:])
			}
		}
		flush()
	}
	return out
}

func sumInts(values []int) int {
	total := 0
	for _, v := range values {
		total += v
	}
	return total
}

func maxIndex(values []int) int {
	idx := 0
	for i := 1; i < len(values); i++ {
		if values[i] > values[idx] {
			idx = i
		}
	}
	return idx
}

func isSeparatorRow(row []string) bool {
	for _, cell := range row {
		if strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
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
