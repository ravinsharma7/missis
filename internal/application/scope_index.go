package application

import (
	"sort"
	"time"

	"github.com/ravinsharma7/missis/internal/model"
)

type scopeTicketIndex struct {
	projectIDs     map[string]bool
	groupIDs       map[string]bool
	projectTickets map[string]map[model.TicketID]bool
	groupTickets   map[string]map[model.TicketID]bool
	groupProjects  map[string]map[string]bool
}

func buildScopeTicketIndex(events []model.Event, effectiveAt, knownAt time.Time) (*scopeTicketIndex, error) {
	index := &scopeTicketIndex{
		projectIDs:     make(map[string]bool),
		groupIDs:       make(map[string]bool),
		projectTickets: make(map[string]map[model.TicketID]bool),
		groupTickets:   make(map[string]map[model.TicketID]bool),
		groupProjects:  make(map[string]map[string]bool),
	}
	for _, event := range events {
		if event.Target.Kind == model.KindProject {
			index.projectIDs[event.Target.Entity] = true
		}
		if event.Target.Kind == model.KindGroup {
			index.groupIDs[event.Target.Entity] = true
		}
		if event.Value.Ref != nil {
			if event.Value.Ref.Kind == model.KindProject {
				index.projectIDs[event.Value.Ref.Entity] = true
			}
			if event.Value.Ref.Kind == model.KindGroup {
				index.groupIDs[event.Value.Ref.Entity] = true
			}
		}
	}
	links, err := model.VisibleLinks(events, effectiveAt, knownAt)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		if link.Direction != "asserted" {
			continue
		}
		switch {
		case link.From.Kind == model.KindProject && link.Relation == model.RelationContains && link.To.Kind == model.KindTicket:
			addTicket(index.projectTickets, link.From.Entity, model.TicketID(link.To.Entity))
		case link.From.Kind == model.KindTicket && link.Relation == model.RelationHasHome && link.To.Kind == model.KindProject:
			addTicket(index.projectTickets, link.To.Entity, model.TicketID(link.From.Entity))
		case link.From.Kind == model.KindGroup && link.Relation == model.RelationContains && link.To.Kind == model.KindTicket:
			addTicket(index.groupTickets, link.From.Entity, model.TicketID(link.To.Entity))
		case link.From.Kind == model.KindGroup && (link.Relation == model.RelationContains || link.Relation == model.RelationGoverns) && link.To.Kind == model.KindProject:
			if index.groupProjects[link.From.Entity] == nil {
				index.groupProjects[link.From.Entity] = make(map[string]bool)
			}
			index.groupProjects[link.From.Entity][link.To.Entity] = true
		}
	}
	for groupID, projects := range index.groupProjects {
		for projectID := range projects {
			for ticketID := range index.projectTickets[projectID] {
				addTicket(index.groupTickets, groupID, ticketID)
			}
		}
	}
	return index, nil
}

func addTicket(index map[string]map[model.TicketID]bool, scopeID string, ticketID model.TicketID) {
	if index[scopeID] == nil {
		index[scopeID] = make(map[model.TicketID]bool)
	}
	index[scopeID][ticketID] = true
}

func unionScopeTickets(index map[string]map[model.TicketID]bool, ids []string) map[model.TicketID]bool {
	result := make(map[model.TicketID]bool)
	for _, id := range ids {
		for ticketID := range index[id] {
			result[ticketID] = true
		}
	}
	return result
}

func sortedScopeIDs(ids map[string]bool) []string {
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}
