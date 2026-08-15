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
}

type LinkView struct {
	From      Ref
	Relation  string
	To        Ref
	Direction string
	Origin    string
	CreatedBy EventID
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

func refEqual(a, b Ref) bool {
	return a.Kind == b.Kind && a.Entity == b.Entity
}

func refKey(ref Ref) string {
	return string(ref.Kind) + ":" + ref.Entity + ":" + strings.Join(ref.Path, "/")
}
