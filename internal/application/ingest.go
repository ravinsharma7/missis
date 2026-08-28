package application

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/plugin"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

type preparedIngest struct {
	metadata   artifact.Metadata
	proposal   plugin.IngestProposal
	selected   string
	invocation model.InvocationRef
	record     store.ArtifactRecord
}

func (s *Service) prepareIngest(
	ctx context.Context,
	req missis.RequestContext,
	opts missis.IngestOptions,
	stream model.Ref,
	path []string,
	parent *model.Ref,
	batchID model.BatchID,
	now time.Time,
) (preparedIngest, error) {
	if opts.Content == nil {
		return preparedIngest{}, invalidInput("ingest content is required")
	}
	operation := opts.Operation
	if operation == "" {
		operation = "attach-artifact"
	}
	mediaType := strings.TrimSpace(opts.MediaType)
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	if len(path) > 0 {
		if err := model.ValidatePathSegments(path); err != nil {
			return preparedIngest{}, validation("%v", err)
		}
	}

	metadata, err := s.artifacts.Put(ctx, opts.Content, mediaType)
	if err != nil {
		return preparedIngest{}, keepStorage(err)
	}
	artifactRef := model.Ref{Kind: model.KindArtifact, Entity: metadata.Ref.String()}
	request := plugin.IngestRequest{
		Operation:            operation,
		Target:               stream,
		Path:                 path,
		MediaType:            mediaType,
		SourceName:           opts.SourceName,
		LegacySource:         opts.LegacySource,
		ExcludeTopLevelTitle: opts.ExcludeTopLevelTitle,
		DeclaredSchema:       opts.DeclaredSchema,
		Capabilities:         append([]string(nil), opts.Capabilities...),
		Content:              opts.Content,
	}
	requestedBy := parseActor(req.Actor)
	input := plugin.IngestInput{
		Request:  request,
		Artifact: metadata,
		Open: func(openCtx context.Context) (io.ReadCloser, error) {
			return s.artifacts.Open(openCtx, metadata.Ref)
		},
		Parent:      parent,
		Invocation:  model.InvocationRef{ID: missis.NewID("run")},
		Actor:       requestedBy,
		RequestedBy: requestedBy,
		RecordedAt:  now,
		EffectiveAt: req.EffectiveAt,
		BatchID:     batchID,
		NewID:       missis.NewID,
	}
	proposal, selected, err := s.ingestion.Run(ctx, input)
	if err != nil {
		return preparedIngest{}, validation("%v", err)
	}
	invocation := input.Invocation
	if len(proposal.Events) > 0 && proposal.Events[0].Invocation != nil {
		invocation = *proposal.Events[0].Invocation
	}
	if request.LegacySource != "" {
		for i := range proposal.Events {
			for _, source := range proposal.Events[i].Sources {
				if source.Ref.Kind != artifactRef.Kind || source.Ref.Entity != artifactRef.Entity {
					continue
				}
				alias := source
				alias.Ref.Entity = request.LegacySource
				proposal.Events[i].Sources = append(proposal.Events[i].Sources, alias)
				break
			}
		}
	}
	if err := s.assignIngestOrderKeys(ctx, stream, proposal.Events, req.EffectiveAt); err != nil {
		return preparedIngest{}, err
	}
	if err := s.validateIngestEvents(ctx, stream, artifactRef, proposal.Events, invocation); err != nil {
		return preparedIngest{}, err
	}
	record := store.ArtifactRecord{
		Ref:        metadata.Ref.String(),
		Algorithm:  metadata.Algorithm,
		Digest:     metadata.Digest,
		MediaType:  metadata.MediaType,
		Size:       metadata.Size,
		Backend:    s.artifactBackend,
		RecordedAt: now,
	}
	return preparedIngest{
		metadata:   metadata,
		proposal:   proposal,
		selected:   selected,
		invocation: invocation,
		record:     record,
	}, nil
}

// assignIngestOrderKeys is intentionally core-owned. Plugins propose Parts in
// source order, but they do not choose containment keys or inspect siblings.
// Existing children are kept live and new children append after them within
// each parent stream.
func (s *Service) assignIngestOrderKeys(ctx context.Context, stream model.Ref, events []model.Event, effectiveAt time.Time) error {
	projection, err := s.CurrentStreamProjection(ctx, stream, effectiveAt)
	if err != nil {
		return keepStorage(err)
	}
	base := make(map[string]int)
	used := make(map[string]int)
	for i := range events {
		event := &events[i]
		if event.Operation != model.OpCreatePart {
			continue
		}
		parentKey := ""
		var parentID *model.PartID
		if event.Value.Ref != nil && event.Value.Ref.Kind == model.KindPart {
			parentKey = event.Value.Ref.Entity
			id := model.PartID(event.Value.Ref.Entity)
			parentID = &id
		}
		if _, ok := base[parentKey]; !ok {
			base[parentKey] = len(model.OrderedChildren(projection, parentID))
		}
		event.Value.OrderKey = model.OrderKeyForIndex(base[parentKey] + used[parentKey])
		used[parentKey]++
	}
	return nil
}

