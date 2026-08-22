package plugin

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ravinsharma7/missis/internal/model"
)

// ProcessorInput is a bounded, read-only processor input. There is no store
// handle by design: a processor can only inspect the data explicitly selected
// by the caller.
type ProcessorInput struct {
	Invocation     model.InvocationRef
	Target         model.Ref
	PartPath       string
	DeclaredSchema string
	Value          model.Value
	InputEvents    []model.EventID
	Sources        []model.Ref
	Capabilities   []string
}

// ProcessorOutput contains proposals. The application layer validates and
// appends accepted events; this package never persists them.
type ProcessorOutput struct {
	Events      []model.Event
	Derived     []DerivedValue
	Diagnostics []Diagnostic
}

type DerivedValue struct {
	Target      model.Ref
	Value       model.Value
	Sources     []model.Ref
	InputEvents []model.EventID
}

type Diagnostic struct {
	Severity string
	Message  string
}

type ProcessorFunc func(ProcessorInput) (ProcessorOutput, error)

// RunProcessor validates the provenance boundary around a processor result.
// A processor result without its invocation attached cannot enter the event
// ledger as processor output.
func RunProcessor(processor ProcessorFunc, input ProcessorInput) (ProcessorOutput, error) {
	if processor == nil {
		return ProcessorOutput{}, fmt.Errorf("processor is nil")
	}
	if input.Invocation.ID == "" {
		return ProcessorOutput{}, fmt.Errorf("processor invocation ID is required")
	}
	output, err := processor(input)
	if err != nil {
		return ProcessorOutput{}, err
	}
	for i := range output.Events {
		event := &output.Events[i]
		if event.Invocation == nil {
			event.Invocation = &model.InvocationRef{ID: input.Invocation.ID}
		}
		if event.Invocation.ID != input.Invocation.ID {
			return ProcessorOutput{}, fmt.Errorf("processor event %s has invocation %q, want %q", event.ID, event.Invocation.ID, input.Invocation.ID)
		}
	}
	return output, nil
}

// ProcessorRegistration declares which processor is eligible for a bounded
// input. Selection uses the same metadata contract as renderers: selectors and
// capabilities decide; registration order and plugin names do not.
type ProcessorRegistration struct {
	Manifest             Manifest
	ID                   string
	Selector             Selector
	RequiredCapabilities []string
	Process              ProcessorFunc
}

type ProcessorRegistry struct {
	processors []ProcessorRegistration
}

func NewProcessorRegistry() *ProcessorRegistry {
	return &ProcessorRegistry{}
}

func (r *ProcessorRegistry) Register(registration ProcessorRegistration) error {
	if registration.Manifest.ID == "" {
		return fmt.Errorf("plugin manifest ID is required")
	}
	if registration.Manifest.Version == "" {
		return fmt.Errorf("plugin %s version is required", registration.Manifest.ID)
	}
	if registration.ID == "" {
		return fmt.Errorf("plugin %s processor ID is required", registration.Manifest.ID)
	}
	if registration.Process == nil {
		return fmt.Errorf("plugin %s processor %s is nil", registration.Manifest.ID, registration.ID)
	}
	for _, existing := range r.processors {
		if existing.Manifest.ID == registration.Manifest.ID && existing.ID == registration.ID {
			return fmt.Errorf("duplicate processor registration: %s/%s", registration.Manifest.ID, registration.ID)
		}
	}
	r.processors = append(r.processors, registration)
	return nil
}

// Run selects and invokes one processor. No eligible processor is a normal
// result, allowing callers to treat processors as optional extensions.
func (r *ProcessorRegistry) Run(input ProcessorInput) (ProcessorOutput, string, error) {
	request := Request{
		PartPath:       input.PartPath,
		ValueKind:      input.Value.Kind,
		DeclaredSchema: input.DeclaredSchema,
		Value:          input.Value,
		Capabilities:   input.Capabilities,
	}
	registration, ok, err := r.resolve(request)
	if err != nil {
		return ProcessorOutput{}, "", err
	}
	if !ok {
		return ProcessorOutput{}, "", nil
	}
	output, err := RunProcessor(registration.Process, input)
	if err != nil {
		return ProcessorOutput{}, "", fmt.Errorf("processor %s/%s: %w", registration.Manifest.ID, registration.ID, err)
	}
	return output, registration.Manifest.ID + "/" + registration.ID, nil
}

func (r *ProcessorRegistry) resolve(request Request) (ProcessorRegistration, bool, error) {
	type candidate struct {
		registration ProcessorRegistration
		score        int
	}
	candidates := make([]candidate, 0)
	for _, registration := range r.processors {
		if !registration.Selector.matches(request) || !hasCapabilities(request.Capabilities, registration.RequiredCapabilities) {
			continue
		}
		candidates = append(candidates, candidate{
			registration: registration,
			score:        registration.Selector.specificity(),
		})
	}
	if len(candidates) == 0 {
		return ProcessorRegistration{}, false, nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > 1 && candidates[0].score == candidates[1].score {
		matches := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.score != candidates[0].score {
				break
			}
			matches = append(matches, candidate.registration.Manifest.ID+"/"+candidate.registration.ID)
		}
		sort.Strings(matches)
		return ProcessorRegistration{}, false, fmt.Errorf("ambiguous processor selection for kind %q schema %q: %s", request.ValueKind, request.DeclaredSchema, strings.Join(matches, ", "))
	}
	return candidates[0].registration, true, nil
}
