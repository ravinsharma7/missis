package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestTruncateCell(t *testing.T) {
	cases := []struct {
		text  string
		width int
		want  string
	}{
		{"short", 10, "short"},
		{"exact", 5, "exact"},
		{"a very long title", 12, "a very lo..."},
		{"abc", 3, "abc"},
		{"abcdef", 2, "ab"},
		{"abc", 0, ""},
	}
	for _, tc := range cases {
		if got := truncateCell(tc.text, tc.width); got != tc.want {
			t.Errorf("truncateCell(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
		}
	}
}

func TestVisibleRange(t *testing.T) {
	cases := []struct {
		name            string
		offset, length  int
		available       int
		wantStart, want int
	}{
		{"basic window", 0, 10, 5, 0, 5},
		{"offset past end", 87, 78, 5, 78, 78},
		{"offset past end tiny available", 87, 78, 0, 78, 78},
		{"negative offset", -1, 10, 5, 0, 5},
		{"zero available gives empty window", 5, 10, 0, 5, 5},
		{"empty content", 0, 0, 5, 0, 0},
		{"window wider than content", 3, 10, 20, 3, 10},
		{"negative length", 0, -3, 5, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := visibleRange(tc.offset, tc.length, tc.available)
			if start != tc.wantStart || end != tc.want {
				t.Errorf("visibleRange(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tc.offset, tc.length, tc.available, start, end, tc.wantStart, tc.want)
			}
			length := tc.length
			if length < 0 {
				length = 0
			}
			if start < 0 || end < start || end > length {
				t.Errorf("invariant broken: start=%d end=%d length=%d", start, end, length)
			}
		})
	}
}

func TestInitSchedulesRefresh(t *testing.T) {
	m := tuiModel{}
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init() returned nil, expected a tick command")
	}
}

func TestViewDetailResizeDoesNotPanic(t *testing.T) {
	m := tuiModel{
		view:   "detail",
		width:  80,
		height: 24,
		detail: &detailState{
			summary: missis.TicketSummary{Ref: "#1", Title: "long ticket"},
			offset:  87,
		},
	}
	for i := 0; i < 100; i++ {
		m.detail.lines = append(m.detail.lines, strings.Repeat("line ", 20))
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 5})
	m = updated.(tuiModel)
	rendered := strings.Split(m.viewDetail(), "\n")
	if len(rendered) > m.height {
		t.Errorf("viewDetail rendered %d lines, terminal height %d", len(rendered), m.height)
	}

	// Shrinking content after a deep scroll must clamp, not panic.
	m.detail.lines = make([]string, 50)
	for i := range m.detail.lines {
		m.detail.lines[i] = "x"
	}
	m.detail.offset = 87
	m.clampDetailOffset()
	if m.detail.offset != 50 {
		t.Errorf("clampDetailOffset = %d, want 50", m.detail.offset)
	}
	if got := m.viewDetail(); got == "" {
		t.Error("viewDetail returned empty after clamp")
	}
}

func TestRefreshPicksUpExternalChanges(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()

	now := time.Now().UTC()
	t1, err := client.NewTicket(context.Background(), missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "one"})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := client.NewTicket(context.Background(), missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "two"})
	if err != nil {
		t.Fatal(err)
	}
	summaries, err := client.ListTicketSummaries(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	m := &tuiModel{client: client, summaries: summaries, view: "detail"}
	for i := range m.summaries {
		if m.summaries[i].ID == t2.ID {
			m.selected = i
		}
		if m.summaries[i].ID == t1.ID {
			m.detail = &detailState{summary: m.summaries[i]}
		}
	}
	m.compareA = &m.summaries[0]

	if _, err := client.Set(context.Background(), missis.RequestContext{Actor: "test"}, missis.SetValue{Target: t1.Ref + "/title", Value: "one-updated", Kind: model.ValueKindText}); err != nil {
		t.Fatal(err)
	}

	m.refresh()

	if m.selected < 0 || m.selected >= len(m.summaries) || m.summaries[m.selected].ID != t2.ID {
		t.Fatalf("selection not preserved: selected=%d summaries=%d", m.selected, len(m.summaries))
	}
	if m.compareA == nil || m.compareA.ID != t1.ID || m.compareA.Title != "one-updated" {
		t.Fatalf("compare A not re-pointed/refreshed: %+v", m.compareA)
	}
	if m.detail == nil || m.detail.summary.ID != t1.ID || m.detail.summary.Title != "one-updated" {
		t.Fatalf("detail not refreshed: %+v", m.detail)
	}
}

func TestEntityDetailSurvivesRefresh(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	created, err := client.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safe", Title: "Safe"})
	if err != nil {
		t.Fatal(err)
	}
	m := tuiModel{
		client:   client,
		view:     "detail",
		kind:     "projects",
		entities: []entityItem{{summary: missis.EntitySummary{Ref: created.Ref, ID: created.ID, Title: created.Title}}},
	}
	ent := m.entities[0].summary
	m.detail = &detailState{entity: &ent}
	m.refresh()
	if m.view != "detail" || m.detail == nil || m.detail.entity == nil || m.detail.entity.Ref != created.Ref {
		t.Fatalf("entity detail after refresh: view=%q detail=%+v", m.view, m.detail)
	}
}

func TestRefreshFailureKeepsOldData(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()

	summary, err := client.NewTicket(context.Background(), missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	old := []missis.TicketSummary{{ID: summary.ID, Ref: summary.Ref, Title: "keep", Status: "open"}}
	m := &tuiModel{client: client, summaries: old}
	// A closed store makes the next read fail.
	client.Close()
	m.refresh()
	if len(m.summaries) != 1 || m.summaries[0].Title != "keep" {
		t.Fatalf("refresh failure replaced data: %+v", m.summaries)
	}
	if !strings.Contains(m.message, "refresh failed") {
		t.Errorf("expected failure message, got %q", m.message)
	}
}

func TestDetailKeysExplicit(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()

	t1, err := client.NewTicket(context.Background(), missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "one"})
	if err != nil {
		t.Fatal(err)
	}
	summaries, err := client.ListTicketSummaries(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	m := tuiModel{client: client, summaries: summaries, view: "detail", width: 80, height: 24}
	for i := range m.summaries {
		if m.summaries[i].ID == t1.ID {
			m.detail = &detailState{summary: m.summaries[i]}
		}
	}

	// R toggles refs; r refreshes without leaving detail or touching refs.
	updated, _ := m.updateDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = updated.(tuiModel)
	if m.detail == nil || !m.detail.showRefs {
		t.Fatal("R did not toggle refs on")
	}
	updated, _ = m.updateDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(tuiModel)
	if m.view != "detail" {
		t.Fatalf("r changed view to %q", m.view)
	}
	if m.detail == nil || !m.detail.showRefs {
		t.Fatal("r toggled refs off")
	}
}

func TestViewStatsCounts(t *testing.T) {
	now := time.Now()
	m := tuiModel{summaries: []missis.TicketSummary{
		{Ref: "#1", Status: "open", RecordedAt: now.Add(-10 * time.Hour)},
		{Ref: "#2", Status: "open", RecordedAt: now.Add(-3 * 24 * time.Hour)},
		{Ref: "#3", Status: "doing", RecordedAt: now.Add(-20 * 24 * time.Hour)},
		{Ref: "#4", Status: "done", RecordedAt: now.Add(-60 * 24 * time.Hour)},
		{Ref: "#5", Status: "doing", RecordedAt: now.Add(-5 * 24 * time.Hour)},
	}}
	out := m.viewStats()
	wants := []string{
		fmt.Sprintf("  %-8s %d", "open", 2),
		fmt.Sprintf("  %-8s %d", "doing", 2),
		fmt.Sprintf("  %-8s %d", "done", 1),
		fmt.Sprintf("  %-8s %d", "<1d", 1),
		fmt.Sprintf("  %-8s %d", "1-7d", 2),
		fmt.Sprintf("  %-8s %d", "7-30d", 1),
		fmt.Sprintf("  %-8s %d", ">30d", 1),
		"total: 5",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("stats missing %q:\n%s", want, out)
		}
	}
}