func (s *Service) Ingest(ctx context.Context, req missis.RequestContext, opts missis.IngestOptions) (missis.IngestResult, error) {
	rawReq := req
	req, now := s.normalize(req)
	if strings.TrimSpace(opts.Target) == "" {
		return missis.IngestResult{}, invalidInput("ingest target is required")
	}
	if req.IdempotencyKey != "" {
		if opts.Content == nil {
			return missis.IngestResult{}, invalidInput("ingest content is required")
		}
		mediaType := strings.TrimSpace(opts.MediaType)
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		metadata, err := s.artifacts.Put(ctx, opts.Content, mediaType)
		if err != nil {
			return missis.IngestResult{}, keepStorage(err)
		}
		ctx, err = s.withIdempotencyRequest(ctx, rawReq, "ingest", ingestFingerprint(opts, metadata.Ref.String()))
		if err != nil {
			return missis.IngestResult{}, err
		}
		var replay missis.IngestResult
		replayed, err := s.LookupIdempotencyContext(ctx, req.IdempotencyKey, &replay)
		if err != nil {
			return missis.IngestResult{}, keepStorage(err)
		}
		if replayed {
			if replay.Artifact == "" {
				return missis.IngestResult{}, validation("idempotency key already belongs to another operation: %s", req.IdempotencyKey)
			}
			return replay, nil
		}
		content, err := s.artifacts.Open(ctx, metadata.Ref)
		if err != nil {
			return missis.IngestResult{}, keepStorage(err)
		}
		defer content.Close()
		opts.Content = content
	}
	stream, basePath, err := s.resolveStreamRef(ctx, opts.Target, req.EffectiveAt)
	if err != nil {
		return missis.IngestResult{}, err
	}
	path := append(append([]string(nil), basePath...), opts.Path...)
	var parent *model.Ref
	if len(basePath) > 0 && stream.Kind == model.KindTicket {
		_, partID, _, partErr := s.resolvePartRef(ctx, opts.Target, req.EffectiveAt)
		if partErr == nil {
			parent = &model.Ref{Kind: model.KindPart, Entity: string(partID)}
		}
	}
	prepared, err := s.prepareIngest(ctx, req, opts, stream, path, parent, model.BatchID(missis.NewID("batch")), now)
	if err != nil {
		return missis.IngestResult{}, err
	}
	operation := opts.Operation
	if operation == "" {
		operation = "attach-artifact"
	}
	result := missis.IngestResult{
		Ref:       opts.Target,
		Artifact:  prepared.metadata.Ref.String(),
		Operation: operation,
		Value:     len(prepared.proposal.Events),
		Plugin:    prepared.selected,
	}
	for _, diagnostic := range prepared.proposal.Diagnostics {
		result.Diagnostics = append(result.Diagnostics, diagnostic.Message)
	}
	outcome, err := s.store.AppendArtifactBatchContext(ctx, prepared.proposal.Events, req.IdempotencyKey, nil, &result, []store.ArtifactRecord{prepared.record})
	if errors.Is(err, store.ErrIdempotencyMismatch) {
		return missis.IngestResult{}, idempotencyMismatch(err)
	}
	if errors.Is(err, store.ErrConflict) {
		return missis.IngestResult{}, conflict(err)
	}
	if err != nil {
		return missis.IngestResult{}, keepStorage(err)
	}
	if outcome.Replayed {
		return result, nil
	}
	if len(outcome.Events) > 0 {
		last := outcome.Events[len(outcome.Events)-1]
		result.Event = "@e" + strconv.FormatUint(last.AliasSeq, 10)
	}
	if req.IdempotencyKey != "" {
		if err := s.UpdateIdempotencyResultContext(ctx, req.IdempotencyKey, &result); err != nil {
			return missis.IngestResult{}, keepStorage(err)
		}
	}
	return result, nil
}

func (s *Service) validateIngestEvents(ctx context.Context, stream, artifactRef model.Ref, events []model.Event, invocation model.InvocationRef) error {
	if len(events) == 0 {
		return validation("ingestion plugin proposed no events")
	}
	for i, event := range events {
		if event.Stream.Kind != stream.Kind || event.Stream.Entity != stream.Entity {
			return validation("ingestion event %d targets a different stream", i)
		}
		if event.Operation != model.OpCreatePart || event.Target.Kind != model.KindPart {
			return validation("ingestion event %d must create a Part", i)
		}
		if err := model.ValidatePathSegments(event.Target.Path); err != nil {
			return validation("ingestion event %d: %v", i, err)
		}
		if event.Actor.Kind != "plugin" || event.Actor.ID == "" {
			return validation("ingestion event %d must identify a plugin actor", i)
		}
		if event.Invocation == nil || event.Invocation.ID != invocation.ID || event.Invocation.Plugin != invocation.Plugin || event.Invocation.Version != invocation.Version || event.Invocation.CodeHash == "" || event.Invocation.RequestedBy == nil {
			return validation("ingestion event %d has incomplete plugin provenance", i)
		}
		foundSource := false
		for _, source := range event.Sources {
			if source.Ref.Kind == artifactRef.Kind && source.Ref.Entity == artifactRef.Entity {
				foundSource = true
				break
			}
		}
		if !foundSource {
			return validation("ingestion event %d does not cite its artifact input", i)
		}
		if err := model.ValidateBuiltInValue(event.Value); err != nil {
			return validation("ingestion event %d: %v", i, err)
		}
		if err := s.kinds.ValidateValue(event.Value); err != nil {
			return validation("ingestion event %d: %v", i, err)
		}
	}
	if err := s.validateImportEvents(ctx, stream, events, events[0].EffectiveAt, events[0].RecordedAt); err != nil {
		return err
	}
	return nil
}
