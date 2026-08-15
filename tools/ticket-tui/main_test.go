package main

import (
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/implementation/store"
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

func TestListVisibleRows(t *testing.T) {
	m := tuiModel{}
	if got := m.listVisibleRows(); got != defaultHeight-4 {
		t.Errorf("default visible = %d, want %d", got, defaultHeight-4)
	}
	m.height = 10
	if got := m.listVisibleRows(); got != 6 {
		t.Errorf("height 10 visible = %d, want 6", got)
	}
	m.height = 3
	if got := m.listVisibleRows(); got != 1 {
		t.Errorf("tiny height visible = %d, want 1", got)
	}
}

func TestViewListRowsFitTerminal(t *testing.T) {
	m := tuiModel{width: 40, height: 10}
	for i := 1; i <= 12; i++ {
		m.summaries = append(m.summaries, store.TicketSummary{
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
