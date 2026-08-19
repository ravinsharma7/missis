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
	"home-project":    "home-of",
	"home-of":         "home-project",
}

type LinkView struct {
	From      Ref
	Relation  string
	To        Ref
	Direction string
	Origin    string
	CreatedBy EventID
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

func applyLinkEvent(proj *Projection, event Event) error {
	if event.Value.Ref == nil {
		return fmt.Errorf("link event requires a target reference")
	}
	if !ValidRelation(event.Value.Text) {
		return fmt.Errorf("unsupported relation: %s", event.Value.Text)
	}

	linkID := LinkID(event.ID)
	switch event.Operation {
	case OpAssertLink:
		for _, existing := range proj.Links {
			if existing.RetractedBy == nil &&
				refEqual(existing.From, event.Target) &&
				existing.Relation == event.Value.Text &&
				refEqual(existing.To, *event.Value.Ref) {
				return fmt.Errorf("duplicate link already exists")
			}
		}
		proj.Links[linkID] = &Link{
			ID:          linkID,
			From:        event.Target,
			Relation:    event.Value.Text,
			To:          *event.Value.Ref,
			Origin:      "asserted",
			CreatedBy:   event.ID,
			RetractedBy: nil,
		}
	case OpRetractLink:
		for _, existing := range proj.Links {
			if existing.RetractedBy == nil &&
				refEqual(existing.From, event.Target) &&
				existing.Relation == event.Value.Text &&
				refEqual(existing.To, *event.Value.Ref) {
				retractedBy := event.ID
				existing.RetractedBy = &retractedBy
				return nil
			}
		}
		return fmt.Errorf("link not found for retraction")
	default:
		return fmt.Errorf("unsupported link operation: %s", event.Operation)
	}
	return nil
}

func LinksForRef(events []Event, ref Ref, effectiveAt, knownAt time.Time) ([]LinkView, error) {
	type linkKey struct {
		from     string
		relation string
		to       string
	}
	type currentLink struct {
		from      Ref
		relation  string
		to        Ref
		createdBy EventID
		retracted bool
	}

	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		if event.Operation != OpAssertLink && event.Operation != OpRetractLink {
			continue
		}
		if event.EffectiveAt.After(effectiveAt) || event.RecordedAt.After(knownAt) {
			continue
		}
		filtered = append(filtered, event)
	}
	sortEventsByValidTime(filtered)

	links := make(map[linkKey]currentLink)
	for _, event := range filtered {
		if event.Value.Ref == nil || !ValidRelation(event.Value.Text) {
			continue
		}
		key := linkKey{
			from:     CanonicalRefKey(event.Target),
			relation: event.Value.Text,
			to:       CanonicalRefKey(*event.Value.Ref),
		}
		switch event.Operation {
		case OpAssertLink:
			links[key] = currentLink{
				from:      event.Target,
				relation:  event.Value.Text,
				to:        *event.Value.Ref,
				createdBy: event.ID,
			}
		case OpRetractLink:
			if current, ok := links[key]; ok {
				current.retracted = true
				links[key] = current
			}
		}
	}

	var views []LinkView
	for _, link := range links {
		if link.retracted {
			continue
		}
		if refEqual(link.from, ref) {
			views = append(views, LinkView{
				From:      ref,
				Relation:  link.relation,
				To:        link.to,
				Direction: "asserted",
				Origin:    "asserted",
				CreatedBy: link.createdBy,
			})
			continue
		}
		if refEqual(link.to, ref) {
			inverse, ok := InverseRelation(link.relation)
			if !ok {
				continue
			}
			views = append(views, LinkView{
				From:      ref,
				Relation:  inverse,
				To:        link.from,
				Direction: "derived-inverse",
				Origin:    "asserted",
				CreatedBy: link.createdBy,
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
	type currentLink struct {
		from      Ref
		relation  string
		to        Ref
		createdBy EventID
		retracted bool
	}

	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		if event.Operation != OpAssertLink && event.Operation != OpRetractLink {
			continue
		}
		if event.EffectiveAt.After(effectiveAt) || event.RecordedAt.After(knownAt) {
			continue
		}
		filtered = append(filtered, event)
	}
	sortEventsByValidTime(filtered)

	links := make(map[linkKey]currentLink)
	for _, event := range filtered {
		if event.Value.Ref == nil || !ValidRelation(event.Value.Text) {
			continue
		}
		key := linkKey{
			from:     CanonicalRefKey(event.Target),
			relation: event.Value.Text,
			to:       CanonicalRefKey(*event.Value.Ref),
		}
		switch event.Operation {
		case OpAssertLink:
			links[key] = currentLink{
				from:      event.Target,
				relation:  event.Value.Text,
				to:        *event.Value.Ref,
				createdBy: event.ID,
			}
		case OpRetractLink:
			if current, ok := links[key]; ok {
				current.retracted = true
				links[key] = current
			}
		}
	}

	views := make([]LinkView, 0, len(links))
	for _, link := range links {
		if link.retracted {
			continue
		}
		views = append(views, LinkView{
			From:      link.from,
			Relation:  link.relation,
			To:        link.to,
			Direction: "asserted",
			Origin:    "asserted",
			CreatedBy: link.createdBy,
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
