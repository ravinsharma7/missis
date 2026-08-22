package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/plugin"
	"github.com/ravinsharma7/missis/pkg/missis"
)

const (
	MarkdownPluginID = "missis/markdown-import"
	ArtifactPluginID = "missis/artifact-attach"
	PluginVersion    = "1.0.0"
)

func MarkdownManifest() plugin.Manifest {
	return builtinManifest(MarkdownPluginID)
}

func ArtifactManifest() plugin.Manifest {
	return builtinManifest(ArtifactPluginID)
}

func builtinManifest(id string) plugin.Manifest {
	sum := sha256.Sum256([]byte(id + "@" + PluginVersion))
	return plugin.Manifest{ID: id, Version: PluginVersion, CodeHash: hex.EncodeToString(sum[:])}
}

// MarkdownImporter is the first-party Markdown ingestion implementation. It
// parses only the artifact selected by the application and returns event
// proposals; it has no storage access.
type MarkdownImporter struct{}

func NewMarkdownImporter() *MarkdownImporter { return &MarkdownImporter{} }

func (p *MarkdownImporter) Propose(ctx context.Context, input plugin.IngestInput) (plugin.IngestProposal, error) {
	reader, err := input.Open(ctx)
	if err != nil {
		return plugin.IngestProposal{}, err
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		return plugin.IngestProposal{}, err
	}
	parts, err := model.ParseMarkdownParts(string(content))
	if err != nil {
		return plugin.IngestProposal{}, err
	}
	if len(parts) == 0 {
		return plugin.IngestProposal{}, fmt.Errorf("Markdown artifact contains no Parts")
	}
	if input.Request.ExcludeTopLevelTitle {
		for i, part := range parts {
			if len(part.Path) == 1 && part.Path[0] != "preamble" {
				parts = append(parts[:i], parts[i+1:]...)
				break
			}
		}
	}
	for i := range parts {
		parts[i].Path = append(append([]string(nil), input.Request.Path...), parts[i].Path...)
		if sequence, parseErr := model.ParseInlineSequenceMarkdown(parts[i].Body); parseErr != nil {
			return plugin.IngestProposal{}, fmt.Errorf("parse explicit inline content: %w", parseErr)
		} else if len(sequence.Items) > 0 {
			parts[i].Inline = &sequence
		}
	}
	return plugin.IngestProposal{Events: buildPartEvents(input, parts, model.ValueKindMarkdown)}, nil
}

// ArtifactAttacher creates one explicit typed child Part. URI/media behavior
// remains data-only; the core never fetches or executes the resulting value.
type ArtifactAttacher struct{}

func NewArtifactAttacher() *ArtifactAttacher { return &ArtifactAttacher{} }

func (p *ArtifactAttacher) Propose(ctx context.Context, input plugin.IngestInput) (plugin.IngestProposal, error) {
	if err := ctx.Err(); err != nil {
		return plugin.IngestProposal{}, err
	}
	name := strings.TrimSpace(input.Request.SourceName)
	if name == "" {
		name = "artifact"
	}
	name = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "artifact"
	}
	kind := mediaKind(input.Artifact.MediaType)
	path := append(append([]string(nil), input.Request.Path...), name)
	return plugin.IngestProposal{Events: buildPartEvents(input, []model.MarkdownPart{{Path: path, StartLine: 1, EndLine: 1}}, kind)}, nil
}

func buildPartEvents(input plugin.IngestInput, parts []model.MarkdownPart, kind model.ValueKind) []model.Event {
	artifactRef := model.Ref{Kind: model.KindArtifact, Entity: input.Artifact.Ref.String()}
	actor := model.ActorRef{Kind: "plugin", ID: input.Invocation.Plugin}
	partIDs := make(map[string]model.PartID, len(parts))
	for _, part := range parts {
		partIDs[strings.Join(part.Path, "/")] = model.PartID(missis.NewID("part"))
	}
	events := make([]model.Event, 0, len(parts))
	orderByParent := make(map[string]int)
	for _, part := range parts {
		path := append([]string(nil), part.Path...)
		key := strings.Join(path, "/")
		parent := input.Parent
		if len(path) > 1 {
			parentKey := strings.Join(path[:len(path)-1], "/")
			if id, ok := partIDs[parentKey]; ok {
				ref := model.Ref{Kind: model.KindPart, Entity: string(id)}
				parent = &ref
			}
		}
		value := model.Value{Kind: kind, Ref: parent}
		valueKind := kind
		if part.Inline != nil {
			valueKind = model.ValueKindInlineSequence
			value.Data = *part.Inline
		} else if kind == model.ValueKindMarkdown {
			value.Text = part.Body
		} else if kind == model.ValueKindArtifact || kind == model.ValueKindImage || kind == model.ValueKindVideo || kind == model.ValueKindAudio {
			value.Data = descriptorFor(kind, input.Artifact)
		}
		value.Kind = valueKind
		start, end := part.StartLine, part.EndLine
		event := model.Event{
			ID:          model.EventID(missis.NewID("event")),
			Stream:      input.Request.Target,
			Operation:   model.OpCreatePart,
			Target:      model.Ref{Kind: model.KindPart, Entity: string(partIDs[key]), Path: path},
			Value:       value,
			RecordedAt:  input.RecordedAt,
			EffectiveAt: input.EffectiveAt,
			Actor:       actor,
			Sources: []model.SourceRef{{
				Ref:       artifactRef,
				MediaType: input.Artifact.MediaType,
				Span:      &model.Span{StartLine: &start, EndLine: &end},
			}},
			Inputs:     []model.Ref{artifactRef},
			BatchID:    &input.BatchID,
			Invocation: &input.Invocation,
		}
		parentKey := ""
		if len(path) > 1 {
			parentKey = strings.Join(path[:len(path)-1], "/")
		}
		event.Value.OrderKey = model.OrderKeyForIndex(orderByParent[parentKey])
		orderByParent[parentKey]++
		events = append(events, event)
	}
	return events
}

func descriptorFor(kind model.ValueKind, metadata artifact.Metadata) any {
	ref := model.Ref{Kind: model.KindArtifact, Entity: metadata.Ref.String()}
	if kind == model.ValueKindArtifact {
		return model.ArtifactDescriptor{Ref: ref, MediaType: metadata.MediaType, Size: metadata.Size}
	}
	return model.MediaDescriptor{Kind: kind, URI: metadata.Ref.String(), MediaType: metadata.MediaType}
}

func mediaKind(mediaType string) model.ValueKind {
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		return model.ValueKindImage
	case strings.HasPrefix(mediaType, "video/"):
		return model.ValueKindVideo
	case strings.HasPrefix(mediaType, "audio/"):
		return model.ValueKindAudio
	default:
		return model.ValueKindArtifact
	}
}
