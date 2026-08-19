// Package schema implements the declaration core from
// specs/schema-declaration.subspec.md (rev 4) as a spike: the declaration
// grammar, subtree matching, scope resolution, enforcement, and deterministic
// version hashing.
//
// The spike is deliberately isolated from the service/CLI layer. Wiring it
// into writes, links, renderers, and scope-entity part paths is ticket #27.
//
// There is deliberately no inference fallback: value kinds are always
// explicit on writes (declared, or writer-supplied), never guessed from key
// names or content.
package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

// Scope identifies where a declaration lives. Smaller values are nearer to
// the ticket and win scope tie-breaks.
type Scope int

const (
	ScopeProject Scope = iota
	ScopeGroup
)

var scopeNames = map[Scope]string{
	ScopeProject: "project",
	ScopeGroup:   "group",
}

func (s Scope) String() string {
	if name, ok := scopeNames[s]; ok {
		return name
	}
	return fmt.Sprintf("scope(%d)", int(s))
}

// Declaration is one schema declaration: a part at
// schema/<key-prefix> (or schema/type/<ticket-type>/<key-prefix>) on a scope
// entity, whose value is the declared kind.
type Declaration struct {
	Scope         Scope
	ScopeID       string // canonical entity ID; "global" for built-ins
	Prefix        []string
	TypeQualified string // ticket type constraint; "" means unqualified
	Kind          KindSpec
	EventRef      string
	EffectiveAt   time.Time
	KnownAt       time.Time
}

// KindSpec is a parsed declaration value.
type KindSpec struct {
	Base     model.ValueKind
	Elements *KindSpec    // list[K]
	KeyKind  *KindSpec    // map[K:V]
	ValKind  *KindSpec    // map[K:V]
	Targets  []model.Kind // ref[T|U|...]
}

var baseKinds = map[string]model.ValueKind{
	"text":         model.ValueKindText,
	"markdown":     model.ValueKindMarkdown,
	"scalar":       model.ValueKindScalar,
	"status":       model.ValueKindStatus,
	"priority":     model.ValueKindPriority,
	"map":          model.ValueKindMap,
	"list":         model.ValueKindList,
	"ref":          model.ValueKindRef,
	"code-ref":     model.ValueKindCodeRef,
	"git-ref":      model.ValueKindGitRef,
	"evidence":     model.ValueKindEvidence,
	"verification": model.ValueKindVerification,
	"json":         model.ValueKindJSON,
	"artifact":     model.ValueKindArtifact,
	"annotation":   model.ValueKindAnnotation,
}

var targetKinds = map[string]model.Kind{
	"ticket":   model.KindTicket,
	"part":     model.KindPart,
	"project":  model.KindProject,
	"group":    model.KindGroup,
	"event":    model.KindEvent,
	"run":      model.KindRun,
	"code":     model.KindCode,
	"git":      model.KindGit,
	"artifact": model.KindArtifact,
}

