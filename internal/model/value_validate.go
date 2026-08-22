package model

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"strings"

	"github.com/ravinsharma7/missis/internal/artifact"
)

// CoerceBuiltInValue restores the typed representation for JSON/API maps so
// validation and event storage do not depend on which caller supplied them.
func CoerceBuiltInValue(value Value) (Value, error) {
	if value.Data == nil {
		if value.Kind == ValueKindImage || value.Kind == ValueKindVideo || value.Kind == ValueKindAudio {
			if strings.TrimSpace(value.Text) != "" {
				value.Data = MediaDescriptor{Kind: value.Kind, URI: value.Text}
				value.Text = ""
			}
		}
		return value, nil
	}
	switch value.Kind {
	case ValueKindCodeRef:
	case ValueKindGitRef:
	case ValueKindArtifact:
	case ValueKindImage, ValueKindVideo, ValueKindAudio, ValueKindEmbed:
	case ValueKindInlineSequence:
	default:
		return value, nil
	}
	if value.Kind == ValueKindInlineSequence {
		sequence, err := CoerceInlineSequence(value.Data)
		value.Data = sequence
		return value, err
	}
	switch value.Data.(type) {
	case CodeRef, *CodeRef, GitRef, *GitRef, ArtifactDescriptor, *ArtifactDescriptor, MediaDescriptor, *MediaDescriptor:
		return value, nil
	}
	raw, err := json.Marshal(value.Data)
	if err != nil {
		return value, err
	}
	switch value.Kind {
	case ValueKindCodeRef:
		var typed CodeRef
		err = json.Unmarshal(raw, &typed)
		value.Data = typed
	case ValueKindGitRef:
		var typed GitRef
		err = json.Unmarshal(raw, &typed)
		value.Data = typed
	case ValueKindArtifact:
		var typed ArtifactDescriptor
		err = json.Unmarshal(raw, &typed)
		value.Data = typed
	case ValueKindImage, ValueKindVideo, ValueKindAudio, ValueKindEmbed:
		var typed MediaDescriptor
		err = json.Unmarshal(raw, &typed)
		value.Data = typed
	}
	return value, err
}

// ValidateBuiltInValue enforces semantic checks for structured core values.
// URLs and references are validated as data only; this function never fetches
// or executes anything.
func ValidateBuiltInValue(value Value) error {
	coerced, err := CoerceBuiltInValue(value)
	if err != nil {
		return fmt.Errorf("structured value: %w", err)
	}
	value = coerced
	switch value.Kind {
	case ValueKindCodeRef:
		ref, ok := value.Data.(CodeRef)
		if !ok {
			return fmt.Errorf("CodeRef data must be a CodeRef")
		}
		if strings.TrimSpace(ref.Repository) == "" || strings.TrimSpace(ref.Commit) == "" || strings.TrimSpace(ref.Path) == "" {
			return fmt.Errorf("CodeRef requires repository, commit, and path")
		}
		if err := validateRange("line", ref.StartLine, ref.EndLine, 1); err != nil {
			return err
		}
		return validateRange("byte", ref.StartByte, ref.EndByte, 0)
	case ValueKindGitRef:
		ref, ok := value.Data.(GitRef)
		if !ok {
			return fmt.Errorf("GitRef data must be a GitRef")
		}
		if strings.TrimSpace(ref.Repository) == "" {
			return fmt.Errorf("GitRef requires a repository")
		}
		if ref.Commit == nil && ref.Base == nil && ref.Head == nil && ref.Branch == nil && ref.Tag == nil && ref.PR == nil && ref.Diff == nil {
			return fmt.Errorf("GitRef requires at least one revision selector")
		}
		if ref.Base != nil && ref.Head == nil {
			return fmt.Errorf("GitRef base requires head")
		}
		if ref.Head != nil && ref.Base == nil {
			return fmt.Errorf("GitRef head requires base")
		}
	case ValueKindArtifact:
		ref, ok := value.Data.(ArtifactDescriptor)
		if !ok {
			return fmt.Errorf("artifact data must be an ArtifactDescriptor")
		}
		if _, err := artifact.ParseRef(ref.Ref.Entity); err != nil || ref.Ref.Kind != KindArtifact {
			return fmt.Errorf("artifact descriptor has invalid artifact reference")
		}
		if ref.Size < 0 {
			return fmt.Errorf("artifact descriptor size must be non-negative")
		}
		if ref.MediaType != "" {
			if _, _, err := mime.ParseMediaType(ref.MediaType); err != nil {
				return fmt.Errorf("artifact descriptor media type: %w", err)
			}
		}
	case ValueKindImage, ValueKindVideo, ValueKindAudio, ValueKindEmbed:
		media, ok := value.Data.(MediaDescriptor)
		if !ok {
			if value.Kind == ValueKindEmbed && strings.TrimSpace(value.Text) != "" {
				return nil
			}
			return fmt.Errorf("media data must be a MediaDescriptor")
		}
		if media.Kind != value.Kind {
			return fmt.Errorf("media descriptor kind %q does not match value kind %q", media.Kind, value.Kind)
		}
		if strings.TrimSpace(media.URI) == "" {
			return fmt.Errorf("media descriptor URI is required")
		}
		if parsed, err := url.Parse(media.URI); err != nil || parsed.Scheme == "" {
			return fmt.Errorf("media descriptor URI must be parseable and include a scheme")
		}
		if media.MediaType != "" {
			if _, _, err := mime.ParseMediaType(media.MediaType); err != nil {
				return fmt.Errorf("media descriptor media type: %w", err)
			}
		}
	case ValueKindInlineSequence:
		sequence, err := CoerceInlineSequence(value.Data)
		if err != nil {
			return fmt.Errorf("inline sequence: %w", err)
		}
		for _, item := range sequence.Items {
			switch item.Kind {
			case InlineMarkdownText, InlineRawMarkdown:
				// Text is inert, including URLs and Markdown media syntax.
			case InlineCodeRef:
				if err := ValidateBuiltInValue(Value{Kind: ValueKindCodeRef, Data: item.Data}); err != nil {
					return err
				}
			case InlineGitRef:
				if err := ValidateBuiltInValue(Value{Kind: ValueKindGitRef, Data: item.Data}); err != nil {
					return err
				}
			case InlineArtifact:
				if err := ValidateBuiltInValue(Value{Kind: ValueKindArtifact, Data: item.Data}); err != nil {
					return err
				}
			case InlineImage, InlineAudio, InlineVideo:
				if err := ValidateBuiltInValue(Value{Kind: ValueKind(item.Kind), Data: item.Data}); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown inline item kind %q", item.Kind)
			}
		}
	}
	return nil
}

func validateRange(label string, start, end *int, minimum int) error {
	if start != nil && *start < minimum {
		return fmt.Errorf("CodeRef %s start must be at least %d", label, minimum)
	}
	if end != nil && *end < minimum {
		return fmt.Errorf("CodeRef %s end must be at least %d", label, minimum)
	}
	if start != nil && end != nil && *start > *end {
		return fmt.Errorf("CodeRef %s range is reversed", label)
	}
	return nil
}
