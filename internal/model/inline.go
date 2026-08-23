package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// InlineSequence is an explicit ordered value. It is separate from raw
// Markdown and recursive Part containment: slice order is semantic, while
// each item remains typed and inert.
type InlineSequence struct {
	Items []InlineItem `json:"Items"`
}

type InlineItemKind string

const (
	InlineMarkdownText InlineItemKind = "markdown-text"
	InlineCodeRef      InlineItemKind = "code-ref"
	InlineGitRef       InlineItemKind = "git-ref"
	InlineArtifact     InlineItemKind = "artifact"
	InlineImage        InlineItemKind = "image"
	InlineAudio        InlineItemKind = "audio"
	InlineVideo        InlineItemKind = "video"
	InlineRawMarkdown  InlineItemKind = "raw-markdown"
)

var builtInInlineItemKinds = []InlineItemKind{
	InlineMarkdownText, InlineCodeRef, InlineGitRef, InlineArtifact,
	InlineImage, InlineAudio, InlineVideo, InlineRawMarkdown,
}

// AllInlineItemKinds returns the complete durable inline transport vocabulary.
func AllInlineItemKinds() []InlineItemKind {
	return append([]InlineItemKind(nil), builtInInlineItemKinds...)
}

// InlineItem carries no executable payload. Data contains a typed model
// descriptor for typed items; Text is raw, inert Markdown for prose/items.
type InlineItem struct {
	ID     string         `json:"ID"`
	Kind   InlineItemKind `json:"Kind"`
	Text   string         `json:"Text,omitempty"`
	Data   any            `json:"Data,omitempty"`
	Ref    *Ref           `json:"Ref,omitempty"`
	Source *SourceRef     `json:"Source,omitempty"`
}

// CoerceInlineSequence restores typed payloads and assigns deterministic IDs
// for items submitted without one. IDs are not order keys and clients never
// calculate containment order.
func CoerceInlineSequence(value any) (InlineSequence, error) {
	var sequence InlineSequence
	switch typed := value.(type) {
	case InlineSequence:
		sequence = typed
	case *InlineSequence:
		if typed == nil {
			return InlineSequence{}, fmt.Errorf("inline sequence is nil")
		}
		sequence = *typed
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return InlineSequence{}, err
		}
		if err := json.Unmarshal(raw, &sequence); err != nil {
			return InlineSequence{}, err
		}
	}
	seen := make(map[string]struct{}, len(sequence.Items))
	for i := range sequence.Items {
		if strings.TrimSpace(sequence.Items[i].ID) == "" {
			sequence.Items[i].ID = fmt.Sprintf("inline-%06d", i+1)
		}
		if _, exists := seen[sequence.Items[i].ID]; exists {
			return InlineSequence{}, fmt.Errorf("duplicate inline item ID %q", sequence.Items[i].ID)
		}
		seen[sequence.Items[i].ID] = struct{}{}
		if err := coerceInlineItem(&sequence.Items[i]); err != nil {
			return InlineSequence{}, err
		}
	}
	return sequence, nil
}

func coerceInlineItem(item *InlineItem) error {
	if item == nil {
		return fmt.Errorf("inline item is nil")
	}
	if item.Data == nil {
		return nil
	}
	var target any
	switch item.Kind {
	case InlineCodeRef:
		target = &CodeRef{}
	case InlineGitRef:
		target = &GitRef{}
	case InlineArtifact:
		target = &ArtifactDescriptor{}
	case InlineImage, InlineAudio, InlineVideo:
		target = &MediaDescriptor{}
	default:
		return nil
	}
	raw, err := json.Marshal(item.Data)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	switch item.Kind {
	case InlineCodeRef:
		item.Data = *(target.(*CodeRef))
	case InlineGitRef:
		item.Data = *(target.(*GitRef))
	case InlineArtifact:
		item.Data = *(target.(*ArtifactDescriptor))
	case InlineImage, InlineAudio, InlineVideo:
		item.Data = *(target.(*MediaDescriptor))
	}
	return nil
}

