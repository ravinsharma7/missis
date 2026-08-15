package model

import (
	"fmt"
	"strings"
)

type MarkdownPart struct {
	Path      []string
	Body      string
	StartLine int
	EndLine   int
}

func ParseMarkdownParts(content string) ([]MarkdownPart, error) {
	lines := strings.Split(content, "\n")
	type node struct {
		level     int
		path      []string
		body      []string
		startLine int
		endLine   int
	}

	var (
		stack []*node
		parts []*node
		used  = make(map[string]int)
	)

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
		level := headingLevel(line)
		if level == 0 {
			if len(stack) > 0 {
				stack[len(stack)-1].body = append(stack[len(stack)-1].body, line)
			}
			continue
		}

		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		segment := slugifyHeading(title)

		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			flushNode(stack[len(stack)-1])
			stack = stack[:len(stack)-1]
		}

		var parentPath []string
		if len(stack) > 0 {
			parentPath = stack[len(stack)-1].path
		}
		path := append(append([]string(nil), parentPath...), segment)
		pathKey := strings.Join(path, "/")
		if count := used[pathKey]; count > 0 {
			path[len(path)-1] = fmt.Sprintf("%s-%d", segment, count+1)
			pathKey = strings.Join(path, "/")
		}
		used[pathKey]++

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

	result := make([]MarkdownPart, 0, len(parts))
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

func headingLevel(line string) int {
	if !strings.HasPrefix(line, "#") {
		return 0
	}
	level := 0
	for _, r := range line {
		if r != '#' {
			break
		}
		level++
		if level == 6 {
			break
		}
	}
	if level == 0 || level > 6 {
		return 0
	}
	if len(line) > level && line[level] != ' ' && line[level] != '\t' {
		return 0
	}
	return level
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