func TestViewStatsTinyTerminal(t *testing.T) {
	m := tuiModel{
		width:  40,
		height: 3,
		summaries: []missis.TicketSummary{
			{Ref: "#1", Status: "open", RecordedAt: time.Now().Add(-time.Hour)},
		},
	}
	rendered := strings.Split(m.viewStats(), "\n")
	if len(rendered) > m.height {
		t.Errorf("viewStats rendered %d lines, terminal height %d", len(rendered), m.height)
	}
}

func TestListVisibleRows(t *testing.T) {
	m := tuiModel{}
	if got := m.listVisibleRows(); got != defaultHeight-5 {
		t.Errorf("default visible = %d, want %d", got, defaultHeight-5)
	}
	m.height = 10
	if got := m.listVisibleRows(); got != 5 {
		t.Errorf("height 10 visible = %d, want 5", got)
	}
	m.message = "exported #1 -> out.md"
	if got := m.listVisibleRows(); got != 4 {
		t.Errorf("height 10 visible with message = %d, want 4", got)
	}
	m.message = ""
	m.height = 3
	if got := m.listVisibleRows(); got != 0 {
		t.Errorf("tiny height visible = %d, want 0", got)
	}
}

func TestViewFitsTerminalHeight(t *testing.T) {
	for _, height := range []int{24, 10, 8, 6} {
		for _, msg := range []string{"", "exported #1 -> out.md"} {
			m := tuiModel{width: 80, height: height, message: msg}
			for i := 1; i <= 40; i++ {
				m.summaries = append(m.summaries, missis.TicketSummary{
					Ref:    fmt.Sprintf("#%d", i),
					Status: "open",
					Title:  "ticket with a deliberately long title that must be truncated",
				})
			}
			if got := len(strings.Split(m.View(), "\n")); got > height {
				t.Errorf("list view at height %d (message %q) rendered %d lines", height, msg, got)
			}

			m.view = "detail"
			m.detail = &detailState{summary: m.summaries[0]}
			for i := 0; i < 40; i++ {
				m.detail.lines = append(m.detail.lines, fmt.Sprintf("content line %02d with padding", i))
			}
			if got := len(strings.Split(m.View(), "\n")); got > height {
				t.Errorf("detail view at height %d (message %q) rendered %d lines", height, msg, got)
			}

			m.view = "stats"
			if got := len(strings.Split(m.View(), "\n")); got > height {
				t.Errorf("stats view at height %d (message %q) rendered %d lines", height, msg, got)
			}
		}
	}
}

func TestDetailRefsViewOmitsNoPartsLine(t *testing.T) {
	m := tuiModel{
		view:   "detail",
		width:  80,
		height: 10,
		detail: &detailState{
			summary:  missis.TicketSummary{Ref: "#1", Title: "t"},
			lines:    []string{"<no references>"},
			showRefs: true,
		},
	}
	if strings.Contains(m.View(), "<no parts>") {
		t.Errorf("refs view with no references should not render <no parts>:\n%s", m.View())
	}
	if got := len(strings.Split(m.View(), "\n")); got > m.height {
		t.Errorf("refs view rendered %d lines, terminal height %d", got, m.height)
	}
}

func TestDetailScrollReachesBothEnds(t *testing.T) {
	m := tuiModel{
		view:   "detail",
		width:  80,
		height: 10,
		detail: &detailState{summary: missis.TicketSummary{Ref: "#1", Title: "t"}},
	}
	for i := 0; i < 40; i++ {
		m.detail.lines = append(m.detail.lines, fmt.Sprintf("line %02d", i))
	}

	// G (end) anchors the last full window: content rows 35..39 at height 10.
	updated, _ := m.updateDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(tuiModel)
	if m.detail.offset != 35 {
		t.Fatalf("G set offset %d, want 35", m.detail.offset)
	}
	view := m.viewDetail()
	if !strings.Contains(view, "line 35") || !strings.Contains(view, "line 39") {
		t.Errorf("bottom page does not show the final window:\n%s", view)
	}

	// Scrolling up must reach the top (offset 0, line 00 visible).
	for i := 0; i < 50; i++ {
		updated, _ := m.updateDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		m = updated.(tuiModel)
	}
	if m.detail.offset != 0 {
		t.Fatalf("scrolling up stopped at offset %d, want 0", m.detail.offset)
	}
	if view := m.viewDetail(); !strings.Contains(view, "line 00") {
		t.Errorf("top page does not show the first content line:\n%s", view)
	}
}

func TestListPagingMovesCursorWithWindow(t *testing.T) {
	m := tuiModel{width: 80, height: 10}
	for i := 1; i <= 20; i++ {
		m.summaries = append(m.summaries, missis.TicketSummary{
			Ref:    fmt.Sprintf("#%d", i),
			Status: "open",
			Title:  "t",
		})
	}
	visible := m.listVisibleRows()
	if visible != 5 {
		t.Fatalf("listVisibleRows = %d, want 5", visible)
	}
	assertCursor := func(t *testing.T, step string) {
		t.Helper()
		if m.selected < m.listOffset || m.selected >= m.listOffset+visible {
			t.Fatalf("%s: cursor %d outside window [%d,%d)", step, m.selected, m.listOffset, m.listOffset+visible)
		}
	}

	// pgdown pages the cursor a full window each time, keeping it visible.
	for _, want := range []int{5, 10, 15, 19} {
		updated, _ := m.updateList(tea.KeyMsg{Type: tea.KeyPgDown})
		m = updated.(tuiModel)
		if m.selected != want {
			t.Fatalf("pgdown selected = %d, want %d", m.selected, want)
		}
		assertCursor(t, "pgdown")
	}
	// At the end, the window is anchored to the last page.
	if m.listOffset != 15 {
		t.Fatalf("pgdown window offset = %d, want 15", m.listOffset)
	}

	// pgup pages back up and returns to the top.
	updated, _ := m.updateList(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(tuiModel)
	if m.selected != 14 {
		t.Fatalf("pgup selected = %d, want 14", m.selected)
	}
	assertCursor(t, "pgup")
	for i := 0; i < 5; i++ {
		updated, _ := m.updateList(tea.KeyMsg{Type: tea.KeyPgUp})
		m = updated.(tuiModel)
		assertCursor(t, "pgup")
	}
	if m.selected != 0 || m.listOffset != 0 {
		t.Fatalf("pgup to top: selected=%d offset=%d, want 0/0", m.selected, m.listOffset)
	}

	// Line-wise scrolling still works after paging.
	updated, _ = m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(tuiModel)
	if m.selected != 1 || m.listOffset != 0 {
		t.Fatalf("j after pgup: selected=%d offset=%d, want 1/0", m.selected, m.listOffset)
	}
	updated, _ = m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(tuiModel)
	if m.selected != 0 || m.listOffset != 0 {
		t.Fatalf("k after j: selected=%d offset=%d, want 0/0", m.selected, m.listOffset)
	}
}

func TestListPagingEmptyListDoesNotPanic(t *testing.T) {
	m := tuiModel{width: 80, height: 10}
	if _, err := m.updateList(tea.KeyMsg{Type: tea.KeyPgDown}); err != nil {
		t.Fatal(err)
	}
	if m.selected != 0 {
		t.Fatalf("selected = %d, want 0", m.selected)
	}
}

func TestViewListRowsFitTerminal(t *testing.T) {
	m := tuiModel{width: 40, height: 10}
	for i := 1; i <= 12; i++ {
		m.summaries = append(m.summaries, missis.TicketSummary{
			Ref:    "#" + strings.Repeat("1", i),
			Status: "open",
			Title:  "ticket with a deliberately long title that must be truncated",
		})
	}
	rendered := strings.Split(m.viewList(), "\n")
	if len(rendered) > m.height {
		t.Errorf("viewList rendered %d lines, terminal height %d", len(rendered), m.height)
	}
	for _, line := range rendered {
		if len([]rune(line)) > m.width {
			t.Errorf("row exceeds width %d: %q (%d)", m.width, line, len([]rune(line)))
		}
	}
	if !strings.Contains(rendered[4], "...") {
		t.Errorf("expected truncated title in row, got %q", rendered[4])
	}
}

func TestAlignTableRows(t *testing.T) {
	in := []string{
		"| A | Long Value |",
		"| --- | --- |",
		"| x | y |",
	}
	out := alignTableRows(in)
	if len(out) != len(in) {
		t.Fatalf("got %d lines, want %d", len(out), len(in))
	}
	for _, line := range out {
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			t.Errorf("table row not wrapped in pipes: %q", line)
		}
	}
	if out[1] != "| --- | ---------- |" {
		t.Errorf("separator row = %q", out[1])
	}
	if out[2] != "| x   | y          |" {
		t.Errorf("data row = %q", out[2])
	}
}

