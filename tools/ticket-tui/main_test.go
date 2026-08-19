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
		{"zero available becomes one", 5, 10, 0, 5, 6},
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
	if m.detail.offset != 49 {
		t.Errorf("clampDetailOffset = %d, want 49", m.detail.offset)
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
	if got := m.listVisibleRows(); got != 1 {
		t.Errorf("tiny height visible = %d, want 1", got)
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

	// G (end) anchors the last full window: content rows 34..39 at height 10.
	updated, _ := m.updateDetail(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(tuiModel)
	if m.detail.offset != 34 {
		t.Fatalf("G set offset %d, want 34", m.detail.offset)
	}
	view := m.viewDetail()
	if !strings.Contains(view, "line 34") || !strings.Contains(view, "line 39") {
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
