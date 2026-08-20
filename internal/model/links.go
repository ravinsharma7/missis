package model

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var relationInverses = map[string]string{
	"blocks":          "blocked-by",
	"blocked-by":      "blocks",
	"caused-by":       "causes",
	"causes":          "caused-by",
	"duplicates":      "duplicated-by",
	"duplicated-by":   "duplicates",
	"supports":        "supported-by",
	"supported-by":    "supports",
	"contradicts":     "contradicted-by",
	"contradicted-by": "contradicts",
	"implements":      "implemented-by",
	"implemented-by":  "implements",
	"tracks":          "tracked-by",
	"tracked-by":      "tracks",
	"documents":       "documented-by",
	"documented-by":   "documents",
	"contains":        "contained-by",
	"contained-by":    "contains",
	"governs":         "governed-by",
	"governed-by":     "governs",
	"has-home":        "home-of",
	"home-of":         "has-home",
	"member-of":       "has-member",
	"has-member":      "member-of",
	"related":         "related",
}

type LinkView struct {
	From       Ref
	Relation   string
	To         Ref
	Direction  string
	Origin     string
	CreatedBy  EventID
	Assertions []LinkAssertionView
}

// LinkAssertionView is one piece of evidence for a visible link relation.
// A relation is visible while at least one assertion is active (not
// retracted); multiple assertions of the same triple coexist (ticket #66).
type LinkAssertionView struct {
	CreatedBy   EventID
	Actor       ActorRef
	Sources     []SourceRef
	RetractedBy *EventID
}

type LineageEdge struct {
	From      Ref
	Relation  string
	To        Ref
	Direction string
	Depth     int
	Origin    string
	CreatedBy EventID
}

type LineageGraph struct {
	byFrom map[string][]LinkView
	byTo   map[string][]LinkView
}

func InverseRelation(relation string) (string, bool) {
	inverse, ok := relationInverses[relation]
	return inverse, ok
}

func ValidRelation(relation string) bool {
	_, ok := relationInverses[relation]
	return ok
}

// ValidRelations returns the built-in relation vocabulary in sorted order.
// The vocabulary is the canonical source for link prompts and validation
// messages; ontologies may add further relations at runtime.
func ValidRelations() []string {
	out := make([]string, 0, len(relationInverses))
	for relation := range relationInverses {
		out = append(out, relation)
	}
	sort.Strings(out)
	return out
}

func applyLinkEvent(proj *Projection, event Event) error {
	if event.Value.Ref == nil {
		return fmt.Errorf("link event requires a target reference")
	}
	if !ValidRelation(event.Value.Text) {
		return fmt.Errorf("unsupported relation: %s", event.Value.Text)
	}

	linkID := LinkID(event.ID)
	switch event.Operation {
	case OpAssertLink, OpJoinScope:
		proj.Links[linkID] = &Link{
			ID:          linkID,
			From:        event.Target,
			Relation:    event.Value.Text,
			To:          *event.Value.Ref,
			Origin:      "asserted",
			CreatedBy:   event.ID,
			RetractedBy: nil,
		}
	case OpRetractLink, OpLeaveScope:
		targetID := retractionTarget(event)
		for _, existing := range proj.Links {
			if existing.RetractedBy != nil ||
				!refEqual(existing.From, event.Target) ||
				existing.Relation != event.Value.Text ||
				!refEqual(existing.To, *event.Value.Ref) {
				continue
			}
			// Targeted retraction names the assertion event; legacy retracts
			// (pre-#66) name no assertion and apply to the first active one.
			if targetID != "" && existing.CreatedBy != targetID {
				continue
			}
			retractedBy := event.ID
			existing.RetractedBy = &retractedBy
			return nil
		}
		return fmt.Errorf("link assertion not found for retraction")
	default:
		return fmt.Errorf("unsupported link operation: %s", event.Operation)
	}
	return nil
}