func TestRenderMarkdownValueTable(t *testing.T) {
	value := "Header\n\n| Ref | Status |\n| --- | --- |\n| #1 | open |"
	rendered := renderMarkdownValue(value)
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "|") {
			if !strings.HasSuffix(line, "|") {
				t.Errorf("table line not terminated with pipe: %q", line)
			}
		}
	}
	if !strings.Contains(rendered, "| #1  | open   |") {
		t.Errorf("table row not rendered:\n%s", rendered)
	}
}

func TestWrapIndentedLinesTableWrapsCells(t *testing.T) {
	lines := []string{
		"   | Norm | Reason |",
		"   | --- | --- |",
		"   | N002 | a very long reason that must wrap inside its own column |",
	}
	width := 40
	out := wrapIndentedLines(lines, width)
	for _, line := range out {
		if len([]rune(line)) > width {
			t.Errorf("line exceeds width %d: %q", width, line)
		}
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "|") {
			if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
				t.Errorf("table line not bounded by pipes: %q", line)
			}
		}
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "N002") || !strings.Contains(joined, "very long reason") {
		t.Errorf("cell content lost:\n%s", joined)
	}
}

func TestWrapCellUsesWordBoundaries(t *testing.T) {
	got := wrapCell("alpha beta gamma delta", 10)
	for _, line := range got {
		if len([]rune(line)) > 10 {
			t.Errorf("wrapped line too long: %q", line)
		}
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "alpha beta") || !strings.Contains(joined, "gamma") {
		t.Errorf("word wrap lost content:\n%s", joined)
	}
}

