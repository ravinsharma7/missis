package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/model"
)

var (
	ErrNoIngestionPlugin  = errors.New("no ingestion plugin matched")
	ErrAmbiguousIngestion = errors.New("ambiguous ingestion plugin selection")
)

// IngestRequest describes the user-selected operation. Content is owned by
// the application boundary and is consumed once by the artifact store.
type IngestRequest struct {
	Operation            string
	Target               model.Ref
	Path                 []string
	MediaType            string
	SourceName           string
	LegacySource         string
	ExcludeTopLevelTitle bool
	DeclaredSchema       string
	Capabilities         []string
	Content              io.Reader
}

// IngestInput is the intentionally narrow input visible to a plugin. The
// plugin can reread only the artifact selected for this invocation; it cannot
// acquire a database, filesystem, or general artifact-store handle.
type IngestInput struct {
	Request     IngestRequest
	Artifact    artifact.Metadata
	Open        func(context.Context) (io.ReadCloser, error)
	Parent      *model.Ref
	Invocation  model.InvocationRef
	Actor       model.ActorRef
	RequestedBy model.ActorRef
	RecordedAt  time.Time
	EffectiveAt time.Time
	BatchID     model.BatchID
}

// IngestProposal is untrusted plugin output. The application validates all
// events and values before passing them to the store.
type IngestProposal struct {
	Events      []model.Event
	Result      any
	Diagnostics []Diagnostic
}

type IngestionPlugin interface {
	Propose(context.Context, IngestInput) (IngestProposal, error)
}

type IngestSelector struct {
	Operation      string
	MediaType      string
	TargetKind     model.Kind
	DeclaredSchema string
}

func (s IngestSelector) matches(request IngestRequest) bool {
	if s.Operation != "" && s.Operation != request.Operation {
		return false
	}
	if s.MediaType != "" && s.MediaType != request.MediaType {
		return false
	}
	if s.TargetKind != "" && s.TargetKind != request.Target.Kind {
		return false
	}
	if s.DeclaredSchema != "" && s.DeclaredSchema != request.DeclaredSchema {
		return false
	}
	return true
}

func (s IngestSelector) specificity() int {
	score := 0
	if s.Operation != "" {
		score += 100
	}
	if s.MediaType != "" {
		score += 1000
	}
	if s.TargetKind != "" {
		score += 10
	}
	if s.DeclaredSchema != "" {
		score += 10000
	}
	return score
}

type IngestionRegistration struct {
	Manifest             Manifest
	ID                   string
	Selector             IngestSelector
	RequiredCapabilities []string
	Plugin               IngestionPlugin
}

type IngestionRegistry struct {
	plugins []IngestionRegistration
}

func NewIngestionRegistry() *IngestionRegistry {
	return &IngestionRegistry{}
}

func (r *IngestionRegistry) Register(registration IngestionRegistration) error {
	if registration.Manifest.ID == "" || registration.Manifest.Version == "" || registration.Manifest.CodeHash == "" {
		return fmt.Errorf("ingestion plugin manifest ID, version, and code hash are required")
	}
	if registration.ID == "" {
		return fmt.Errorf("ingestion plugin %s ID is required", registration.Manifest.ID)
	}
	if registration.Plugin == nil {
		return fmt.Errorf("ingestion plugin %s/%s is nil", registration.Manifest.ID, registration.ID)
	}
	for _, existing := range r.plugins {
		if existing.Manifest.ID == registration.Manifest.ID && existing.ID == registration.ID {
			return fmt.Errorf("duplicate ingestion plugin: %s/%s", registration.Manifest.ID, registration.ID)
		}
	}
	r.plugins = append(r.plugins, registration)
	return nil
}

func (r *IngestionRegistry) Run(ctx context.Context, input IngestInput) (IngestProposal, string, error) {
	registration, err := r.resolve(input.Request)
	if err != nil {
		return IngestProposal{}, "", err
	}
	if input.Invocation.ID == "" {
		return IngestProposal{}, "", fmt.Errorf("ingestion invocation ID is required")
	}
	input.Invocation.Plugin = registration.Manifest.ID
	input.Invocation.Version = registration.Manifest.Version
	input.Invocation.CodeHash = registration.Manifest.CodeHash
	input.Invocation.RequestedBy = &input.RequestedBy
	proposal, err := registration.Plugin.Propose(ctx, input)
	if err != nil {
		return IngestProposal{}, "", fmt.Errorf("ingestion plugin %s/%s: %w", registration.Manifest.ID, registration.ID, err)
	}
	for i := range proposal.Events {
		event := &proposal.Events[i]
		if event.Invocation == nil {
			event.Invocation = &input.Invocation
		}
		if event.Invocation.ID != input.Invocation.ID {
			return IngestProposal{}, "", fmt.Errorf("plugin event %s has invocation %q, want %q", event.ID, event.Invocation.ID, input.Invocation.ID)
		}
	}
	return proposal, registration.Manifest.ID + "/" + registration.ID, nil
}

func (r *IngestionRegistry) resolve(request IngestRequest) (IngestionRegistration, error) {
	type candidate struct {
		registration IngestionRegistration
		score        int
	}
	var candidates []candidate
	for _, registration := range r.plugins {
		if !registration.Selector.matches(request) || !hasCapabilities(request.Capabilities, registration.RequiredCapabilities) {
			continue
		}
		candidates = append(candidates, candidate{registration: registration, score: registration.Selector.specificity()})
	}
	if len(candidates) == 0 {
		return IngestionRegistration{}, fmt.Errorf("%w: operation=%q media_type=%q target=%s", ErrNoIngestionPlugin, request.Operation, request.MediaType, request.Target.Kind)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if len(candidates) > 1 && candidates[0].score == candidates[1].score {
		matches := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.score != candidates[0].score {
				break
			}
			matches = append(matches, candidate.registration.Manifest.ID+"/"+candidate.registration.ID)
		}
		sort.Strings(matches)
		return IngestionRegistration{}, fmt.Errorf("%w: %s", ErrAmbiguousIngestion, strings.Join(matches, ", "))
	}
	return candidates[0].registration, nil
}
