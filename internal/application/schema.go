package application

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/schema"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// schemaDeclarations loads all schema declarations from project/group entity
// streams valid at (effectiveAt, knownAt). Malformed declarations fail loudly
// with the violating event ref; they are never silently skipped.
func (s *Service) schemaDeclarations(ctx context.Context, effectiveAt, knownAt time.Time) ([]schema.Declaration, error) {
	events, err := s.LoadEvents(ctx)
	if err != nil {
		return nil, keepStorage(err)
	}
	streams := make(map[string]model.Ref)
	for _, event := range events {
		if event.Stream.Kind == model.KindProject || event.Stream.Kind == model.KindGroup {
			streams[string(event.Stream.Kind)+":"+event.Stream.Entity] = event.Stream
		}
	}
	var decls []schema.Declaration
	for _, stream := range streams {
		streamEvents, err := s.LoadStreamEvents(ctx, stream)
		if err != nil {
			return nil, keepStorage(err)
		}
		proj, err := model.ProjectStream(streamEvents, stream, effectiveAt, knownAt)
		if err != nil {
			return nil, keepStorage(err)
		}
		for path, partID := range proj.Paths {
			segments := strings.Split(path, "/")
			if len(segments) == 0 || segments[0] != "schema" {
				continue
			}
			part := proj.Parts[partID]
			if part == nil || part.Value == nil || strings.TrimSpace(part.Value.Text) == "" {
				continue
			}
			prefix, typeQualified, err := schema.ParseDeclarationPath(segments[1:])
			if err != nil {
				return nil, fmt.Errorf("malformed declaration %s on %s (part %s): %w", path, streamText(stream), partID, err)
			}
			kind, err := schema.ParseKind(part.Value.Text)
			if err != nil {
				return nil, fmt.Errorf("malformed declaration kind %q on %s (%s): %w", part.Value.Text, streamText(stream), path, err)
			}
			scope := schema.ScopeProject
			if stream.Kind == model.KindGroup {
				scope = schema.ScopeGroup
			}
			eventRef, eff, known := declarationProvenance(streamEvents, part.CurrentFrom)
			decls = append(decls, schema.Declaration{
				Scope:         scope,
				ScopeID:       stream.Entity,
				Prefix:        prefix,
				TypeQualified: typeQualified,
				Kind:          kind,
				EventRef:      eventRef,
				EffectiveAt:   eff,
				KnownAt:       known,
			})
		}
	}
	return decls, nil
}

func declarationProvenance(events []model.Event, currentFrom model.EventID) (string, time.Time, time.Time) {
	for _, event := range events {
		if event.ID == currentFrom {
			return "@e" + strconv.FormatUint(event.AliasSeq, 10), event.EffectiveAt, event.RecordedAt
		}
	}
	return string(currentFrom), time.Time{}, time.Time{}
}

func streamText(stream model.Ref) string {
	return string(stream.Kind) + ":" + stream.Entity
}

// schemaContext derives the effective scope chain for a ticket: its types,
// its home project, and the groups governing that project (canonical order).
func (s *Service) schemaContext(ctx context.Context, ticketID model.TicketID, effectiveAt, knownAt time.Time) (schema.TicketContext, error) {
	var tc schema.TicketContext
	proj, err := s.BitemporalProjection(ctx, ticketID, effectiveAt, knownAt)
	if err != nil {
		return tc, keepStorage(err)
	}
	if id, ok := proj.Paths["type"]; ok {
		if p := proj.Parts[id]; p != nil && p.Value != nil {
			tc.Types = append(tc.Types, p.Value.List...)
		}
	}
	linkEvents, err := s.LoadLinkEvents(ctx)
	if err != nil {
		return tc, keepStorage(err)
	}
	ticketRef := model.Ref{Kind: model.KindTicket, Entity: string(ticketID)}
	links, err := model.LinksForRef(linkEvents, ticketRef, effectiveAt, knownAt)
	if err != nil {
		return tc, keepStorage(err)
	}
	for _, link := range links {
		if link.Direction == "derived-inverse" && link.Relation == "contained-by" && link.To.Kind == model.KindProject {
			tc.ProjectID = link.To.Entity
		}
	}
	if tc.ProjectID != "" {
		projectRef := model.Ref{Kind: model.KindProject, Entity: tc.ProjectID}
		projectLinks, err := model.LinksForRef(linkEvents, projectRef, effectiveAt, knownAt)
		if err != nil {
			return tc, keepStorage(err)
		}
		for _, link := range projectLinks {
			if link.Direction == "derived-inverse" &&
				(link.Relation == "contained-by" || link.Relation == "governed-by") &&
				link.To.Kind == model.KindGroup {
				tc.Groups = append(tc.Groups, link.To.Entity)
			}
		}
	}
	sort.Strings(tc.Groups)
	return tc, nil
}