func LinksForRef(events []Event, ref Ref, effectiveAt, knownAt time.Time) ([]LinkView, error) {
	current, err := currentLinkViews(events, effectiveAt, knownAt)
	if err != nil {
		return nil, err
	}

	var views []LinkView
	for _, link := range current {
		if refEqual(link.From, ref) {
			views = append(views, LinkView{
				From:       ref,
				Relation:   link.Relation,
				To:         link.To,
				Direction:  "asserted",
				Origin:     "asserted",
				CreatedBy:  link.CreatedBy,
				Assertions: link.Assertions,
			})
			continue
		}
		if refEqual(link.To, ref) {
			inverse, ok := InverseRelation(link.Relation)
			if !ok {
				continue
			}
			views = append(views, LinkView{
				From:       ref,
				Relation:   inverse,
				To:         link.From,
				Direction:  "derived-inverse",
				Origin:     "asserted",
				CreatedBy:  link.CreatedBy,
				Assertions: link.Assertions,
			})
		}
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Relation != views[j].Relation {
			return views[i].Relation < views[j].Relation
		}
		return PresentationRefKey(views[i].To) < PresentationRefKey(views[j].To)
	})
	return views, nil
}

func BuildLineageGraph(events []Event, effectiveAt, knownAt time.Time) (*LineageGraph, error) {
	current, err := currentLinkViews(events, effectiveAt, knownAt)
	if err != nil {
		return nil, err
	}
	graph := &LineageGraph{
		byFrom: make(map[string][]LinkView),
		byTo:   make(map[string][]LinkView),
	}
	for _, link := range current {
		fromKey := CanonicalRefKey(link.From)
		toKey := CanonicalRefKey(link.To)
		graph.byFrom[fromKey] = append(graph.byFrom[fromKey], link)
		graph.byTo[toKey] = append(graph.byTo[toKey], link)
	}
	for key := range graph.byFrom {
		sort.Slice(graph.byFrom[key], func(i, j int) bool {
			if graph.byFrom[key][i].Relation != graph.byFrom[key][j].Relation {
				return graph.byFrom[key][i].Relation < graph.byFrom[key][j].Relation
			}
			return PresentationRefKey(graph.byFrom[key][i].To) < PresentationRefKey(graph.byFrom[key][j].To)
		})
	}
	for key := range graph.byTo {
		sort.Slice(graph.byTo[key], func(i, j int) bool {
			if graph.byTo[key][i].Relation != graph.byTo[key][j].Relation {
				return graph.byTo[key][i].Relation < graph.byTo[key][j].Relation
			}
			return PresentationRefKey(graph.byTo[key][i].From) < PresentationRefKey(graph.byTo[key][j].From)
		})
	}
	return graph, nil
}

func (g *LineageGraph) Walk(start Ref, direction string, maxDepth int, relations map[string]bool) ([]LineageEdge, error) {
	if maxDepth < 1 {
		return nil, nil
	}
	switch direction {
	case "outgoing", "incoming", "both":
	default:
		return nil, fmt.Errorf("invalid direction: %s", direction)
	}

	var edges []LineageEdge
	visited := map[string]bool{CanonicalRefKey(start): true}
	var walk func(Ref, int)
	walk = func(current Ref, depth int) {
		if depth > maxDepth {
			return
		}
		if direction == "outgoing" || direction == "both" {
			for _, link := range g.byFrom[CanonicalRefKey(current)] {
				if len(relations) > 0 && !relations[link.Relation] {
					continue
				}
				nextKey := CanonicalRefKey(link.To)
				if visited[nextKey] {
					continue
				}
				edges = append(edges, LineageEdge{
					From:      current,
					Relation:  link.Relation,
					To:        link.To,
					Direction: "outgoing",
					Depth:     depth,
					Origin:    link.Origin,
					CreatedBy: link.CreatedBy,
				})
				visited[nextKey] = true
				walk(link.To, depth+1)
			}
		}
		if direction == "incoming" || direction == "both" {
			for _, link := range g.byTo[CanonicalRefKey(current)] {
				inverse, ok := InverseRelation(link.Relation)
				if !ok {
					continue
				}
				if len(relations) > 0 && !relations[inverse] {
					continue
				}
				nextKey := CanonicalRefKey(link.From)
				if visited[nextKey] {
					continue
				}
				edges = append(edges, LineageEdge{
					From:      current,
					Relation:  inverse,
					To:        link.From,
					Direction: "incoming",
					Depth:     depth,
					Origin:    link.Origin,
					CreatedBy: link.CreatedBy,
				})
				visited[nextKey] = true
				walk(link.From, depth+1)
			}
		}
	}
	walk(start, 1)
	return edges, nil
}