// ParseKind parses the declaration-value grammar:
// a base kind, list[K], map[K:V], or ref[T|U|...].
// In v1, K and V inside composites are base kinds only (no nesting).
func ParseKind(text string) (KindSpec, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return KindSpec{}, errors.New("declaration kind must not be empty")
	}
	if base, ok := baseKinds[trimmed]; ok {
		return KindSpec{Base: base}, nil
	}
	if inner, ok := compositeInner(trimmed, "list["); ok {
		if inner == "" {
			return KindSpec{}, errors.New("list[K] requires an element kind")
		}
		elem, err := ParseKind(inner)
		if err != nil {
			return KindSpec{}, err
		}
		if !elem.isBase() {
			return KindSpec{}, errors.New("list[K] element kind must be a base kind in v1")
		}
		return KindSpec{Base: model.ValueKindList, Elements: &elem}, nil
	}
	if inner, ok := compositeInner(trimmed, "map["); ok {
		parts := strings.Split(inner, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return KindSpec{}, errors.New("map[K:V] requires exactly one key kind and one value kind")
		}
		keyKind, err := ParseKind(parts[0])
		if err != nil {
			return KindSpec{}, err
		}
		valKind, err := ParseKind(parts[1])
		if err != nil {
			return KindSpec{}, err
		}
		if !keyKind.isBase() || !valKind.isBase() {
			return KindSpec{}, errors.New("map[K:V] kinds must be base kinds in v1")
		}
		return KindSpec{Base: model.ValueKindMap, KeyKind: &keyKind, ValKind: &valKind}, nil
	}
	if inner, ok := compositeInner(trimmed, "ref["); ok {
		parts := strings.Split(inner, "|")
		if len(parts) == 0 || parts[0] == "" {
			return KindSpec{}, errors.New("ref[...] requires at least one target kind")
		}
		targets := make([]model.Kind, 0, len(parts))
		for _, part := range parts {
			kind, ok := targetKinds[strings.TrimSpace(part)]
			if !ok {
				return KindSpec{}, fmt.Errorf("unknown ref target kind: %s", part)
			}
			targets = append(targets, kind)
		}
		return KindSpec{Base: model.ValueKindRef, Targets: targets}, nil
	}
	return KindSpec{}, fmt.Errorf("unknown declaration kind: %s", trimmed)
}

// ValidBaseKind reports whether text names one of the base value kinds that
// a writer may supply explicitly (composites are declaration-only).
func ValidBaseKind(kind model.ValueKind) bool {
	_, ok := baseKinds[string(kind)]
	return ok
}

func compositeInner(text, prefix string) (string, bool) {
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, "]") {
		return "", false
	}
	inner := text[len(prefix) : len(text)-1]
	if strings.Contains(inner, "[") || strings.Contains(inner, "]") {
		return "", false
	}
	return inner, true
}

func (k KindSpec) isBase() bool {
	return k.Elements == nil && k.KeyKind == nil && k.Targets == nil
}

// String returns the canonical declaration-value text.
func (k KindSpec) String() string {
	switch {
	case k.Elements != nil:
		return "list[" + k.Elements.String() + "]"
	case k.KeyKind != nil:
		return "map[" + k.KeyKind.String() + ":" + k.ValKind.String() + "]"
	case k.Targets != nil:
		parts := make([]string, 0, len(k.Targets))
		for _, t := range k.Targets {
			parts = append(parts, string(t))
		}
		return "ref[" + strings.Join(parts, "|") + "]"
	default:
		return string(k.Base)
	}
}

// StoredKind returns the ValueKind that an accepted write under this
// declaration stores: composites store their base kind (list/map/ref).
func (k KindSpec) StoredKind() model.ValueKind {
	return k.Base
}

// ParseDeclarationPath interprets the segments after the "schema" root:
// schema/<key-prefix> or schema/type/<ticket-type>/<key-prefix>.
// The first segment "type" is reserved and starts a type-qualified
// declaration. All segments must satisfy the part-path grammar (spec 7.4);
// wildcard characters are invalid.
func ParseDeclarationPath(segments []string) (prefix []string, typeQualified string, err error) {
	if len(segments) == 0 {
		return nil, "", errors.New("declaration path must not be empty")
	}
	if segments[0] == "type" {
		if len(segments) < 3 {
			return nil, "", errors.New("schema/type requires a ticket type and a key prefix")
		}
		typeQualified = segments[1]
		prefix = segments[2:]
	} else {
		prefix = segments
	}
	if err := model.ValidatePathSegments(prefix); err != nil {
		return nil, "", err
	}
	return prefix, typeQualified, nil
}

// TicketContext identifies the governing scopes for one ticket.
// Groups must be canonical IDs; the resolver orders them deterministically
// regardless of caller order.
type TicketContext struct {
	Types     []string
	ProjectID string
	Groups    []string
}

// ValueShape describes a proposed value at the kind level (a spike
// simplification of model.Value; the service maps values to shapes).
type ValueShape struct {
	Kind        model.ValueKind
	ElementKind model.ValueKind // list element kind
	MapKeyKind  model.ValueKind // map key kind
	MapValKind  model.ValueKind // map value kind
	RefKind     model.Kind      // reference target kind
}