// UnmarshalJSON restores typed inline payloads after an event is read.
func (item *InlineItem) UnmarshalJSON(data []byte) error {
	type wireItem struct {
		ID     string          `json:"ID"`
		Kind   InlineItemKind  `json:"Kind"`
		Text   string          `json:"Text"`
		Data   json.RawMessage `json:"Data"`
		Ref    *Ref            `json:"Ref"`
		Source *SourceRef      `json:"Source"`
	}
	var wire wireItem
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*item = InlineItem{ID: wire.ID, Kind: wire.Kind, Text: wire.Text, Ref: wire.Ref, Source: wire.Source}
	if len(wire.Data) == 0 || string(wire.Data) == "null" {
		return nil
	}
	var raw any
	if err := json.Unmarshal(wire.Data, &raw); err != nil {
		return err
	}
	item.Data = raw
	return coerceInlineItem(item)
}

// InlineSequenceMarkdown emits an explicit, lossless Markdown transport for
// typed items. Ordinary Markdown media syntax is never promoted: only the
// missis-inline marker denotes an explicit typed item.
func InlineSequenceMarkdown(sequence InlineSequence) (string, error) {
	sequence, err := CoerceInlineSequence(sequence)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, item := range sequence.Items {
		payload, err := json.Marshal(item)
		if err != nil {
			return "", err
		}
		out.WriteString("<!-- missis-inline ")
		out.Write(payload)
		out.WriteString(" -->\n")
		if item.Text != "" {
			out.WriteString(item.Text)
			if !strings.HasSuffix(item.Text, "\n") {
				out.WriteByte('\n')
			}
		}
	}
	return out.String(), nil
}

// ParseInlineSequenceMarkdown reads only explicit missis-inline markers.
// URLs, image syntax, HTML, and other unmarked Markdown remain inert text.
func ParseInlineSequenceMarkdown(markdown string) (InlineSequence, error) {
	var result InlineSequence
	protectedLines := markdownCodeLines([]byte(markdown))
	for lineNumber, line := range strings.Split(markdown, "\n") {
		if protectedLines[lineNumber+1] {
			continue
		}
		const prefix = "<!-- missis-inline "
		trimmed := strings.TrimSpace(line)
		if !hasMarkdownMarkerName(trimmed, "missis-inline") {
			continue
		}
		if !strings.HasPrefix(trimmed, prefix) || !strings.HasSuffix(trimmed, " -->") {
			return InlineSequence{}, fmt.Errorf("malformed missis-inline marker on line %d", lineNumber+1)
		}
		payload := strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), " -->")
		var item InlineItem
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return InlineSequence{}, fmt.Errorf("parse inline marker on line %d: %w", lineNumber+1, err)
		}
		if strings.TrimSpace(item.ID) == "" {
			return InlineSequence{}, fmt.Errorf("inline marker on line %d is missing ID", lineNumber+1)
		}
		result.Items = append(result.Items, item)
	}
	coerced, err := CoerceInlineSequence(result)
	if err != nil {
		return InlineSequence{}, err
	}
	if err := ValidateBuiltInValue(Value{Kind: ValueKindInlineSequence, Data: coerced}); err != nil {
		return InlineSequence{}, fmt.Errorf("validate inline markers: %w", err)
	}
	return coerced, nil
}

// markdownCodeLines returns source lines owned by Goldmark code-block nodes.
// Explicit transport markers inside code are examples, not Missis values;
// using the AST here prevents a fenced or indented code sample from being
// promoted merely because it contains a marker-looking comment.
func markdownCodeLines(source []byte) map[int]bool {
	protected := make(map[int]bool)
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var lines *text.Segments
		switch code := node.(type) {
		case *ast.CodeBlock:
			lines = code.Lines()
		case *ast.FencedCodeBlock:
			lines = code.Lines()
		default:
			return ast.WalkContinue, nil
		}
		if lines == nil {
			return ast.WalkSkipChildren, nil
		}
		for i := 0; i < lines.Len(); i++ {
			segment := lines.At(i)
			protected[sourceLine(source, segment.Start)] = true
		}
		return ast.WalkSkipChildren, nil
	})
	return protected
}
