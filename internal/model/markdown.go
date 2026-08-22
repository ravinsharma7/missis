package model

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type MarkdownPart struct {
	Path      []string
	Body      string
	StartLine int
	EndLine   int
	// Inline is populated only by the explicit missis-inline transport. Raw
	// Markdown without those markers remains Body and keeps ValueKindMarkdown.
	Inline *InlineSequence
}

func ParseMarkdownParts(content string) ([]MarkdownPart, error) {
	source := []byte(content)
	headings, err := parseMarkdownHeadings(source)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(content, "\n")
	type node struct {
		level     int
		path      []string
		body      []string
		startLine int
		endLine   int
	}

	var (
		stack       []*node
		parts       []*node
		used        = make(map[string]int)  // unsuffixed sibling path key -> occurrence count
		taken       = make(map[string]bool) // final path key -> occupied
		preamble    []string
		firstHeader int // 1-based line number of the first heading; 0 when none
	)
	headingByLine := make(map[int]markdownHeading, len(headings))
	headingSyntaxLines := make(map[int]bool)
	for _, heading := range headings {
		headingByLine[heading.startLine] = heading
		for line := heading.startLine; line <= heading.endLine; line++ {
			headingSyntaxLines[line] = true
		}
	}

	flushNode := func(n *node) {
		if n == nil {
			return
		}
		n.endLine = n.startLine
		if len(n.body) > 0 {
			n.endLine = n.startLine + len(n.body)
		}
		parts = append(parts, n)
	}

	for i, line := range lines {
		heading, isHeading := headingByLine[i+1]
		if !isHeading {
			// Setext heading underlines are part of the heading syntax, not
			// body content. Other non-heading lines, including lines inside
			// fenced code blocks, remain body content.
			if headingSyntaxLines[i+1] {
				continue
			}
			if firstHeader == 0 {
				preamble = append(preamble, line)
				continue
			}
			if len(stack) > 0 {
				stack[len(stack)-1].body = append(stack[len(stack)-1].body, line)
			}
			continue
		}
		level := heading.level
		if firstHeader == 0 {
			firstHeader = i + 1
		}

		segment := slugifyHeading(heading.title)

		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			flushNode(stack[len(stack)-1])
			stack = stack[:len(stack)-1]
		}

		var parentPath []string
		if len(stack) > 0 {
			parentPath = stack[len(stack)-1].path
		}

		// Count occurrences per unsuffixed sibling path so repeated headings
		// get deterministic suffixes, and re-check the final path for
		// collisions with headings that naturally contain those suffixes.
		baseKey := strings.Join(append(append([]string(nil), parentPath...), segment), "/")
		count := used[baseKey] + 1
		used[baseKey] = count

		candidate := segment
		if count > 1 {
			candidate = fmt.Sprintf("%s-%d", segment, count)
		}
		candidateKey := strings.Join(append(append([]string(nil), parentPath...), candidate), "/")
		for taken[candidateKey] {
			count++
			used[baseKey] = count
			candidate = fmt.Sprintf("%s-%d", segment, count)
			candidateKey = strings.Join(append(append([]string(nil), parentPath...), candidate), "/")
		}
		taken[candidateKey] = true
		path := append(append([]string(nil), parentPath...), candidate)

		n := &node{
			level:     level,
			path:      path,
			startLine: i + 1,
		}
		stack = append(stack, n)
	}
	for len(stack) > 0 {
		flushNode(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
	}

	if len(preamble) > 0 {
		preambleEnd := len(lines)
		if firstHeader > 0 {
			preambleEnd = firstHeader - 1
		}
		parts = append([]*node{{
			path:      []string{"preamble"},
			body:      preamble,
			startLine: 1,
			endLine:   preambleEnd,
		}}, parts...)
	}

	result := make([]MarkdownPart, 0, len(parts))
	sort.SliceStable(parts, func(i, j int) bool {
		return parts[i].startLine < parts[j].startLine
	})
	for _, n := range parts {
		if len(n.path) == 0 {
			continue
		}
		body := strings.TrimSpace(strings.Join(n.body, "\n"))
		result = append(result, MarkdownPart{
			Path:      append([]string(nil), n.path...),
			Body:      body,
			StartLine: n.startLine,
			EndLine:   n.endLine,
		})
	}
	return result, nil
}

type markdownHeading struct {
	level     int
	title     string
	startLine int
	endLine   int
}

// parseMarkdownHeadings delegates Markdown structure recognition to Goldmark.
// The importer still owns the missis-specific heading-to-Part projection, but
// it no longer guesses whether a line is a heading. This is important for
// fenced code blocks, setext headings, escapes, and nested Markdown syntax.
func parseMarkdownHeadings(source []byte) ([]markdownHeading, error) {
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	headings := make([]markdownHeading, 0)
	lines := strings.Split(string(source), "\n")
	err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Lines() == nil || heading.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}

		first := heading.Lines().At(0)
		last := heading.Lines().At(heading.Lines().Len() - 1)
		startLine := sourceLine(source, first.Start)
		endLine := sourceLine(source, maxOffset(last.Stop-1, last.Start))
		if startLine < len(lines) && !isATXHeadingLine(lines[startLine-1]) && isSetextUnderline(lines[startLine]) {
			endLine = startLine + 1
		}
		title := strings.TrimSpace(string(heading.Text(source)))
		headings = append(headings, markdownHeading{
			level:     heading.Level,
			title:     title,
			startLine: startLine,
			endLine:   endLine,
		})
		return ast.WalkSkipChildren, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse Markdown: %w", err)
	}
	return headings, nil
}

func sourceLine(source []byte, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	return 1 + bytes.Count(source[:offset], []byte{'\n'})
}

func maxOffset(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isSetextUnderline(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	marker := trimmed[0]
	if marker != '=' && marker != '-' {
		return false
	}
	for _, r := range trimmed {
		if r != rune(marker) {
			return false
		}
	}
	return true
}

func isATXHeadingLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	return level > 0 && level <= 6 && level < len(trimmed) && (trimmed[level] == ' ' || trimmed[level] == '\t')
}

func slugifyHeading(title string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '\t' || r == '/':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	result := strings.Trim(b.String(), "-._")
	if result == "" {
		return "section"
	}
	return result
}
