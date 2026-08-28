package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

type idempotencyRequestEnvelopeV1 struct {
	Operation   string `json:"operation"`
	Actor       string `json:"actor"`
	EffectiveAt string `json:"effective_at,omitempty"`
	KnownAt     string `json:"known_at,omitempty"`
	IfCurrent   string `json:"if_current,omitempty"`
	Because     string `json:"because,omitempty"`
	Payload     any    `json:"payload"`
}

type mutationFingerprintV1 struct {
	Kind  string `json:"kind"`
	Value any    `json:"value"`
}

type ingestFingerprintV1 struct {
	Operation            string   `json:"operation"`
	Target               string   `json:"target"`
	Path                 []string `json:"path,omitempty"`
	MediaType            string   `json:"media_type"`
	SourceName           string   `json:"source_name,omitempty"`
	LegacySource         string   `json:"legacy_source,omitempty"`
	ExcludeTopLevelTitle bool     `json:"exclude_top_level_title,omitempty"`
	DeclaredSchema       string   `json:"declared_schema,omitempty"`
	Capabilities         []string `json:"capabilities,omitempty"`
	Artifact             string   `json:"artifact"`
}

// withIdempotencyRequest binds a user-provided key to the normalized public
// request, excluding server-assigned IDs, sequence numbers, and defaulted
// transaction/effective times. Explicit caller times remain part of the
// request. Equivalent aliases are not collapsed in v1: callers must retry the
// same public request representation.
func (s *Service) withIdempotencyRequest(ctx context.Context, req missis.RequestContext, operation string, payload any) (context.Context, error) {
	if req.IdempotencyKey == "" {
		return ctx, nil
	}
	actor := req.Actor
	if actor == "" {
		actor = "human/local"
	}
	envelope := idempotencyRequestEnvelopeV1{
		Operation:   operation,
		Actor:       actor,
		EffectiveAt: explicitRequestTime(req.EffectiveAt),
		KnownAt:     explicitRequestTime(req.KnownAt),
		IfCurrent:   req.IfCurrent,
		Because:     req.Because,
		Payload:     payload,
	}
	hash, err := store.ComputeIdempotencyRequestHashV1(envelope)
	if err != nil {
		return ctx, validation("cannot fingerprint idempotent %s request: %v", operation, err)
	}
	return store.WithIdempotencyRequestHash(ctx, hash), nil
}

func explicitRequestTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func mutationFingerprint(mutation missis.Mutation) (mutationFingerprintV1, error) {
	switch value := mutation.(type) {
	case missis.SetValue:
		return mutationFingerprintV1{Kind: "set-value", Value: value}, nil
	case missis.SetValueData:
		return mutationFingerprintV1{Kind: "set-value-data", Value: value}, nil
	case missis.AddValue:
		return mutationFingerprintV1{Kind: "add-value", Value: value}, nil
	case missis.RetractValue:
		return mutationFingerprintV1{Kind: "retract-value", Value: value}, nil
	case missis.RetractSubtree:
		return mutationFingerprintV1{Kind: "retract-subtree", Value: value}, nil
	case missis.RenamePart:
		return mutationFingerprintV1{Kind: "rename-part", Value: value}, nil
	case missis.MovePart:
		return mutationFingerprintV1{Kind: "move-part", Value: value}, nil
	case missis.SupersedeEvent:
		return mutationFingerprintV1{Kind: "supersede-event", Value: value}, nil
	default:
		return mutationFingerprintV1{}, fmt.Errorf("unsupported mutation %T", mutation)
	}
}

func ingestFingerprint(opts missis.IngestOptions, artifact string) ingestFingerprintV1 {
	operation := opts.Operation
	if operation == "" {
		operation = "attach-artifact"
	}
	mediaType := strings.TrimSpace(opts.MediaType)
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	capabilities := append([]string(nil), opts.Capabilities...)
	sort.Strings(capabilities)
	return ingestFingerprintV1{
		Operation:            operation,
		Target:               opts.Target,
		Path:                 append([]string(nil), opts.Path...),
		MediaType:            mediaType,
		SourceName:           opts.SourceName,
		LegacySource:         opts.LegacySource,
		ExcludeTopLevelTitle: opts.ExcludeTopLevelTitle,
		DeclaredSchema:       opts.DeclaredSchema,
		Capabilities:         capabilities,
		Artifact:             artifact,
	}
}
