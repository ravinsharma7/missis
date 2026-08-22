// Package plugin contains the capability and dispatch boundary for optional
// missis behavior. It deliberately does not know about concrete media,
// Markdown, or project plugins. Registrations describe what they handle;
// request metadata decides which registration is eligible.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ravinsharma7/missis/internal/model"
)

var (
	// ErrKnownFallback makes an unrendered-but-known value observable to the
	// caller. The returned Lines still contain only the original value; the
	// diagnostic is out-of-band and must not be injected into Markdown/data.
	ErrKnownFallback = errors.New("known value fallback")
	// ErrUnsupportedValueKind means the core has no safe representation for
	// the value kind. Callers must surface the error instead of guessing.
	ErrUnsupportedValueKind = errors.New("unsupported value kind")
)

// Manifest identifies the implementation that registered a capability.
// Hash is optional in the first in-process implementation, but is part of the
// selection/provenance contract for loadable plugins.
type Manifest struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	CodeHash   string `json:"code_hash,omitempty"`
	ConfigHash string `json:"config_hash,omitempty"`
}

// Selector describes the data contract a registration handles. Empty fields
// are wildcards. A registration with a more specific selector wins; equal
// matches are rejected rather than resolved by plugin name or registration
// order.
type Selector struct {
	ValueKind      model.ValueKind
	DeclaredSchema string
	PartPrefix     string
}

func (s Selector) matches(request Request) bool {
	if s.ValueKind != "" && s.ValueKind != request.ValueKind {
		return false
	}
	if s.DeclaredSchema != "" && s.DeclaredSchema != request.DeclaredSchema {
		return false
	}
	if s.PartPrefix != "" && request.PartPath != s.PartPrefix && !strings.HasPrefix(request.PartPath, s.PartPrefix+"/") {
		return false
	}
	return true
}

func (s Selector) specificity() int {
	score := 0
	if s.ValueKind != "" {
		score += 100
	}
	if s.DeclaredSchema != "" {
		score += 1000
	}
	if s.PartPrefix != "" {
		score += len(s.PartPrefix)
	}
	return score
}

// Request is the read-only input supplied to a renderer. Capabilities are
// granted by the caller; a renderer cannot acquire new capabilities.
type Request struct {
	PartPath       string
	ValueKind      model.ValueKind
	DeclaredSchema string
	Value          model.Value
	Capabilities   []string
	Width          int
}

// RenderState describes how a value was handled. A fallback is deliberately a
// state and an error signal, not a successful semantic rendering.
type RenderState string

const (
	RenderStateRendered      RenderState = "rendered"
	RenderStateKnownFallback RenderState = "known-fallback"
	RenderStateUnsupported   RenderState = "unsupported"
)

// RenderResult is renderer output. Lines are sanitized by Registry.Render at
// the boundary so a plugin cannot emit terminal control sequences accidentally
// or intentionally. KnownFallback lines contain only the original value; the
// Diagnostic is separate metadata.
type RenderResult struct {
	Lines    []string
	Renderer string
	State    RenderState
	// Diagnostic explains a fallback or unsupported result without becoming
	// part of the content itself.
	Diagnostic string
	Fallback   bool
	Derived    bool
}

// RendererFunc is intentionally a pure function over a request. It has no
// store handle and therefore cannot mutate authoritative state.
type RendererFunc func(Request) (RenderResult, error)

// RendererRegistration declares one renderer and its selection contract.
type RendererRegistration struct {
	Manifest             Manifest
	ID                   string
	Selector             Selector
	RequiredCapabilities []string
	Render               RendererFunc
}

