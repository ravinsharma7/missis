package application

import (
	"context"
	"fmt"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

const (
	missisExternalNamespace       = "missis"
	missisProjectionID            = "missis-current"
	missisProjectionVersionAlpha1 = "missis-fold-alpha.1"
)

type externalResolutionSnapshot struct {
	store *store.ReadSnapshot
	clock missis.Clock
}

func (s *Service) OpenExternalResolutionSnapshot(ctx context.Context) (missis.ExternalAuthoritySnapshot, error) {
	snapshot, err := s.store.BeginVerifiedReadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return &externalResolutionSnapshot{store: snapshot, clock: s.clock}, nil
}

// StoreIdentityClaimContext reports exact hashed identity evidence from the
// same verified read snapshot used by entity resolution.
func (s *externalResolutionSnapshot) StoreIdentityClaimContext(ctx context.Context) (missis.StoreIdentityClaimV1, error) {
	identity, err := s.store.IdentityInfoContext(ctx)
	if err != nil {
		return missis.StoreIdentityClaimV1{}, err
	}
	genesisDigest, err := s.store.GenesisHashContext(ctx)
	if err != nil {
		return missis.StoreIdentityClaimV1{}, err
	}
	headDigest, err := s.store.HeadHashContext(ctx)
	if err != nil {
		return missis.StoreIdentityClaimV1{}, err
	}
	genesisEpoch, err := s.store.GenesisIntegrityEpochContext(ctx)
	if err != nil {
		return missis.StoreIdentityClaimV1{}, err
	}
	headEpoch, err := s.store.HeadIntegrityEpochContext(ctx)
	if err != nil {
		return missis.StoreIdentityClaimV1{}, err
	}
	eventCount, err := s.store.EventCountContext(ctx)
	if err != nil {
		return missis.StoreIdentityClaimV1{}, err
	}
	formatRevision, err := s.store.FormatRevisionContext(ctx)
	if err != nil {
		return missis.StoreIdentityClaimV1{}, err
	}
	lineageDigest, err := s.store.LatestIdentityLineageReceiptDigestContext(ctx)
	if err != nil {
		return missis.StoreIdentityClaimV1{}, err
	}
	claim := missis.NewHashedStoreIdentityClaimV1(identity.StoreID, identity.DocumentBytes, genesisDigest, genesisEpoch, headDigest, headEpoch, eventCount, formatRevision)
	claim.LineageReceiptDigest = lineageDigest
	return claim, nil
}

// ResolveExternalReferenceContext resolves from the same snapshot that
// produced the identity claim. It performs no mutation and exposes no path.
func (s *externalResolutionSnapshot) ResolveExternalReferenceContext(ctx context.Context, ref missis.ExternalReferenceV1, query missis.ExternalResolutionQuery) (missis.ExternalResolutionV1, error) {
	if err := ctx.Err(); err != nil {
		return missis.ExternalResolutionV1{}, err
	}
	if err := ref.Validate(); err != nil {
		return missis.ExternalResolutionV1{}, err
	}
	resolved := missis.ExternalResolutionV1{
		Reference:         ref,
		AuthorityState:    missis.ExternalAuthorityVerified,
		IdentityState:     missis.ExternalIdentityUnknown,
		Lifecycle:         missis.ExternalLifecycleUnknown,
		Freshness:         missis.ExternalFreshnessUnverified,
		ProjectionID:      missisProjectionID,
		ProjectionVersion: missisProjectionVersionAlpha1,
	}
	if ref.Namespace != missisExternalNamespace {
		resolved.AuthorityState = missis.ExternalAuthorityDegraded
		resolved.IdentityState = missis.ExternalIdentityUnsupported
		resolved.Warnings = []string{"consumer namespace is not supported by the Missis authority adapter"}
		return resolved, nil
	}
	if ref.Pin != nil && ref.Pin.CheckpointDigest != "" {
		resolved.AuthorityState = missis.ExternalAuthorityDegraded
		resolved.IdentityState = missis.ExternalIdentityUnsupported
		resolved.Warnings = []string{"checkpoint pin verification is not implemented"}
		return resolved, nil
	}

	stream := model.Ref{Kind: model.KindTicket, Entity: ref.EntityID}
	events, err := s.store.LoadStreamEventsContext(ctx, stream)
	if err != nil {
		return missis.ExternalResolutionV1{}, err
	}
	if len(events) == 0 {
		resolved.IdentityState = missis.ExternalIdentityMissing
		return resolved, nil
	}
	resolved.StreamRevision = events[len(events)-1].Sequence
	resolved.CurrentEventID = string(events[len(events)-1].ID)
	resolved.VerifiedThroughCursor = fmt.Sprintf("@e%d", events[len(events)-1].AliasSeq)

	if ref.Pin != nil && ref.Pin.EventID != "" {
		if !streamContainsEvent(events, ref.Pin.EventID) {
			resolved.IdentityState = missis.ExternalIdentityMissing
			resolved.Warnings = []string{"pinned event is not present in the referenced stream"}
			return resolved, nil
		}
		resolved.EvidenceRefs = append(resolved.EvidenceRefs, ref.Pin.EventID)
	}

	effectiveAt := query.EffectiveAt
	if effectiveAt.IsZero() {
		effectiveAt = s.clock.Now().UTC()
	}
	knownAt := query.KnownAt
	if knownAt.IsZero() {
		knownAt = model.MaxRecordedAt(events)
	}
	resolved.EffectiveAt = effectiveAt
	resolved.KnownAt = knownAt

	switch ref.Kind {
	case string(model.KindTicket):
		if ref.SubentityID != "" {
			resolved.IdentityState = missis.ExternalIdentityKindMismatch
			resolved.Warnings = []string{"ticket reference must not include subentity_id"}
			return resolved, nil
		}
		resolved.IdentityState = missis.ExternalIdentityMatched
		if !streamHasVisibleEvent(events, effectiveAt, knownAt) {
			resolved.Lifecycle = missis.ExternalLifecycleNotYetEffective
		} else {
			resolved.Lifecycle = missis.ExternalLifecycleActive
		}
		return resolved, nil

	case string(model.KindPart):
		if ref.SubentityID == "" {
			resolved.IdentityState = missis.ExternalIdentityKindMismatch
			resolved.Warnings = []string{"part reference requires subentity_id"}
			return resolved, nil
		}
		return resolveExternalPart(events, stream, ref.SubentityID, effectiveAt, knownAt, resolved)

	default:
		resolved.AuthorityState = missis.ExternalAuthorityDegraded
		resolved.IdentityState = missis.ExternalIdentityUnsupported
		resolved.Warnings = []string{"reference kind is not supported by the Missis authority adapter"}
		return resolved, nil
	}
}

func (s *externalResolutionSnapshot) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func streamContainsEvent(events []model.Event, eventID string) bool {
	for _, event := range events {
		if string(event.ID) == eventID {
			return true
		}
	}
	return false
}

func streamHasVisibleEvent(events []model.Event, effectiveAt, knownAt time.Time) bool {
	for _, event := range events {
		if !event.RecordedAt.After(knownAt) && !event.EffectiveAt.After(effectiveAt) {
			return true
		}
	}
	return false
}

func resolveExternalPart(events []model.Event, stream model.Ref, partID string, effectiveAt, knownAt time.Time, resolved missis.ExternalResolutionV1) (missis.ExternalResolutionV1, error) {
	historical := false
	for _, event := range events {
		if event.Target.Kind == model.KindPart && event.Target.Entity == partID {
			historical = true
		}
	}
	if !historical {
		resolved.IdentityState = missis.ExternalIdentityMissing
		return resolved, nil
	}
	projection, err := model.ProjectStream(events, stream, effectiveAt, knownAt)
	if err != nil {
		return missis.ExternalResolutionV1{}, err
	}
	resolved.IdentityState = missis.ExternalIdentityMatched
	part := projection.Parts[model.PartID(partID)]
	if part == nil {
		if !partHasVisibleEvent(events, partID, effectiveAt, knownAt) {
			resolved.Lifecycle = missis.ExternalLifecycleNotYetEffective
		} else {
			resolved.Lifecycle = missis.ExternalLifecycleRetracted
		}
		return resolved, nil
	}
	if part.RetractedBy != nil {
		resolved.Lifecycle = missis.ExternalLifecycleRetracted
		resolved.CurrentEventID = string(*part.RetractedBy)
		resolved.EvidenceRefs = append(resolved.EvidenceRefs, string(*part.RetractedBy))
		return resolved, nil
	}
	resolved.Lifecycle = missis.ExternalLifecycleActive
	resolved.CurrentEventID = string(part.CurrentFrom)
	if part.CurrentFrom != "" {
		resolved.EvidenceRefs = append(resolved.EvidenceRefs, string(part.CurrentFrom))
	}
	return resolved, nil
}

func partHasVisibleEvent(events []model.Event, partID string, effectiveAt, knownAt time.Time) bool {
	for _, event := range events {
		if event.Target.Kind == model.KindPart && event.Target.Entity == partID &&
			!event.RecordedAt.After(knownAt) && !event.EffectiveAt.After(effectiveAt) {
			return true
		}
	}
	return false
}