func currentLinkViews(events []Event, effectiveAt, knownAt time.Time) ([]LinkView, error) {
	type linkKey struct {
		from     string
		relation string
		to       string
	}
	type assertionState struct {
		createdBy   EventID
		actor       ActorRef
		sources     []SourceRef
		retractedBy *EventID
	}
	type currentLink struct {
		from       Ref
		relation   string
		to         Ref
		assertions []assertionState
	}

	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		if event.Operation != OpAssertLink && event.Operation != OpRetractLink &&
			event.Operation != OpJoinScope && event.Operation != OpLeaveScope {
			continue
		}
		if event.EffectiveAt.After(effectiveAt) || event.RecordedAt.After(knownAt) {
			continue
		}
		filtered = append(filtered, event)
	}
	sortEventsByValidTime(filtered)

	links := make(map[linkKey]*currentLink)
	for _, event := range filtered {
		if event.Value.Ref == nil || !ValidRelation(event.Value.Text) {
			continue
		}
		key := linkKey{
			from:     CanonicalRefKey(event.Target),
			relation: event.Value.Text,
			to:       CanonicalRefKey(*event.Value.Ref),
		}
		current := links[key]
		if current == nil {
			current = &currentLink{from: event.Target, relation: event.Value.Text, to: *event.Value.Ref}
			links[key] = current
		}
		switch event.Operation {
		case OpAssertLink, OpJoinScope:
			current.assertions = append(current.assertions, assertionState{
				createdBy: event.ID,
				actor:     event.Actor,
				sources:   event.Sources,
			})
		case OpRetractLink, OpLeaveScope:
			targetID := retractionTarget(event)
			for i := range current.assertions {
				assertion := &current.assertions[i]
				if assertion.retractedBy != nil {
					continue
				}
				if targetID != "" && assertion.createdBy != targetID {
					continue
				}
				retractedBy := event.ID
				assertion.retractedBy = &retractedBy
				break
			}
		}
	}

	views := make([]LinkView, 0, len(links))
	for _, link := range links {
		active := make([]LinkAssertionView, 0, len(link.assertions))
		var latest EventID
		for _, assertion := range link.assertions {
			if assertion.retractedBy != nil {
				continue
			}
			active = append(active, LinkAssertionView{
				CreatedBy: assertion.createdBy,
				Actor:     assertion.actor,
				Sources:   assertion.sources,
			})
			latest = assertion.createdBy
		}
		if len(active) == 0 {
			continue
		}
		views = append(views, LinkView{
			From:       link.from,
			Relation:   link.relation,
			To:         link.to,
			Direction:  "asserted",
			Origin:     "asserted",
			CreatedBy:  latest,
			Assertions: active,
		})
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Relation != views[j].Relation {
			return views[i].Relation < views[j].Relation
		}
		if PresentationRefKey(views[i].From) != PresentationRefKey(views[j].From) {
			return PresentationRefKey(views[i].From) < PresentationRefKey(views[j].From)
		}
		return PresentationRefKey(views[i].To) < PresentationRefKey(views[j].To)
	})
	return views, nil
}

// retractionTarget names the assertion event a retract-link withdraws.
// New retracts carry the target as an event-kind cause (ticket #66); legacy
// pre-#66 retracts carry none and apply to the first active assertion.
func retractionTarget(event Event) EventID {
	for _, cause := range event.Causes {
		if cause.Kind == KindEvent {
			return EventID(cause.Entity)
		}
	}
	if len(event.Supersedes) > 0 {
		return event.Supersedes[0]
	}
	return ""
}

func refEqual(a, b Ref) bool {
	return a.Kind == b.Kind && a.Entity == b.Entity
}

// CanonicalRefKey is the identity key for a reference: Kind and Entity only,
// length-prefixed so no separator character can cause collisions. Mutable
// paths never participate in canonical identity.
func CanonicalRefKey(ref Ref) string {
	return strconv.Itoa(len(ref.Kind)) + ":" + string(ref.Kind) +
		strconv.Itoa(len(ref.Entity)) + ":" + ref.Entity
}

// PresentationRefKey is the canonical key plus the path. It is for display
// sorting and path lookup only; it never participates in identity,
// deduplication, retraction matching, or traversal.
func PresentationRefKey(ref Ref) string {
	return CanonicalRefKey(ref) + ":" + strings.Join(ref.Path, "/")
}
