package model

import (
	"reflect"
	"sort"
	"testing"
)

func TestParseMarkdownDuplicateSiblings(t *testing.T) {
	content := "## Evidence\nA\n\n## Evidence\nB\n\n## Evidence\nC\n"
	parts, err := ParseMarkdownParts(content)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, part := range parts {
		paths = append(paths, joinPath(part.Path))
	}
	want := []string{"evidence", "evidence-2", "evidence-3"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	bodies := map[string]string{}
	for _, part := range parts {
		bodies[joinPath(part.Path)] = part.Body
	}
	if bodies["evidence"] != "A" || bodies["evidence-2"] != "B" || bodies["evidence-3"] != "C" {
		t.Fatalf("unexpected bodies: %v", bodies)
	}
}

func TestParseMarkdownNaturalSuffixCollision(t *testing.T) {
	content := "## Evidence\nA\n\n## Evidence 2\nB\n\n## Evidence\nC\n"
	parts, err := ParseMarkdownParts(content)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, part := range parts {
		paths = append(paths, joinPath(part.Path))
	}
	want := []string{"evidence", "evidence-2", "evidence-3"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestParseMarkdownDuplicatesUnderDifferentParents(t *testing.T) {
	content := "## A\n\n### X\none\n\n## B\n\n### X\ntwo\n"
	parts, err := ParseMarkdownParts(content)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, part := range parts {
		paths = append(paths, joinPath(part.Path))
	}
	sort.Strings(paths)
	want := []string{"a", "a/x", "b", "b/x"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestParseMarkdownPreamblePreserved(t *testing.T) {
	content := "This introductory context is important.\n\n## Problem\n\nThe problem body.\n"
	parts, err := ParseMarkdownParts(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("got %d parts, want 2: %+v", len(parts), parts)
	}
	if !reflect.DeepEqual(parts[0].Path, []string{"preamble"}) {
		t.Fatalf("first part path = %v, want [preamble]", parts[0].Path)
	}
	if parts[0].Body != "This introductory context is important." {
		t.Fatalf("preamble body = %q", parts[0].Body)
	}
	if parts[0].StartLine != 1 || parts[0].EndLine != 2 {
		t.Fatalf("preamble span = %d..%d, want 1..2", parts[0].StartLine, parts[0].EndLine)
	}
	if !reflect.DeepEqual(parts[1].Path, []string{"problem"}) || parts[1].Body != "The problem body." {
		t.Fatalf("second part = %+v", parts[1])
	}
}

func TestParseMarkdownPreambleOnlyDocument(t *testing.T) {
	content := "Only preamble content.\nNo headings at all.\n"
	parts, err := ParseMarkdownParts(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 {
		t.Fatalf("got %d parts, want 1", len(parts))
	}
	if !reflect.DeepEqual(parts[0].Path, []string{"preamble"}) {
		t.Fatalf("path = %v, want [preamble]", parts[0].Path)
	}
	if parts[0].Body != "Only preamble content.\nNo headings at all." {
		t.Fatalf("body = %q", parts[0].Body)
	}
	if parts[0].StartLine != 1 || parts[0].EndLine != 3 {
		t.Fatalf("span = %d..%d, want 1..3", parts[0].StartLine, parts[0].EndLine)
	}
}

func TestParseMarkdownDoesNotTreatFencedHeadingAsPart(t *testing.T) {
	content := "## Evidence\n\n```markdown\n## Not a part\nbody\n```\n\n### Actual child\nchild body\n"
	parts, err := ParseMarkdownParts(content)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		paths = append(paths, joinPath(part.Path))
	}
	want := []string{"evidence", "evidence/actual-child"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	if parts[0].Body != "```markdown\n## Not a part\nbody\n```" {
		t.Fatalf("fenced body = %q", parts[0].Body)
	}
}

func TestParseMarkdownRecognizesSetextHeading(t *testing.T) {
	content := "Problem\n=======\n\nThe body.\n"
	parts, err := ParseMarkdownParts(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || joinPath(parts[0].Path) != "problem" || parts[0].Body != "The body." {
		t.Fatalf("parts = %+v", parts)
	}
}

func joinPath(path []string) string {
	out := ""
	for i, segment := range path {
		if i > 0 {
			out += "/"
		}
		out += segment
	}
	return out
}