// Registry is an immutable-after-composition registry in normal use. Register
// is kept explicit so the application composition root controls which plugins
// are available for a process.
type Registry struct {
	renderers []RendererRegistration
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(registration RendererRegistration) error {
	if registration.Manifest.ID == "" {
		return fmt.Errorf("plugin manifest ID is required")
	}
	if registration.Manifest.Version == "" {
		return fmt.Errorf("plugin %s version is required", registration.Manifest.ID)
	}
	if registration.ID == "" {
		return fmt.Errorf("plugin %s renderer ID is required", registration.Manifest.ID)
	}
	if registration.Render == nil {
		return fmt.Errorf("plugin %s renderer %s is nil", registration.Manifest.ID, registration.ID)
	}
	for _, existing := range r.renderers {
		if existing.Manifest.ID == registration.Manifest.ID && existing.ID == registration.ID {
			return fmt.Errorf("duplicate renderer registration: %s/%s", registration.Manifest.ID, registration.ID)
		}
	}
	r.renderers = append(r.renderers, registration)
	return nil
}

// Render selects a renderer by declared request metadata and capabilities.
// With no eligible renderer it permits only a known core value to fall back
// to its original representation, and returns ErrKnownFallback so callers
// cannot silently treat that as a successful specialized rendering. Unknown
// kinds return ErrUnsupportedValueKind. It never guesses behavior from URLs,
// filenames, or plugin names.
func (r *Registry) Render(request Request) (RenderResult, error) {
	registration, ok, err := r.resolve(request)
	if err != nil {
		return RenderResult{}, err
	}
	if !ok {
		result, fallbackErr := fallback(request)
		if fallbackErr != nil {
			return result, fallbackErr
		}
		return result, fmt.Errorf("%w: %s", ErrKnownFallback, result.Diagnostic)
	}
	result, err := registration.Render(request)
	if err != nil {
		return RenderResult{}, fmt.Errorf("renderer %s/%s: %w", registration.Manifest.ID, registration.ID, err)
	}
	result.Renderer = registration.Manifest.ID + "/" + registration.ID
	result.Lines = sanitizeLines(result.Lines)
	if result.State == "" && result.Fallback {
		result.State = RenderStateKnownFallback
	}
	if result.State == RenderStateKnownFallback {
		if result.Diagnostic == "" {
			result.Diagnostic = fmt.Sprintf("renderer %s/%s returned the original value", registration.Manifest.ID, registration.ID)
		}
		result.Fallback = true
		return result, fmt.Errorf("%w: %s", ErrKnownFallback, result.Diagnostic)
	}
	if result.State == RenderStateUnsupported {
		if result.Diagnostic == "" {
			result.Diagnostic = fmt.Sprintf("renderer %s/%s cannot represent the value", registration.Manifest.ID, registration.ID)
		}
		return result, fmt.Errorf("%w: %s", ErrUnsupportedValueKind, result.Diagnostic)
	}
	result.State = RenderStateRendered
	result.Fallback = false
	return result, nil
}

func (r *Registry) resolve(request Request) (RendererRegistration, bool, error) {
	type candidate struct {
		registration RendererRegistration
		score        int
	}
	candidates := make([]candidate, 0)
	for _, registration := range r.renderers {
		if !registration.Selector.matches(request) || !hasCapabilities(request.Capabilities, registration.RequiredCapabilities) {
			continue
		}
		candidates = append(candidates, candidate{
			registration: registration,
			score:        registration.Selector.specificity(),
		})
	}
	if len(candidates) == 0 {
		return RendererRegistration{}, false, nil
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
		return RendererRegistration{}, false, fmt.Errorf("ambiguous renderer selection for kind %q schema %q: %s", request.ValueKind, request.DeclaredSchema, strings.Join(matches, ", "))
	}
	return candidates[0].registration, true, nil
}

// Fallback returns a data-only result for callers that already know they are
// handling a fallback (for example, a renderer that cannot parse its own
// declared media descriptor). Registry.Render should normally be preferred,
// because it also returns ErrKnownFallback or ErrUnsupportedValueKind.
func Fallback(request Request) RenderResult {
	result, err := fallback(request)
	if err != nil {
		result.Diagnostic = err.Error()
	}
	return result
}

func fallback(request Request) (RenderResult, error) {
	if !knownCoreKind(request.ValueKind) {
		return RenderResult{
			State:      RenderStateUnsupported,
			Diagnostic: fmt.Sprintf("no safe core representation for value kind %q", request.ValueKind),
		}, fmt.Errorf("%w: %q", ErrUnsupportedValueKind, request.ValueKind)
	}
	value := request.Value
	if value.Data != nil {
		lines, err := jsonLines(value.Data)
		if err != nil {
			return RenderResult{State: RenderStateUnsupported, Diagnostic: "known value cannot be encoded as data"}, fmt.Errorf("%w: %v", ErrUnsupportedValueKind, err)
		}
		return knownFallback(lines, request.ValueKind)
	}
	if value.Ref != nil {
		lines, err := jsonLines(value.Ref)
		if err != nil {
			return RenderResult{State: RenderStateUnsupported, Diagnostic: "reference cannot be encoded as data"}, fmt.Errorf("%w: %v", ErrUnsupportedValueKind, err)
		}
		return knownFallback(lines, request.ValueKind)
	}
	if value.List != nil {
		lines, err := jsonLines(value.List)
		if err != nil {
			return RenderResult{State: RenderStateUnsupported, Diagnostic: "list cannot be encoded as data"}, fmt.Errorf("%w: %v", ErrUnsupportedValueKind, err)
		}
		return knownFallback(lines, request.ValueKind)
	}
	return knownFallback(safeTextLines(value.Text), request.ValueKind)
}

func knownFallback(lines []string, kind model.ValueKind) (RenderResult, error) {
	return RenderResult{
		Lines:      lines,
		State:      RenderStateKnownFallback,
		Diagnostic: fmt.Sprintf("no specialized renderer for %q; showing original value", kind),
		Fallback:   true,
	}, nil
}

func jsonLines(value any) ([]string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return safeTextLines(string(encoded)), nil
}

func knownCoreKind(kind model.ValueKind) bool {
	switch kind {
	case model.ValueKindText,
		model.ValueKindMarkdown,
		model.ValueKindScalar,
		model.ValueKindStatus,
		model.ValueKindPriority,
		model.ValueKindMap,
		model.ValueKindList,
		model.ValueKindRef,
		model.ValueKindCodeRef,
		model.ValueKindGitRef,
		model.ValueKindEvidence,
		model.ValueKindVerification,
		model.ValueKindJSON,
		model.ValueKindArtifact,
		model.ValueKindAnnotation,
		model.ValueKindImage,
		model.ValueKindVideo,
		model.ValueKindAudio,
		model.ValueKindEmbed:
		return true
	default:
		return false
	}
}

func safeTextLines(value string) []string {
	// Split before sanitizing so Markdown line breaks remain content. Each
	// emitted line is still terminal-safe; newlines never reach a terminal
	// output line as control input.
	rawLines := strings.Split(value, "\n")
	lines := make([]string, len(rawLines))
	for i, line := range rawLines {
		lines[i] = sanitizeText(line)
	}
	return lines
}

func sanitizeLines(lines []string) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = sanitizeText(line)
	}
	return result
}

// SanitizeText makes an error or diagnostic safe for a terminal boundary.
// It is exported so callers can show plugin errors without trusting their
// text as terminal control input.
func SanitizeText(value string) string {
	value = strings.ToValidUTF8(value, "�")
	return string(bytes.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return ' '
		}
		if r == 0x7f {
			return ' '
		}
		return r
	}, []byte(value)))
}

func sanitizeText(value string) string { return SanitizeText(value) }

func hasCapabilities(granted, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(granted))
	for _, capability := range granted {
		set[capability] = struct{}{}
	}
	for _, capability := range required {
		if _, ok := set[capability]; !ok {
			return false
		}
	}
	return true
}