// schemaResolver builds a resolver from the declarations valid at the given
// times.
func (s *Service) schemaResolver(ctx context.Context, effectiveAt, knownAt time.Time) (*schema.Resolver, error) {
	decls, err := s.schemaDeclarations(ctx, effectiveAt, knownAt)
	if err != nil {
		return nil, err
	}
	return schema.NewResolver(decls), nil
}

// resolveWriteKind determines the value kind for a proposed write. A matched
// declaration wins; otherwise the caller-supplied explicit kind is required.
// There is no inference and no fallback.
func (s *Service) resolveWriteKind(ctx context.Context, stream model.Ref, path []string, explicit model.ValueKind, value model.Value, elements []string, effectiveAt, knownAt time.Time) (model.ValueKind, error) {
	if stream.Kind != model.KindTicket {
		if explicit == "" {
			return "", validation("value kind required (no inference); pass --kind or declare the key")
		}
		return explicit, nil
	}
	decls, err := s.schemaDeclarations(ctx, effectiveAt, knownAt)
	if err != nil {
		return "", err
	}
	if explicit != "" && !schema.ValidBaseKind(explicit) {
		return "", validation("unknown value kind: %s", explicit)
	}
	tc, err := s.schemaContext(ctx, model.TicketID(stream.Entity), effectiveAt, knownAt)
	if err != nil {
		return "", err
	}
	resolver := schema.NewResolver(decls)
	resolved := resolver.Resolve(tc, path, explicit, effectiveAt)
	if resolved.Declared != nil {
		if explicit == model.ValueKindList && resolved.Declared.StoredKind() != model.ValueKindList {
			return "", validation("declared kind %s does not allow list additions", resolved.Declared.String())
		}
		if resolved.Declared.Elements != nil {
			if len(elements) == 0 {
				return "", validation("declared kind %s requires element-level writes (use --add)", resolved.Declared.String())
			}
			if err := s.validateListElements(ctx, resolved.Declared.Elements, elements, effectiveAt, resolved.Declared.String()); err != nil {
				return "", err
			}
		}
		proposedKind := explicit
		if proposedKind == "" {
			proposedKind = resolved.Declared.StoredKind()
		}
		shape := shapeForValue(model.Value{Kind: proposedKind, Ref: value.Ref, List: value.List})
		if rej := resolver.ValidateWrite(tc, path, shape, effectiveAt); rej != nil {
			return "", validation("%s", rej.Reason)
		}
		return resolved.Declared.StoredKind(), nil
	}
	if explicit == "" {
		return "", validation("value kind required (no inference); pass --kind or declare the key")
	}
	shape := shapeForValue(value)
	if rej := resolver.ValidateWrite(tc, path, shape, effectiveAt); rej != nil {
		return "", validation("%s", rej.Reason)
	}
	return explicit, nil
}

// validateListElements enforces the declared element kind for list writes.
// For list[ref], every element must resolve to a known reference. Other
// element kinds are shape-only until their value grammars are defined.
func (s *Service) validateListElements(ctx context.Context, elem *schema.KindSpec, elements []string, effectiveAt time.Time, declared string) error {
	if elem == nil || elem.Base != model.ValueKindRef {
		return nil
	}
	for i, element := range elements {
		el := strings.TrimSpace(element)
		if el == "" {
			continue
		}
		ref, err := s.resolveAnyRef(ctx, el, effectiveAt)
		if err != nil {
			return validation("declared kind %s: element %d %q is not a resolvable ref", declared, i+1, el)
		}
		if len(elem.Targets) > 0 && !containsKind(elem.Targets, ref.Kind) {
			return validation("declared kind %s: element %d %q resolves to %s, not an allowed target", declared, i+1, el, ref.Kind)
		}
	}
	return nil
}

