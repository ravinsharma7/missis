package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

var relationInverses = map[string]string{
	"blocks":        "blocked-by",
	"blocked-by":    "blocks",
	"caused-by":     "causes",
	"causes":        "caused-by",
	"duplicates":    "duplicated-by",
	"duplicated-by": "duplicates",
	"supports":      "supported-by",
	"supported-by":  "supports",
	"contradicts":   "contradicted-by",
	"contradicted-by": "contradicts",
	"implements":     "implemented-by",
	"implemented-by": "implements",
	"tracks":         "tracked-by",
	"tracked-by":     "tracks",
	"documents":      "documented-by",
	"documented-by":  "documents",
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
			ID:         linkID,
			From:       event.Target,
			Relation:   event.Value.Text,
			To:         *event.Value.Ref,
			Origin:     "asserted",
			CreatedBy:  event.ID,
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
	sortEvents(filtered)

	links := make(map[linkKey]currentLink)
	for _, event := range filtered {
		if event.Value.Ref == nil || !ValidRelation(event.Value.Text) {
			continue
		}
		key := linkKey{
			from:     refKey(event.Target),
			relation: event.Value.Text,
			to:       refKey(*event.Value.Ref),
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
		return refKey(views[i].To) < refKey(views[j].To)
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
		fromKey := refKey(link.From)
		toKey := refKey(link.To)
		graph.byFrom[fromKey] = append(graph.byFrom[fromKey], link)
		graph.byTo[toKey] = append(graph.byTo[toKey], link)
	}
	for key := range graph.byFrom {
		sort.Slice(graph.byFrom[key], func(i, j int) bool {
			if graph.byFrom[key][i].Relation != graph.byFrom[key][j].Relation {
				return graph.byFrom[key][i].Relation < graph.byFrom[key][j].Relation
			}
			return refKey(graph.byFrom[key][i].To) < refKey(graph.byFrom[key][j].To)
		})
	}
	for key := range graph.byTo {
		sort.Slice(graph.byTo[key], func(i, j int) bool {
			if graph.byTo[key][i].Relation != graph.byTo[key][j].Relation {
				return graph.byTo[key][i].Relation < graph.byTo[key][j].Relation
			}
			return refKey(graph.byTo[key][i].From) < refKey(graph.byTo[key][j].From)
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
	visited := map[string]bool{refKey(start): true}
	var walk func(Ref, int)
	walk = func(current Ref, depth int) {
		if depth > maxDepth {
			return
		}
		if direction == "outgoing" || direction == "both" {
			for _, link := range g.byFrom[refKey(current)] {
				if len(relations) > 0 && !relations[link.Relation] {
					continue
				}
				nextKey := refKey(link.To)
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
			for _, link := range g.byTo[refKey(current)] {
				inverse, ok := InverseRelation(link.Relation)
				if !ok {
					continue
				}
				if len(relations) > 0 && !relations[inverse] {
					continue
				}
				nextKey := refKey(link.From)
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
	sortEvents(filtered)

	links := make(map[linkKey]currentLink)
	for _, event := range filtered {
		if event.Value.Ref == nil || !ValidRelation(event.Value.Text) {
			continue
		}
		key := linkKey{
			from:     refKey(event.Target),
			relation: event.Value.Text,
			to:       refKey(*event.Value.Ref),
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
		if refKey(views[i].From) != refKey(views[j].From) {
			return refKey(views[i].From) < refKey(views[j].From)
		}
		return refKey(views[i].To) < refKey(views[j].To)
	})
	return views, nil
}

func refEqual(a, b Ref) bool {
	return a.Kind == b.Kind && a.Entity == b.Entity
}

func refKey(ref Ref) string {
	return string(ref.Kind) + ":" + ref.Entity + ":" + strings.Join(ref.Path, "/")
}
