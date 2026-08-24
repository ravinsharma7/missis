// Package media contains the small, renderer-neutral media descriptor
// descriptor contract. It deliberately does not fetch, decode, or execute media.
package media

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ravinsharma7/missis/internal/model"
)

type Descriptor struct {
	Kind      model.ValueKind
	URI       string
	MediaType string
	Alt       string
	PosterURI string
	Inline    bool
}

var iframeSource = regexp.MustCompile(`(?is)<iframe\b[^>]*\bsrc\s*=\s*["']([^"']+)["']`)

// Parse accepts the revision-2 contract's explicit media kinds and structured
// descriptors carried in Value.Data. It does not infer a media kind from a
// filename, URL, or MIME type.
func Parse(kind model.ValueKind, value any) (Descriptor, bool) {
	descriptor := Descriptor{Kind: kind}

	switch typed := value.(type) {
	case string:
		descriptor.URI = strings.TrimSpace(typed)
	case model.MediaDescriptor:
		descriptor = fromModelDescriptor(typed)
	case *model.MediaDescriptor:
		if typed == nil {
			return Descriptor{}, false
		}
		descriptor = fromModelDescriptor(*typed)
	case map[string]any:
		descriptor = fromMapDescriptor(typed)
	default:
		return Descriptor{}, false
	}
	if descriptor.Kind == "" {
		descriptor.Kind = kind
	}

	if descriptor.Kind == model.ValueKindArtifact || descriptor.Kind == model.ValueKindJSON {
		return Descriptor{}, false
	}
	if !isMediaKind(descriptor.Kind) {
		return Descriptor{}, false
	}
	if descriptor.Kind == model.ValueKindEmbed {
		descriptor = normalizeEmbed(descriptor)
	}
	if descriptor.URI == "" && !descriptor.Inline {
		return Descriptor{}, false
	}
	return descriptor, true
}

func fromModelDescriptor(value model.MediaDescriptor) Descriptor {
	return Descriptor{
		Kind:      value.Kind,
		URI:       strings.TrimSpace(value.URI),
		MediaType: strings.TrimSpace(value.MediaType),
		Alt:       strings.TrimSpace(value.Alt),
		PosterURI: strings.TrimSpace(value.PosterURI),
	}
}

func fromMapDescriptor(value map[string]any) Descriptor {
	return Descriptor{
		Kind:      model.ValueKind(stringValue(value, "kind")),
		URI:       strings.TrimSpace(firstString(value, "uri", "url", "src")),
		MediaType: strings.TrimSpace(stringValue(value, "media_type")),
		Alt:       strings.TrimSpace(stringValue(value, "alt")),
		PosterURI: strings.TrimSpace(firstString(value, "poster_uri", "poster")),
	}
}

func stringValue(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(values, key); value != "" {
			return value
		}
	}
	return ""
}

func normalizeEmbed(descriptor Descriptor) Descriptor {
	if match := iframeSource.FindStringSubmatch(descriptor.URI); len(match) == 2 {
		descriptor.URI = strings.TrimSpace(match[1])
		descriptor.Inline = true
		return descriptor
	}
	if strings.Contains(strings.ToLower(descriptor.URI), "<iframe") || strings.HasPrefix(strings.TrimSpace(descriptor.URI), "<") {
		descriptor.URI = ""
		descriptor.Inline = true
	}
	return descriptor
}

func isMediaKind(kind model.ValueKind) bool {
	switch kind {
	case model.ValueKindImage, model.ValueKindVideo, model.ValueKindAudio, model.ValueKindEmbed:
		return true
	default:
		return false
	}
}

// FallbackLines returns terminal-safe text. In particular, inline embed data
// is represented by a notice and is never emitted as executable HTML.
func FallbackLines(value Descriptor) []string {
	label := string(value.Kind)
	if label == "" {
		label = "media"
	}
	alt := value.Alt
	if alt == "" {
		alt = "<no alt text>"
	}
	lines := []string{fmt.Sprintf("[%s] %s", label, clean(alt))}
	if value.Inline {
		lines = append(lines, "  source: inline embed data (not executed)")
	} else if value.URI != "" {
		lines = append(lines, "  uri: "+clean(value.URI))
	}
	if value.MediaType != "" {
		lines = append(lines, "  media type: "+clean(value.MediaType))
	}
	if value.PosterURI != "" {
		lines = append(lines, "  poster: "+clean(value.PosterURI))
	}
	switch value.Kind {
	case model.ValueKindImage:
		lines = append(lines, "  display: terminal image protocol not enabled")
	case model.ValueKindVideo:
		lines = append(lines, "  playback: external player required")
	case model.ValueKindAudio:
		lines = append(lines, "  playback: external player required")
	case model.ValueKindEmbed:
		lines = append(lines, "  display: external or web renderer required")
	}
	return lines
}

func clean(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(value))
}