func containsKind(list []model.Kind, want model.Kind) bool {
	for _, k := range list {
		if k == want {
			return true
		}
	}
	return false
}

// validateLinkSchema enforces schema/links/<relation> endpoint legality for
// links asserted from a ticket stream.
func (s *Service) validateLinkSchema(ctx context.Context, stream model.Ref, targetKind model.Kind, relation string, effectiveAt, knownAt time.Time) error {
	if stream.Kind != model.KindTicket {
		return nil
	}
	decls, err := s.schemaDeclarations(ctx, effectiveAt, knownAt)
	if err != nil {
		return err
	}
	tc, err := s.schemaContext(ctx, model.TicketID(stream.Entity), effectiveAt, knownAt)
	if err != nil {
		return err
	}
	if rej := schema.NewResolver(decls).ValidateLink(tc, relation, targetKind, effectiveAt); rej != nil {
		return validation("%s", rej.Reason)
	}
	return nil
}

func shapeForValue(v model.Value) schema.ValueShape {
	shape := schema.ValueShape{Kind: v.Kind}
	if v.Ref != nil {
		shape.RefKind = v.Ref.Kind
	}
	if v.Kind == model.ValueKindMap {
		shape.MapKeyKind = model.ValueKindText
		shape.MapValKind = model.ValueKindText
	}
	return shape
}

// validateImportEvents enforces the effective schema for every value-carrying
// event in a batch. All violations are collected; the caller must not append
// the batch when any are returned (all-or-nothing).
func (s *Service) validateImportEvents(ctx context.Context, stream model.Ref, events []model.Event, effectiveAt, knownAt time.Time) error {
	if stream.Kind != model.KindTicket || len(events) == 0 {
		return nil
	}
	decls, err := s.schemaDeclarations(ctx, effectiveAt, knownAt)
	if err != nil {
		return err
	}
	tc, err := s.schemaContext(ctx, model.TicketID(stream.Entity), effectiveAt, knownAt)
	if err != nil {
		return err
	}
	resolver := schema.NewResolver(decls)
	var violations []string
	for _, event := range events {
		if event.Target.Kind != model.KindPart || event.Value.Kind == "" {
			continue
		}
		if rej := resolver.ValidateWrite(tc, event.Target.Path, shapeForValue(event.Value), event.EffectiveAt); rej != nil {
			violations = append(violations, rej.Reason)
		}
	}
	if len(violations) > 0 {
		return validation("import rejected (%d violation(s)): %s", len(violations), strings.Join(violations, "; "))
	}
	return nil
}

// decorateParts resolves the renderer contract for each part: declared kind
// wins over stored kind, and the matched declaration pattern is exposed.
func (s *Service) decorateParts(ctx context.Context, stream model.Ref, parts map[string]missis.PartView, effectiveAt, knownAt time.Time) error {
	if stream.Kind != model.KindTicket {
		return nil
	}
	decls, err := s.schemaDeclarations(ctx, effectiveAt, knownAt)
	if err != nil {
		return err
	}
	tc, err := s.schemaContext(ctx, model.TicketID(stream.Entity), effectiveAt, knownAt)
	if err != nil {
		return err
	}
	resolver := schema.NewResolver(decls)
	for path, part := range parts {
		key := strings.Split(path, "/")
		resolved := resolver.Resolve(tc, key, model.ValueKind(part.ValueKind), effectiveAt)
		if resolved.Declared == nil || resolved.Matched == nil {
			continue
		}
		part.ValueKind = string(resolved.Declared.StoredKind())
		part.DeclaredSchema = "schema/" + strings.Join(resolved.Matched.Prefix, "/") +
			" (" + resolved.Matched.Scope.String() + ":" + resolved.Matched.ScopeID + ")"
		parts[path] = part
	}
	return nil
}