func TestBreadcrumbForViews(t *testing.T) {
	cases := []struct {
		name string
		m    tuiModel
		want string
	}{
		{"tickets list", tuiModel{view: "list", kind: "tickets"}, "missis / tickets (all tickets)"},
		{"tickets list with context", tuiModel{view: "list", kind: "tickets", projectCtx: "safedesign"}, "missis / tickets (project: safedesign · group: none)"},
		{"projects list ignores ticket context", tuiModel{view: "list", kind: "projects", projectCtx: "safedesign"}, "missis / projects"},
		{"stats with scope", tuiModel{view: "stats", kind: "tickets", projectCtx: "safedesign"}, "missis / tickets / stats (project: safedesign · group: none)"},
		{"stats without scope", tuiModel{view: "stats", kind: "tickets"}, "missis / tickets / stats (all tickets)"},
		{"entity detail", tuiModel{view: "detail", kind: "projects", detail: &detailState{entity: &missis.EntitySummary{Ref: "project:safedesign", Title: "SafeDesign"}}}, "missis / projects / project:safedesign SafeDesign"},
		{"ticket detail", tuiModel{view: "detail", kind: "tickets", detail: &detailState{summary: missis.TicketSummary{Ref: "#12", Title: "Fix"}}}, "missis / tickets / #12 Fix"},
		{"create prompt", tuiModel{view: "input", kind: "projects", inputMode: "create"}, "missis / projects / create"},
		{"create ticket prompt", tuiModel{view: "input", inputMode: "create-ticket"}, "missis / tickets / create"},
		{"context prompt", tuiModel{view: "context"}, "missis / context"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.breadcrumb(); got != tc.want {
				t.Errorf("breadcrumb = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKeyHintsPerViewAndKind(t *testing.T) {
	join := func(m tuiModel) string {
		parts := make([]string, 0, 4)
		for _, h := range m.keyHints() {
			parts = append(parts, h.key+" "+h.action)
		}
		return strings.Join(parts, " | ")
	}
	tickets := join(tuiModel{view: "list", kind: "tickets"})
	for _, want := range []string{"n create ticket", "c/v compare", "e export", "s stats", "q quit"} {
		if !strings.Contains(tickets, want) {
			t.Errorf("ticket list help missing %q: %s", want, tickets)
		}
	}
	projects := join(tuiModel{view: "list", kind: "projects"})
	for _, want := range []string{"n create project", "q quit"} {
		if !strings.Contains(projects, want) {
			t.Errorf("projects list help missing %q: %s", want, projects)
		}
	}
	for _, banned := range []string{"stats", "export", "compare"} {
		if strings.Contains(projects, banned) {
			t.Errorf("projects list help advertises %q: %s", banned, projects)
		}
	}
	ticketDetail := join(tuiModel{view: "detail", kind: "tickets", detail: &detailState{summary: missis.TicketSummary{Ref: "#1"}}})
	for _, want := range []string{"T edit title", "l links", "e export", "t/p/g lists", "b back"} {
		if !strings.Contains(ticketDetail, want) {
			t.Errorf("ticket detail help missing %q: %s", want, ticketDetail)
		}
	}
	entityDetail := join(tuiModel{view: "detail", kind: "projects", detail: &detailState{entity: &missis.EntitySummary{Ref: "project:s"}}})
	for _, banned := range []string{"edit title", "links", "export"} {
		if strings.Contains(entityDetail, banned) {
			t.Errorf("entity detail help advertises %q: %s", banned, entityDetail)
		}
	}
	if !strings.Contains(entityDetail, "R refs") {
		t.Errorf("entity detail help missing R refs: %s", entityDetail)
	}
	if !strings.Contains(entityDetail, "f filter tickets") {
		t.Errorf("entity detail help missing f filter tickets: %s", entityDetail)
	}
	if !strings.Contains(entityDetail, "t/p/g lists") {
		t.Errorf("entity detail help missing t/p/g lists: %s", entityDetail)
	}
	groupDetail := join(tuiModel{view: "detail", kind: "groups", detail: &detailState{entity: &missis.EntitySummary{Ref: "group:eng"}}})
	if !strings.Contains(groupDetail, "l links") {
		t.Errorf("group detail help missing l links: %s", groupDetail)
	}
	contextHelp := join(tuiModel{view: "context"})
	for _, want := range []string{"j/k move", "space toggle", "enter apply", "b back"} {
		if !strings.Contains(contextHelp, want) {
			t.Errorf("context help missing %q: %s", want, contextHelp)
		}
	}
	helpView := join(tuiModel{view: "help"})
	for _, want := range []string{"j/k scroll", "b back"} {
		if !strings.Contains(helpView, want) {
			t.Errorf("help view help missing %q: %s", want, helpView)
		}
	}
	compareView := join(tuiModel{view: "compare"})
	if !strings.Contains(compareView, "j/k scroll") {
		t.Errorf("compare help missing j/k scroll: %s", compareView)
	}
	input := join(tuiModel{view: "input"})
	for _, want := range []string{"enter save", "esc cancel", "←/→ cursor", "home/end jump", "backspace delete"} {
		if !strings.Contains(input, want) {
			t.Errorf("input help missing %q: %s", want, input)
		}
	}
}

func TestHelpLinesWrapToWidth(t *testing.T) {
	m := tuiModel{view: "list", kind: "tickets", width: 40, height: 24}
	lines := m.helpLines()
	if len(lines) < 2 {
		t.Fatalf("expected wrapped help at width 40, got %d line(s): %q", len(lines), lines)
	}
	if m.helpRows() != len(lines) {
		t.Errorf("helpRows = %d, want %d", m.helpRows(), len(lines))
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"n create ticket", "x context", "q quit"} {
		if !strings.Contains(joined, want) {
			t.Errorf("wrapped help lost %q:\n%s", want, joined)
		}
	}
	for _, line := range lines {
		if len([]rune(line)) > 40 {
			t.Errorf("help line exceeds width 40: %q", line)
		}
	}
}

func TestQQuitsFromSubpagesAndRoot(t *testing.T) {
	for _, view := range []string{"detail", "compare", "stats"} {
		m := tuiModel{view: view, detail: &detailState{summary: missis.TicketSummary{Ref: "#1"}}}
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		if cmd == nil {
			t.Errorf("q on %s should quit", view)
		}
	}
	m := tuiModel{view: "list"}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q on list should quit")
	}
}

func TestBBacksOutOfSubpages(t *testing.T) {
	m := tuiModel{view: "detail", detail: &detailState{summary: missis.TicketSummary{Ref: "#1"}}}
	updated, cmd := m.updateDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = updated.(tuiModel)
	if cmd != nil || m.view != "list" || m.detail != nil {
		t.Fatalf("b on detail: view=%q cmd=%v detail=%v, want list/nil/nil", m.view, cmd, m.detail)
	}

	m = tuiModel{view: "compare", compareA: &missis.TicketSummary{Ref: "#1"}, compareB: &missis.TicketSummary{Ref: "#2"}}
	updated, cmd = m.updateCompare(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = updated.(tuiModel)
	if cmd != nil || m.view != "list" || m.compareA != nil || m.compareB != nil {
		t.Fatalf("b on compare: view=%q cmd=%v, want list/nil", m.view, cmd)
	}

	m = tuiModel{view: "stats"}
	updated, cmd = m.updateStats(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = updated.(tuiModel)
	if cmd != nil || m.view != "list" {
		t.Fatalf("b on stats: view=%q cmd=%v, want list/nil", m.view, cmd)
	}
}

func TestCompareScroll(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	t1, err := client.NewTicket(ctx, req, missis.NewTicketOptions{Title: "one"})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := client.NewTicket(ctx, req, missis.NewTicketOptions{Title: "two"})
	if err != nil {
		t.Fatal(err)
	}
	var long strings.Builder
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&long, "content line %d\n", i)
	}
	if _, err := client.Set(ctx, req, missis.SetValue{Target: t1.Ref + "/plan", Value: long.String(), Kind: model.ValueKindMarkdown}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Set(ctx, req, missis.SetValue{Target: t2.Ref + "/plan", Value: long.String(), Kind: model.ValueKindMarkdown}); err != nil {
		t.Fatal(err)
	}
	summaries, err := client.ListTicketSummaries(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	m := tuiModel{client: client, view: "compare", width: 80, height: 24}
	for i := range summaries {
		if summaries[i].ID == t1.ID {
			m.compareA = &summaries[i]
		}
		if summaries[i].ID == t2.ID {
			m.compareB = &summaries[i]
		}
	}
	maxOffset := len(m.compareLines()) - m.compareWindowRows()
	if maxOffset < 1 {
		t.Fatalf("compare content too short to scroll: %d lines, window %d", len(m.compareLines()), m.compareWindowRows())
	}
	updated, _ := m.updateCompare(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(tuiModel)
	if m.compareOffset != maxOffset {
		t.Fatalf("G offset = %d, want %d", m.compareOffset, maxOffset)
	}
	if out := m.viewCompare(); !strings.Contains(out, "content line 80") {
		t.Errorf("bottom window missing last content line:\n%s", out)
	}
	for i := 0; i < 500; i++ {
		updated, _ = m.updateCompare(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		m = updated.(tuiModel)
	}
	if m.compareOffset != 0 {
		t.Fatalf("k to top offset = %d, want 0", m.compareOffset)
	}
	if out := m.viewCompare(); !strings.Contains(out, "content line 1") {
		t.Errorf("top window missing first content line:\n%s", out)
	}
}

func TestTPGFromSubpagesSwitchesList(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	if _, err := client.NewTicket(context.Background(), missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "one"}); err != nil {
		t.Fatal(err)
	}

	detail := &detailState{summary: missis.TicketSummary{Ref: "#1"}}
	for _, tc := range []struct {
		key  rune
		kind string
	}{
		{'t', "tickets"},
		{'p', "projects"},
		{'g', "groups"},
	} {
		m := tuiModel{client: client, view: "detail", kind: "tickets", detail: detail}
		updated, cmd := m.updateDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
		m = updated.(tuiModel)
		if cmd != nil || m.view != "list" || m.kind != tc.kind {
			t.Fatalf("%c on detail: view=%q kind=%q cmd=%v, want list/%s/nil", tc.key, m.view, m.kind, cmd, tc.kind)
		}
	}

	m := tuiModel{client: client, view: "stats", kind: "tickets"}
	updated, cmd := m.updateStats(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(tuiModel)
	if cmd != nil || m.view != "list" || m.kind != "groups" {
		t.Fatalf("g on stats: view=%q kind=%q cmd=%v, want list/groups/nil", m.view, m.kind, cmd)
	}

	m = tuiModel{client: client, view: "compare", kind: "tickets", compareA: &missis.TicketSummary{}, compareB: &missis.TicketSummary{}}
	updated, cmd = m.updateCompare(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(tuiModel)
	if cmd != nil || m.view != "list" || m.kind != "projects" {
		t.Fatalf("p on compare: view=%q kind=%q cmd=%v, want list/projects/nil", m.view, m.kind, cmd)
	}
}

func TestErrorDismissedWhenBackingOut(t *testing.T) {
	m := tuiModel{view: "detail", detail: &detailState{summary: missis.TicketSummary{Ref: "#1"}}, err: fmt.Errorf("boom")}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = updated.(tuiModel)
	if m.err != nil || m.view != "list" {
		t.Fatalf("back from error screen: err=%v view=%q, want nil/list", m.err, m.view)
	}
}

func TestErrorDismissedOnRefresh(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	if _, err := client.NewTicket(context.Background(), missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "one"}); err != nil {
		t.Fatal(err)
	}
	m := tuiModel{client: client, view: "list", kind: "tickets", err: fmt.Errorf("boom")}
	m.refresh()
	if m.err != nil {
		t.Fatalf("refresh did not dismiss the error: %v", m.err)
	}
}

func TestErrorDismissedOnEscAtRoot(t *testing.T) {
	m := tuiModel{view: "list", kind: "tickets", err: fmt.Errorf("boom")}
	updated, _ := m.updateList(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(tuiModel)
	if m.err != nil || m.view != "list" {
		t.Fatalf("esc at root: err=%v view=%q, want nil/list", m.err, m.view)
	}
}

func TestNavigationKeysDismissErrorsAcrossViews(t *testing.T) {
	keyMessage := func(key string) tea.KeyMsg {
		switch key {
		case "up":
			return tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			return tea.KeyMsg{Type: tea.KeyDown}
		case "left":
			return tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			return tea.KeyMsg{Type: tea.KeyRight}
		case "pgup":
			return tea.KeyMsg{Type: tea.KeyPgUp}
		case "pgdown":
			return tea.KeyMsg{Type: tea.KeyPgDown}
		case "home":
			return tea.KeyMsg{Type: tea.KeyHome}
		case "end":
			return tea.KeyMsg{Type: tea.KeyEnd}
		case "esc":
			return tea.KeyMsg{Type: tea.KeyEsc}
		case "enter":
			return tea.KeyMsg{Type: tea.KeyEnter}
		case "ctrl+c":
			return tea.KeyMsg{Type: tea.KeyCtrlC}
		default:
			return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		}
	}

	cases := []struct {
		name      string
		view      string
		kind      string
		key       string
		wantClear bool
	}{
		{"list scroll", "list", "tickets", "j", true},
		{"list left arrow is not navigation", "list", "tickets", "left", false},
		{"list page", "list", "tickets", "pgdown", true},
		{"list home", "list", "tickets", "home", true},
		{"list end", "list", "tickets", "G", true},
		{"list back alias", "list", "tickets", "esc", true},
		{"list switch", "list", "tickets", "p", true},
		{"list refresh", "list", "tickets", "r", true},
		{"list stats transition", "list", "tickets", "s", true},
		{"list context transition", "list", "tickets", "x", true},
		{"list help transition", "list", "tickets", "?", true},
		{"list selection", "list", "tickets", "enter", true},
		{"entity list does not treat stats as navigation", "list", "projects", "s", false},
		{"detail scroll", "detail", "tickets", "k", true},
		{"detail page", "detail", "tickets", "pgup", true},
		{"detail end", "detail", "tickets", "end", true},
		{"detail back", "detail", "tickets", "b", true},
		{"ticket detail filter is not navigation", "detail", "tickets", "f", false},
		{"detail right arrow is not navigation", "detail", "tickets", "right", false},
		{"detail refresh", "detail", "tickets", "r", true},
		{"detail help transition", "detail", "tickets", "?", true},
		{"compare scroll", "compare", "tickets", "j", true},
		{"compare page", "compare", "tickets", "pgdown", true},
		{"compare back", "compare", "tickets", "b", true},
		{"compare list switch", "compare", "tickets", "t", true},
		{"compare help transition", "compare", "tickets", "?", true},
		{"stats scroll", "stats", "tickets", "j", true},
		{"stats home", "stats", "tickets", "home", true},
		{"stats back", "stats", "tickets", "b", true},
		{"stats list switch", "stats", "tickets", "g", true},
		{"stats help transition", "stats", "tickets", "?", true},
		{"context move", "context", "tickets", "k", true},
		{"context page", "context", "tickets", "pgdown", true},
		{"context end", "context", "tickets", "G", true},
		{"context back", "context", "tickets", "b", true},
		{"context refresh", "context", "tickets", "r", true},
		{"context selection", "context", "tickets", "enter", true},
		{"context scope toggle", "context", "tickets", " ", true},
		{"context list switch", "context", "tickets", "t", true},
		{"context question mark is not global", "context", "tickets", "?", false},
		{"help scroll", "help", "tickets", "j", true},
		{"help page", "help", "tickets", "pgdown", true},
		{"help back", "help", "tickets", "b", true},
		{"help list switch", "help", "tickets", "p", true},
		{"help question mark is not a transition", "help", "tickets", "?", false},
		{"input printable navigation rune", "input", "tickets", "j", false},
		{"input enter is submit", "input", "tickets", "enter", false},
		{"input question mark is text", "input", "tickets", "?", false},
		{"input back rune is text", "input", "tickets", "b", false},
		{"export action", "list", "tickets", "e", false},
		{"create action", "list", "tickets", "n", false},
		{"compare action", "list", "tickets", "c", false},
		{"compare second-selection action", "list", "tickets", "v", false},
		{"quit", "list", "tickets", "q", false},
		{"hard exit", "list", "tickets", "ctrl+c", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tuiModel{view: tc.view, kind: tc.kind, err: fmt.Errorf("boom")}
			got := m.dismissErrorForKey(keyMessage(tc.key)).err == nil
			if got != tc.wantClear {
				t.Fatalf("view=%q key=%q clear=%v, want %v", tc.view, tc.key, got, tc.wantClear)
			}
		})
	}
}

func TestEntityDetailFilterDismissesError(t *testing.T) {
	m := tuiModel{
		view:   "detail",
		kind:   "projects",
		detail: &detailState{entity: &missis.EntitySummary{Ref: "project:p1"}},
		err:    fmt.Errorf("boom"),
	}
	updated := m.dismissErrorForKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if updated.err != nil {
		t.Fatalf("entity-detail filter kept error: %v", updated.err)
	}
}

func TestUpdateDismissesErrorBeforeGlobalAndViewDispatch(t *testing.T) {
	m := tuiModel{view: "detail", kind: "tickets", err: fmt.Errorf("boom")}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(tuiModel)
	if cmd != nil || m.err != nil || m.view != "help" {
		t.Fatalf("help transition: err=%v view=%q cmd=%v, want nil/help/nil", m.err, m.view, cmd)
	}

	m = tuiModel{view: "list", kind: "tickets", err: fmt.Errorf("boom")}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(tuiModel)
	if m.err != nil {
		t.Fatalf("list navigation did not dismiss error: %v", m.err)
	}

	m = tuiModel{view: "input", inputMode: "create", err: fmt.Errorf("boom")}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(tuiModel)
	if m.err == nil || m.input != "j" {
		t.Fatalf("input text changed error semantics: err=%v input=%q", m.err, m.input)
	}
}

func TestActionErrorsAndInputValidationRemainIndependent(t *testing.T) {
	for _, key := range []rune{'e', 'n', 'c', 'v'} {
		m := tuiModel{view: "list", kind: "tickets", err: fmt.Errorf("boom")}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		m = updated.(tuiModel)
		if m.err == nil {
			t.Fatalf("action %q dismissed an existing error", key)
		}
	}

	m := tuiModel{
		view:      "input",
		inputMode: "create",
		input:     "abc",
		inputErr:  "validation failed",
		err:       fmt.Errorf("page failure"),
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(tuiModel)
	if m.inputErr != "validation failed" || m.err == nil {
		t.Fatalf("cursor navigation changed errors: inputErr=%q err=%v", m.inputErr, m.err)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	if m.inputErr != "" || m.err == nil {
		t.Fatalf("input edit did not isolate validation error: inputErr=%q err=%v", m.inputErr, m.err)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(tuiModel)
	if m.inputErr != "" || m.err != nil {
		t.Fatalf("abandoning prompt did not clear prompt/page errors: inputErr=%q err=%v", m.inputErr, m.err)
	}
}

func TestQTypesInInput(t *testing.T) {
	m := tuiModel{view: "input", inputMode: "create", input: ""}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(tuiModel)
	if m.view != "input" || m.input != "q" {
		t.Fatalf("q in input: view=%q input=%q", m.view, m.input)
	}
}

func TestQuestionMarkTypesInInput(t *testing.T) {
	m := tuiModel{view: "input", inputMode: "create", input: ""}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(tuiModel)
	if m.view != "input" || m.input != "?" {
		t.Fatalf("? in input: view=%q input=%q", m.view, m.input)
	}
}

func TestGlobalKeyExceptions(t *testing.T) {
	m := tuiModel{view: "context", kind: "tickets"}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(tuiModel)
	if cmd != nil || m.view != "context" {
		t.Fatalf("q on context: view=%q cmd=%v, want context/nil", m.view, cmd)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(tuiModel)
	if cmd != nil || m.view != "context" {
		t.Fatalf("? on context: view=%q cmd=%v, want context/nil", m.view, cmd)
	}

	m = tuiModel{view: "help"}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(tuiModel)
	if cmd != nil || m.view != "help" {
		t.Fatalf("? on help: view=%q cmd=%v, want help/nil", m.view, cmd)
	}
}

func TestHelpViewOpensAndBacks(t *testing.T) {
	m := tuiModel{view: "detail", detail: &detailState{summary: missis.TicketSummary{Ref: "#1"}}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(tuiModel)
	if cmd != nil || m.view != "help" || m.prevView != "detail" {
		t.Fatalf("? on detail: view=%q prev=%q cmd=%v", m.view, m.prevView, cmd)
	}
	updated, cmd = m.updateHelp(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = updated.(tuiModel)
	if cmd != nil || m.view != "detail" || m.prevView != "" {
		t.Fatalf("b from help: view=%q prev=%q cmd=%v", m.view, m.prevView, cmd)
	}
	m = tuiModel{view: "help"}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q on help should quit")
	}
}

func TestHelpContentCoversViews(t *testing.T) {
	m := tuiModel{view: "help", width: 80, height: 24}
	content := strings.Join(m.helpContent(), "\n")
	for _, want := range []string{"tickets list", "context picker", "q quit regular views", "T edit title", "enter apply", "global:", "context/help ? inert"} {
		if !strings.Contains(content, want) {
			t.Errorf("help content missing %q", want)
		}
	}

	for _, line := range (tuiModel{view: "help", width: 40, height: 24}).helpContent() {
		if len([]rune(line)) > 40 {
			t.Errorf("help content line exceeds width 40: %q", line)
		}
	}
}

func TestCreateTicketFromTicketList(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	summaries, err := client.ListTicketSummaries(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	m := tuiModel{client: client, summaries: summaries, view: "list", kind: "tickets", width: 80, height: 24}
	updated, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(tuiModel)
	if m.view != "input" || m.inputMode != "create-ticket" {
		t.Fatalf("n on ticket list: view=%q inputMode=%q", m.view, m.inputMode)
	}
	m.input = "Brand new ticket"
	updated, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if m.view != "list" || m.inputMode != "" {
		t.Fatalf("after create: view=%q inputMode=%q", m.view, m.inputMode)
	}
	if len(m.summaries) != 1 || m.summaries[0].Title != "Brand new ticket" {
		t.Fatalf("ticket not created: %+v", m.summaries)
	}
	if !strings.Contains(m.message, "created #1") {
		t.Errorf("unexpected message %q", m.message)
	}
}

func TestKindSwitchNoticesWhenAlreadyOnList(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	summaries, err := client.ListTicketSummaries(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	m := tuiModel{client: client, summaries: summaries, view: "list", kind: "tickets"}
	updated, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(tuiModel)
	if m.kind != "tickets" || !strings.Contains(m.message, "already on tickets") {
		t.Fatalf("t on tickets list: kind=%q message=%q", m.kind, m.message)
	}

	updated, _ = m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(tuiModel)
	if m.kind != "projects" || m.message != "" {
		t.Fatalf("switch to projects: kind=%q message=%q, want projects/empty", m.kind, m.message)
	}
	updated, _ = m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(tuiModel)
	if !strings.Contains(m.message, "already on projects") {
		t.Fatalf("p on projects list: message=%q", m.message)
	}

	updated, _ = m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(tuiModel)
	if m.kind != "groups" || m.message != "" {
		t.Fatalf("switch to groups: kind=%q message=%q, want groups/empty", m.kind, m.message)
	}
	updated, _ = m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(tuiModel)
	if !strings.Contains(m.message, "already on groups") {
		t.Fatalf("g on groups list: message=%q", m.message)
	}
}

func TestEntityListShowsFullIDs(t *testing.T) {
	m := tuiModel{
		view:     "list",
		kind:     "projects",
		width:    80,
		height:   24,
		entities: []entityItem{{summary: missis.EntitySummary{Ref: "project:safedesign", Title: "SafeDesign"}, counts: entityCounts{groups: 2, tickets: 5}}},
	}
	out := m.viewList()
	if !strings.Contains(out, "project:safedesign") {
		t.Errorf("entity list truncated ID:\n%s", out)
	}
	if strings.Contains(out, "STATUS") {
		t.Errorf("entity list still shows STATUS column:\n%s", out)
	}
	if !strings.Contains(out, "ID") {
		t.Errorf("entity list missing ID header:\n%s", out)
	}
	if !strings.Contains(out, "MEMBERS") {
		t.Errorf("entity list missing MEMBERS header:\n%s", out)
	}
	if !strings.Contains(out, "2 groups · 5 tickets") {
		t.Errorf("entity list missing membership counts:\n%s", out)
	}
}

func TestEntityDetailTitleStatusOnce(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	if _, err := client.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safe", Title: "SafeDesign"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	items, err := client.ListEntities(ctx, model.KindProject, missis.ListFilter{EffectiveAt: now, KnownAt: now})
	if err != nil || len(items) != 1 {
		t.Fatalf("ListEntities: %d items, err=%v", len(items), err)
	}
	lines, err := entityLines(client, items[0], 80)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Join(lines, "\n")
	if strings.Contains(out, "▸ title") || strings.Contains(out, "▸ status") {
		t.Errorf("entity detail still renders title/status parts:\n%s", out)
	}
	if got := strings.Count(out, "title: "); got != 1 {
		t.Errorf("entity detail has %d title: lines, want 1:\n%s", got, out)
	}
	if got := strings.Count(out, "status: "); got != 1 {
		t.Errorf("entity detail has %d status: lines, want 1:\n%s", got, out)
	}
}

func TestMembershipCounts(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	if _, err := client.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safe", Title: "Safe"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewEntity(ctx, req, missis.EntityOptions{Kind: "group", ID: "eng", Title: "Eng"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewTicket(ctx, req, missis.NewTicketOptions{Title: "homed", Project: "safe"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetLink(ctx, req, missis.LinkOptions{Ref: "group:eng/links", Relation: "contains", Target: "project:safe", Add: true}); err != nil {
		t.Fatal(err)
	}
	project := membershipCounts(client, "project:safe")
	if project.groups != 1 || project.tickets != 1 {
		t.Fatalf("project counts = %+v, want 1 group and 1 ticket", project)
	}
	group := membershipCounts(client, "group:eng")
	if group.projects != 1 {
		t.Fatalf("group counts = %+v, want 1 project", group)
	}
}

func TestFilterTicketsFromEntityDetail(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	if _, err := client.NewTicket(context.Background(), missis.RequestContext{Actor: "test"}, missis.NewTicketOptions{Title: "one"}); err != nil {
		t.Fatal(err)
	}
	m := tuiModel{
		client: client,
		view:   "detail",
		kind:   "projects",
		detail: &detailState{entity: &missis.EntitySummary{Ref: "project:safe", Title: "Safe"}},
	}
	updated, _ := m.updateDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(tuiModel)
	if m.view != "context" || !m.draftScope.contains("project", "safe") {
		t.Fatalf("after f: view=%q draft=%+v", m.view, m.draftScope)
	}
	if !strings.Contains(m.message, "scope added to draft") {
		t.Errorf("draft message = %q", m.message)
	}
}

func TestGroupLinkActionsFromGroupDetail(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	if _, err := client.NewEntity(ctx, req, missis.EntityOptions{Kind: "group", ID: "eng", Title: "Eng"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewTicket(ctx, req, missis.NewTicketOptions{Title: "one"}); err != nil {
		t.Fatal(err)
	}

	m := tuiModel{
		client: client,
		view:   "detail",
		kind:   "groups",
		detail: &detailState{entity: &missis.EntitySummary{Ref: "group:eng", Title: "Eng"}},
	}
	updated, _ := m.updateDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(tuiModel)
	if m.view != "input" || m.inputMode != "link" {
		t.Fatalf("l on group detail: view=%q inputMode=%q", m.view, m.inputMode)
	}
	if out := m.View(); !strings.Contains(out, "contains:<project|ticket>") {
		t.Errorf("group link prompt missing relation hints:\n%s", out)
	}

	m.input = "add contains:#1"
	updated, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if m.view != "detail" || !strings.Contains(m.message, "link updated") {
		t.Fatalf("add contains from group: view=%q message=%q", m.view, m.message)
	}

	m.input = "add has-home:project:x"
	m.view = "input"
	m.inputMode = "link"
	updated, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if !strings.Contains(m.inputErr, "group links support contains") {
		t.Errorf("invalid group relation error = %q", m.inputErr)
	}
}

func TestViewListEmptyState(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want string
	}{
		{"tickets", "no tickets yet"},
		{"projects", "no projects yet"},
		{"groups", "no groups yet"},
	} {
		m := tuiModel{view: "list", kind: tc.kind, width: 80, height: 24}
		if out := m.viewList(); !strings.Contains(out, tc.want) {
			t.Errorf("%s empty list missing %q:\n%s", tc.kind, tc.want, out)
		}
	}
	scoped := tuiModel{view: "list", kind: "tickets", projectCtx: "safe", width: 80, height: 24}
	if out := scoped.viewList(); !strings.Contains(out, "no tickets in project: safe yet") {
		t.Errorf("scoped empty list missing scope-aware message:\n%s", out)
	}
}

func TestStatsEmptyStateMatchesScope(t *testing.T) {
	m := tuiModel{view: "stats", kind: "tickets", projectCtx: "safe"}
	if out := strings.Join(m.statsLines(), "\n"); !strings.Contains(out, "no tickets in project: safe yet") {
		t.Errorf("scoped stats empty state missing scope-aware message:\n%s", out)
	}
}

func TestLoadTicketSummariesFiltersByContext(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	if _, err := client.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safe", Title: "Safe"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewTicket(ctx, req, missis.NewTicketOptions{Title: "homed", Project: "safe"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewTicket(ctx, req, missis.NewTicketOptions{Title: "loose"}); err != nil {
		t.Fatal(err)
	}
	all, err := loadTicketSummaries(client, "none", "none")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unscoped load = %d tickets, want 2", len(all))
	}
	filtered, err := loadTicketSummaries(client, "safe", "none")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].Title != "homed" {
		t.Fatalf("scoped load = %+v, want only the homed ticket", filtered)
	}
}

func TestRenderInputCursorPosition(t *testing.T) {
	if got := renderInput("p: ", "abc", 1); got != "p: a▌bc" {
		t.Errorf("renderInput = %q, want %q", got, "p: a▌bc")
	}
	if got := renderInput("p: ", "abc", 99); got != "p: abc▌" {
		t.Errorf("renderInput clamp end = %q, want %q", got, "p: abc▌")
	}
	if got := renderInput("p: ", "abc", -2); got != "p: ▌abc" {
		t.Errorf("renderInput clamp start = %q, want %q", got, "p: ▌abc")
	}
}

func TestLinkPromptShowsRelations(t *testing.T) {
	m := tuiModel{view: "input", inputMode: "link", width: 80, height: 24, detail: &detailState{summary: missis.TicketSummary{Ref: "#1"}}}
	out := m.View()
	for _, want := range []string{"relations:", "blocks:<ticket>", "has-home:<project>", "move project:<id>"} {
		if !strings.Contains(out, want) {
			t.Errorf("link prompt missing %q:\n%s", want, out)
		}
	}
	// The relation hint and help lines must never exceed the terminal width
	// (lipgloss pads lines to the widest block width, so measure trimmed).
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimRight(line, " ")
		if strings.HasPrefix(trimmed, "Ticket ") {
			continue // the prompt itself can be long; it does not wrap
		}
		if len([]rune(trimmed)) > 80 {
			t.Errorf("link prompt line exceeds width 80: %q", line)
		}
	}
}

func TestCursorMovementInInput(t *testing.T) {
	m := tuiModel{view: "input", input: "abcd", cursor: 4}
	updated, _ := m.updateInput(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(tuiModel)
	if m.cursor != 3 {
		t.Fatalf("left cursor = %d, want 3", m.cursor)
	}
	updated, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	m = updated.(tuiModel)
	if m.input != "abcXd" || m.cursor != 4 {
		t.Fatalf("insert at cursor: input=%q cursor=%d, want abcXd/4", m.input, m.cursor)
	}
	updated, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(tuiModel)
	if m.cursor != 0 {
		t.Fatalf("home cursor = %d, want 0", m.cursor)
	}
	updated, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	if m.cursor != 1 {
		t.Fatalf("right cursor = %d, want 1", m.cursor)
	}
	updated, _ = m.updateInput(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(tuiModel)
	if m.input != "bcXd" || m.cursor != 0 {
		t.Fatalf("backspace at cursor: input=%q cursor=%d, want bcXd/0", m.input, m.cursor)
	}
}

func TestContextPickerSelectsProject(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	if _, err := client.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "safedesign", Title: "SafeDesign"}); err != nil {
		t.Fatal(err)
	}
	m := tuiModel{client: client, view: "list", kind: "tickets"}
	updated, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	if m.view != "context" {
		t.Fatalf("x opened %q, want context", m.view)
	}
	rows := m.contextRows()
	found := -1
	for i, row := range rows {
		if row.kind == "project" && row.ref == "safedesign" {
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("picker missing project:safedesign: %+v", rows)
	}
	m.ctxSelected = found
	updated, _ = m.updateContext(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(tuiModel)
	updated, _ = m.updateContext(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if m.view != "list" || m.kind != "tickets" || m.projectCtx != "safedesign" || m.groupCtx != "none" {
		t.Fatalf("after select: view=%q kind=%q project=%q group=%q", m.view, m.kind, m.projectCtx, m.groupCtx)
	}
	if !strings.Contains(m.message, "ticket list context: project=safedesign") {
		t.Errorf("confirmation message = %q", m.message)
	}
}

func TestContextPickerSupportsMultiScopeDrafts(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	for _, entity := range []missis.EntityOptions{
		{Kind: "project", ID: "p1", Title: "Project 1"},
		{Kind: "project", ID: "p2", Title: "Project 2"},
		{Kind: "group", ID: "g1", Title: "Group 1"},
	} {
		if _, err := client.NewEntity(ctx, req, entity); err != nil {
			t.Fatal(err)
		}
	}
	m := tuiModel{client: client, view: "list", kind: "tickets", projectCtx: "p1", groupCtx: "none", width: 100, height: 30}
	updated, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)

	selectRow := func(kind, ref string) {
		t.Helper()
		for i, row := range m.contextRows() {
			if row.kind == kind && row.ref == ref {
				m.ctxSelected = i
				updated, _ := m.updateContext(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
				m = updated.(tuiModel)
				return
			}
		}
		t.Fatalf("context row %s:%s not found", kind, ref)
	}
	selectRow("project", "p2")
	selectRow("group", "g1")
	if !m.activeScope.contains("project", "p1") || m.activeScope.contains("project", "p2") {
		t.Fatalf("active scope changed before apply: %+v", m.activeScope)
	}
	if !m.draftScope.contains("project", "p1") || !m.draftScope.contains("project", "p2") || !m.draftScope.contains("group", "g1") {
		t.Fatalf("draft scope = %+v, want p1,p2,g1", m.draftScope)
	}
	contextView := m.viewContext()
	for _, want := range []string{"Active: project=p1 group=none", "Draft:  project=p1,p2 group=g1"} {
		if !strings.Contains(contextView, want) {
			t.Errorf("context view missing %q:\n%s", want, contextView)
		}
	}
	updated, _ = m.updateContext(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(tuiModel)
	if m.view != "list" || m.projectCtx != "p1" || m.groupCtx != "none" {
		t.Fatalf("cancel changed active context: view=%q project=%q group=%q", m.view, m.projectCtx, m.groupCtx)
	}

	updated, _ = m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	updated, _ = m.updateContext(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(tuiModel)
	selectRow("group", "g1")
	updated, _ = m.updateContext(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if len(m.activeScope.Projects) != 0 || len(m.activeScope.Groups) != 1 || m.activeScope.Groups[0] != "g1" {
		t.Fatalf("clean apply active scope = %+v", m.activeScope)
	}

	updated, _ = m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	updated, _ = m.updateContext(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(tuiModel)
	updated, _ = m.updateContext(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if !m.activeScope.empty() || m.projectCtx != "none" || m.groupCtx != "none" {
		t.Fatalf("clear all active scope = %+v, project=%q group=%q", m.activeScope, m.projectCtx, m.groupCtx)
	}
}

func TestContextPickerDistinguishesAllAndUnscopedWithCounts(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	for _, entity := range []missis.EntityOptions{
		{Kind: "project", ID: "p1", Title: "Project 1"},
		{Kind: "group", ID: "g1", Title: "Group 1"},
	} {
		if _, err := client.NewEntity(ctx, req, entity); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.NewTicket(ctx, req, missis.NewTicketOptions{Title: "project ticket", Project: "p1"}); err != nil {
		t.Fatal(err)
	}
	directGroup, err := client.NewTicket(ctx, req, missis.NewTicketOptions{Title: "group ticket"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewTicket(ctx, req, missis.NewTicketOptions{Title: "unscoped ticket"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SetLink(ctx, req, missis.LinkOptions{Ref: "group:g1/links", Relation: "contains", Target: directGroup.Ref, Add: true}); err != nil {
		t.Fatal(err)
	}

	m := tuiModel{client: client, view: "list", kind: "tickets", width: 100, height: 30}
	updated, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	if !m.contextLoaded {
		t.Fatal("opening context should load picker counts")
	}
	if got := m.contextCounts["all"]; got != 3 {
		t.Fatalf("all count = %d, want 3", got)
	}
	if got := m.contextCounts["unscoped"]; got != 1 {
		t.Fatalf("unscoped count = %d, want 1", got)
	}
	if got := m.contextCounts["project:p1"]; got != 1 {
		t.Fatalf("project count = %d, want 1", got)
	}
	if got := m.contextCounts["group:g1"]; got != 1 {
		t.Fatalf("group count = %d, want 1", got)
	}
	view := m.viewContext()
	for _, want := range []string{"(all tickets) — 3", "(unscoped tickets) — 1", "Draft matches: 3"} {
		if !strings.Contains(view, want) {
			t.Errorf("context view missing %q:\n%s", want, view)
		}
	}

	selectRow := func(kind, ref string) {
		t.Helper()
		for i, row := range m.contextRows() {
			if row.kind == kind && row.ref == ref {
				m.ctxSelected = i
				updated, _ := m.updateContext(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
				m = updated.(tuiModel)
				return
			}
		}
		t.Fatalf("context row %s:%s not found", kind, ref)
	}
	selectRow("unscoped", "")
	if !m.draftScope.Unscoped || len(m.draftScope.Projects) != 0 || len(m.draftScope.Groups) != 0 {
		t.Fatalf("unscoped draft = %+v", m.draftScope)
	}
	selectRow("project", "p1")
	if m.draftScope.Unscoped || len(m.draftScope.Projects) != 1 || m.draftScope.Projects[0] != "p1" {
		t.Fatalf("project should replace unscoped mode: %+v", m.draftScope)
	}
	if !m.draftCountReady || m.draftCount != 1 {
		t.Fatalf("draft count = %d ready=%v err=%v, want 1", m.draftCount, m.draftCountReady, m.draftCountErr)
	}
}

func TestContextPickerRefreshesAndReclamps(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	if _, err := client.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "p1", Title: "Project 1"}); err != nil {
		t.Fatal(err)
	}
	m := tuiModel{
		client:      client,
		view:        "context",
		kind:        "tickets",
		activeScope: scopeFromLegacy("p1", "none"),
		draftScope:  scopeFromLegacy("p1", "none"),
		width:       80,
		height:      24,
	}
	before := m.contextRows()
	for _, row := range before {
		if row.kind == "project" && row.ref == "p2" {
			t.Fatal("p2 unexpectedly existed before refresh")
		}
	}
	if _, err := client.NewEntity(ctx, req, missis.EntityOptions{Kind: "project", ID: "p2", Title: "Project 2"}); err != nil {
		t.Fatal(err)
	}
	m.ctxSelected = len(before) + 10
	m.ctxOffset = len(before) + 10
	updated, _ := m.updateContext(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(tuiModel)
	rows := m.contextRows()
	found := false
	for _, row := range rows {
		if row.kind == "project" && row.ref == "p2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("refresh did not expose p2: %+v", rows)
	}
	if m.message != "scope list refreshed" {
		t.Fatalf("refresh message = %q", m.message)
	}
	if m.ctxSelected < 0 || m.ctxSelected >= len(rows) {
		t.Fatalf("selection not reclamped: selected=%d rows=%d", m.ctxSelected, len(rows))
	}
	if m.ctxOffset < 0 {
		t.Fatalf("window offset not reclamped: %d", m.ctxOffset)
	}
	if !m.activeScope.contains("project", "p1") || !m.draftScope.contains("project", "p1") {
		t.Fatalf("refresh changed active/draft scope: active=%+v draft=%+v", m.activeScope, m.draftScope)
	}
}

func TestContextPickerCreateRows(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()
	m := tuiModel{client: client, view: "context"}
	rows := m.contextRows()
	createProject, createGroup := -1, -1
	for i, row := range rows {
		if row.kind == "create-project" {
			createProject = i
		}
		if row.kind == "create-group" {
			createGroup = i
		}
	}
	if createProject < 0 || createGroup < 0 {
		t.Fatalf("picker missing create rows: %+v", rows)
	}
	m.ctxSelected = createProject
	updated, _ := m.updateContext(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if m.view != "input" || m.inputMode != "create" || m.kind != "projects" {
		t.Fatalf("create project row: view=%q mode=%q kind=%q", m.view, m.inputMode, m.kind)
	}
}

func TestContextPickerPagesOnShortTerminal(t *testing.T) {
	dir := t.TempDir()
	svc, err := application.OpenPath(filepath.Join(dir, "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := missis.NewClient(svc)
	defer client.Close()

	ctx := context.Background()
	req := missis.RequestContext{Actor: "test"}
	for i := 0; i < 4; i++ {
		if _, err := client.NewEntity(ctx, req, missis.EntityOptions{
			Kind:  "project",
			ID:    fmt.Sprintf("project-%d", i),
			Title: fmt.Sprintf("Project %d", i),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := client.NewEntity(ctx, req, missis.EntityOptions{
			Kind:  "group",
			ID:    fmt.Sprintf("group-%d", i),
			Title: fmt.Sprintf("Group %d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	m := tuiModel{client: client, view: "context", width: 80, height: 10}
	rows := m.contextRows()
	if len(rows) < 10 {
		t.Fatalf("context rows = %d, want enough rows to page: %+v", len(rows), rows)
	}
	m.clampContextWindow()
	visible := m.contextWindowRows()
	if visible <= 0 || visible >= len(rows) {
		t.Fatalf("context window = %d for %d rows, want a non-empty partial window", visible, len(rows))
	}

	first := m.viewContext()
	if !strings.Contains(first, rows[0].label) {
		t.Fatalf("first context page missing %q:\n%s", rows[0].label, first)
	}
	if strings.Contains(first, rows[len(rows)-1].label) {
		t.Fatalf("first context page unexpectedly renders bottom row %q:\n%s", rows[len(rows)-1].label, first)
	}
	if got := len(strings.Split(m.View(), "\n")); got > m.height {
		t.Fatalf("context view rendered %d lines in terminal height %d:\n%s", got, m.height, m.View())
	}

	for i := 0; i < len(rows)-1; i++ {
		updated, _ := m.updateContext(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(tuiModel)
	}
	if m.ctxSelected != len(rows)-1 {
		t.Fatalf("down navigation selected %d, want %d", m.ctxSelected, len(rows)-1)
	}
	if m.ctxOffset == 0 {
		t.Fatalf("down navigation never advanced the context window")
	}
	last := m.viewContext()
	if !strings.Contains(last, rows[len(rows)-1].label) {
		t.Fatalf("last context page missing bottom row %q:\n%s", rows[len(rows)-1].label, last)
	}
	if !strings.Contains(last, fmt.Sprintf("%d/%d", len(rows), len(rows))) {
		t.Fatalf("last context page missing position indicator:\n%s", last)
	}

	updated, _ := m.updateContext(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(tuiModel)
	if m.ctxSelected != 0 || m.ctxOffset != 0 {
		t.Fatalf("home navigation selected=%d offset=%d, want 0/0", m.ctxSelected, m.ctxOffset)
	}
}