// ResolvedKind is the consumer contract tuple.
type ResolvedKind struct {
	Declared *KindSpec
	Stored   model.ValueKind
	Matched  *Declaration
}

// Rejection is a deterministic enforcement outcome.
type Rejection struct {
	Reason      string
	Scope       string
	Pattern     string
	VersionHash string
	Expected    string
	Proposed    string
}

func (r *Rejection) Error() string { return r.Reason }

// Resolver performs matching, resolution, and enforcement over a fixed set of
// declarations. It is stateless and safe for concurrent use.
type Resolver struct {
	decls []Declaration
}

func NewResolver(decls []Declaration) *Resolver {
	return &Resolver{decls: decls}
}

// applicable returns the declarations that govern the ticket at any time.
// It is insertion-order independent.
func (r *Resolver) applicable(t TicketContext) []Declaration {
	out := make([]Declaration, 0, len(r.decls)+2)
	for _, d := range r.decls {
		switch d.Scope {
		case ScopeProject:
			if d.ScopeID != t.ProjectID {
				continue
			}
		case ScopeGroup:
			if !containsString(t.Groups, d.ScopeID) {
				continue
			}
		}
		if d.TypeQualified != "" && !containsString(t.Types, d.TypeQualified) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// active filters declarations to those effective and known by at.
func active(decls []Declaration, at time.Time) []Declaration {
	out := make([]Declaration, 0, len(decls))
	for _, d := range decls {
		if d.EffectiveAt.After(at) || d.KnownAt.After(at) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// Resolve returns the kind contract for a ticket part key.
func (r *Resolver) Resolve(t TicketContext, key []string, stored model.ValueKind, at time.Time) ResolvedKind {
	if d := r.bestMatch(active(r.applicable(t), at), key); d != nil {
		kind := d.Kind
		return ResolvedKind{Declared: &kind, Stored: stored, Matched: d}
	}
	return ResolvedKind{Stored: stored}
}

// ValidateWrite checks a proposed part value against the effective schema.
// A nil rejection means the write is allowed (including open-world keys).
func (r *Resolver) ValidateWrite(t TicketContext, key []string, shape ValueShape, at time.Time) *Rejection {
	cands := active(r.applicable(t), at)
	d := r.bestMatch(cands, key)
	if d == nil {
		return nil
	}
	if kindMatches(d.Kind, shape) {
		return nil
	}
	return r.reject(t, *d, key, shape, at)
}

// ValidateLink checks a typed link's target kind against
// schema/links/<relation> declarations. A nil rejection means the link is
// allowed.
func (r *Resolver) ValidateLink(t TicketContext, relation string, targetKind model.Kind, at time.Time) *Rejection {
	key := []string{"links", relation}
	cands := active(r.applicable(t), at)
	d := r.bestMatch(cands, key)
	if d == nil {
		return nil
	}
	if d.Kind.Targets == nil {
		return nil
	}
	shape := ValueShape{Kind: model.ValueKindRef, RefKind: targetKind}
	if kindMatches(d.Kind, shape) {
		return nil
	}
	return r.reject(t, *d, key, shape, at)
}

// VersionHash is a deterministic content hash over the declarations active
// for the ticket at at. It is independent of declaration insertion order.
func (r *Resolver) VersionHash(t TicketContext, at time.Time) string {
	decls := active(r.applicable(t), at)
	sort.Slice(decls, func(i, j int) bool { return declLess(decls[i], decls[j]) })
	h := sha256.New()
	for _, d := range decls {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00",
			d.Scope, d.ScopeID, strings.Join(d.Prefix, "/"), d.TypeQualified,
			d.Kind.String(), d.EventRef,
			d.EffectiveAt.UTC().Format(time.RFC3339Nano), d.KnownAt.UTC().Format(time.RFC3339Nano))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (r *Resolver) bestMatch(cands []Declaration, key []string) *Declaration {
	var best *Declaration
	for i := range cands {
		d := &cands[i]
		if !prefixMatches(d.Prefix, key) {
			continue
		}
		if best == nil || declBetter(*d, *best) {
			best = d
		}
	}
	return best
}

func (r *Resolver) reject(t TicketContext, d Declaration, key []string, shape ValueShape, at time.Time) *Rejection {
	pattern := "schema/" + strings.Join(d.Prefix, "/")
	scope := fmt.Sprintf("%s:%s", d.Scope, d.ScopeID)
	expected := d.Kind.String()
	proposed := string(shape.Kind)
	if d.Kind.Elements != nil && shape.ElementKind != "" {
		proposed = fmt.Sprintf("list[%s]", shape.ElementKind)
	}
	if d.Kind.Targets != nil && shape.RefKind != "" {
		proposed = fmt.Sprintf("ref[%s]", shape.RefKind)
	}
	reason := fmt.Sprintf(
		"schema violation: %s (%s) declares %s but proposed value is %s; matched declaration %s (event %s), effective schema version %s",
		pattern, scope, expected, proposed, pattern, d.EventRef, r.VersionHash(t, at))
	return &Rejection{
		Reason:      reason,
		Scope:       scope,
		Pattern:     pattern,
		VersionHash: r.VersionHash(t, at),
		Expected:    expected,
		Proposed:    proposed,
	}
}

func kindMatches(spec KindSpec, shape ValueShape) bool {
	switch {
	case spec.Elements != nil:
		// Element-kind checks apply only when the caller tracks element
		// kinds; an empty ElementKind means "list level only" (service v1).
		return shape.Kind == model.ValueKindList &&
			(shape.ElementKind == "" || shape.ElementKind == spec.Elements.Base)
	case spec.KeyKind != nil:
		return shape.Kind == model.ValueKindMap &&
			(shape.MapKeyKind == "" || shape.MapKeyKind == spec.KeyKind.Base) &&
			(shape.MapValKind == "" || shape.MapValKind == spec.ValKind.Base)
	case spec.Targets != nil:
		return shape.Kind == model.ValueKindRef && containsKind(spec.Targets, shape.RefKind)
	default:
		return shape.Kind == spec.Base
	}
}

// declBetter is the deterministic selection order:
// longer prefix, type-qualified over unqualified, nearer scope, bitemporal
// winner, then canonical declaration order.
func declBetter(a, b Declaration) bool {
	if len(a.Prefix) != len(b.Prefix) {
		return len(a.Prefix) > len(b.Prefix)
	}
	if (a.TypeQualified == "") != (b.TypeQualified == "") {
		return a.TypeQualified != ""
	}
	if a.Scope != b.Scope {
		return a.Scope < b.Scope
	}
	if !a.EffectiveAt.Equal(b.EffectiveAt) {
		return a.EffectiveAt.After(b.EffectiveAt)
	}
	if !a.KnownAt.Equal(b.KnownAt) {
		return a.KnownAt.After(b.KnownAt)
	}
	return declLess(a, b)
}

func declLess(a, b Declaration) bool {
	if a.Scope != b.Scope {
		return a.Scope < b.Scope
	}
	if a.ScopeID != b.ScopeID {
		return a.ScopeID < b.ScopeID
	}
	if got, want := strings.Join(a.Prefix, "/"), strings.Join(b.Prefix, "/"); got != want {
		return got < want
	}
	if a.TypeQualified != b.TypeQualified {
		return a.TypeQualified < b.TypeQualified
	}
	if a.Kind.String() != b.Kind.String() {
		return a.Kind.String() < b.Kind.String()
	}
	return a.EventRef < b.EventRef
}

func prefixMatches(prefix, key []string) bool {
	if len(prefix) > len(key) {
		return false
	}
	for i := range prefix {
		if prefix[i] != key[i] {
			return false
		}
	}
	return true
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func containsKind(list []model.Kind, want model.Kind) bool {
	for _, k := range list {
		if k == want {
			return true
		}
	}
	return false
}
