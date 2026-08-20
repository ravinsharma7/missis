package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

type ticketMembership struct {
	projects []string
	groups   []string
}

type entityCounts struct {
	groups   int
	projects int
	tickets  int
}

type membershipSnapshot struct {
	links []model.LinkView
}

type entityItem struct {
	summary missis.EntitySummary
	counts  entityCounts
}

// loadMembershipSnapshot builds one bitemporal link read model for TUI
// ownership columns, entity counts, and ticket membership summaries.
func loadMembershipSnapshot(client *missis.Client, now time.Time) (membershipSnapshot, error) {
	events, err := client.LoadLinkEvents(context.Background())
	if err != nil {
		return membershipSnapshot{}, err
	}
	links, err := model.VisibleLinks(events, now, now)
	if err != nil {
		return membershipSnapshot{}, err
	}
	return membershipSnapshot{links: links}, nil
}

func parseMembershipRef(ref string) (model.Ref, bool) {
	kind, entity, ok := strings.Cut(ref, ":")
	if !ok || kind == "" || entity == "" {
		return model.Ref{}, false
	}
	if kind == string(model.KindTicket) {
		return model.Ref{Kind: model.KindTicket, Entity: ref}, true
	}
	return model.Ref{Kind: model.Kind(kind), Entity: entity}, true
}

func sameMembershipRef(a, b model.Ref) bool {
	return a.Kind == b.Kind && a.Entity == b.Entity
}

func (s membershipSnapshot) counts(ref string) entityCounts {
	var counts entityCounts
	wanted, ok := parseMembershipRef(ref)
	if !ok {
		return counts
	}
	for _, link := range s.links {
		if wanted.Kind == model.KindGroup && sameMembershipRef(link.From, wanted) {
			if link.Relation == model.RelationContains && link.To.Kind == model.KindProject {
				counts.projects++
			}
			if link.Relation == model.RelationGoverns && link.To.Kind == model.KindProject {
				counts.projects++
			}
			if link.Relation == model.RelationContains && link.To.Kind == model.KindTicket {
				counts.tickets++
			}
			continue
		}
		if wanted.Kind == model.KindProject {
			if link.From.Kind == model.KindGroup && link.Relation == model.RelationContains && sameMembershipRef(link.To, wanted) {
				counts.groups++
			}
			if link.From.Kind == model.KindTicket && link.Relation == model.RelationHasHome && sameMembershipRef(link.To, wanted) {
				counts.tickets++
			}
		}
	}
	return counts
}

func (s membershipSnapshot) ticketMembership(ref string) ticketMembership {
	var membership ticketMembership
	wanted, ok := parseMembershipRef(ref)
	if !ok || wanted.Kind != model.KindTicket {
		return membership
	}
	for _, link := range s.links {
		if link.From.Kind == model.KindTicket && sameMembershipRef(link.From, wanted) && link.Relation == model.RelationHasHome && link.To.Kind == model.KindProject {
			membership.projects = appendUniqueString(membership.projects, "project:"+link.To.Entity)
		}
		if link.From.Kind == model.KindGroup && link.Relation == model.RelationContains && sameMembershipRef(link.To, wanted) {
			membership.groups = appendUniqueString(membership.groups, "group:"+link.From.Entity)
		}
	}
	sort.Strings(membership.projects)
	sort.Strings(membership.groups)
	return membership
}

// membershipCounts derives how many groups, projects, and tickets are linked
// to an entity from one bitemporal link snapshot.
func membershipCounts(client *missis.Client, ref string) (entityCounts, error) {
	now := time.Now().UTC()
	snapshot, err := loadMembershipSnapshot(client, now)
	if err != nil {
		return entityCounts{}, err
	}
	return snapshot.counts(ref), nil
}

func (c entityCounts) label(kind string) string {
	if kind == "groups" {
		return fmt.Sprintf("%d projects · %d tickets", c.projects, c.tickets)
	}
	return fmt.Sprintf("%d groups · %d tickets", c.groups, c.tickets)
}

func loadTicketMemberships(client *missis.Client, summaries []missis.TicketSummary) (map[string]ticketMembership, error) {
	now := time.Now().UTC()
	snapshot, err := loadMembershipSnapshot(client, now)
	if err != nil {
		return nil, err
	}
	memberships := make(map[string]ticketMembership, len(summaries))
	for _, summary := range summaries {
		memberships[ticketSummaryKey(summary)] = snapshot.ticketMembership(summary.ID)
	}
	return memberships, nil
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// loadEntityItems lists entities of a kind with their membership counts
// attached, mirroring loadTicketSummaries as the single load path for entity
// lists.
func loadEntityItems(client *missis.Client, kind model.Kind) ([]entityItem, error) {
	now := time.Now().UTC()
	raw, err := client.ListEntities(context.Background(), kind, missis.ListFilter{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return nil, err
	}
	snapshot, err := loadMembershipSnapshot(client, now)
	if err != nil {
		return nil, err
	}
	items := make([]entityItem, 0, len(raw))
	for _, summary := range raw {
		items = append(items, entityItem{summary: summary, counts: snapshot.counts(summary.Ref)})
	}
	return items, nil
}
