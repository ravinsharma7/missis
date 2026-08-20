package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/pkg/missis"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

const (
	defaultWidth    = 80
	defaultHeight   = 24
	refreshInterval = 2 * time.Second
)

type detailState struct {
	summary  missis.TicketSummary
	entity   *missis.EntitySummary
	lines    []string
	offset   int
	showRefs bool
}

// quickLinkTarget is a context-aware link target. Membership links sometimes
// need to be asserted from the target's container (for example, adding a
// project to a group), so the picker keeps the actual source separate from
// the item shown to the user.
type quickLinkTarget struct {
	ref      string
	labelRef string
	title    string
	hint     string
	source   string
	relation string
	existing bool
}

type quickLinkPicker struct {
	targets          []quickLinkTarget
	relations        []string
	selected         int
	marked           map[int]bool
	selectionTouched bool
	relation         string
}

type refreshMsg struct{}

type scopeSelection struct {
	Projects []string
	Groups   []string
	Unscoped bool
}

func normalizeScopeList(values ...string) []string {
	seen := make(map[string]struct{})
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" && item != "none" {
				seen[item] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(seen))
	for item := range seen {
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

func newScopeSelection(projects, groups []string) scopeSelection {
	return scopeSelection{
		Projects: normalizeScopeList(projects...),
		Groups:   normalizeScopeList(groups...),
	}
}

func (s scopeSelection) empty() bool {
	return len(s.Projects) == 0 && len(s.Groups) == 0 && !s.Unscoped
}

func (s scopeSelection) contains(kind, ref string) bool {
	values := s.Groups
	if kind == "project" {
		values = s.Projects
	}
	for _, value := range values {
		if value == ref {
			return true
		}
	}
	return false
}

func (s *scopeSelection) toggle(kind, ref string) {
	if kind != "project" && kind != "group" {
		return
	}
	s.Unscoped = false
	values := &s.Groups
	if kind == "project" {
		values = &s.Projects
	}
	for i, value := range *values {
		if value == ref {
			*values = append((*values)[:i], (*values)[i+1:]...)
			return
		}
	}
	*values = append(*values, ref)
	*values = normalizeScopeList((*values)...)
}

func scopeLabel(values []string, empty string) string {
	if len(values) == 0 {
		return empty
	}
	return strings.Join(values, ",")
}

type tuiModel struct {
	client                  *missis.Client
	summaries               []missis.TicketSummary
	entities                []entityItem
	selected                int
	view                    string
	kind                    string
	detail                  *detailState
	compareA                *missis.TicketSummary
	compareB                *missis.TicketSummary
	message                 string
	linkNotice              string
	linkNoticeRef           string
	linkNoticeError         bool
	err                     error
	width                   int
	height                  int
	listOffset              int
	statsOffset             int
	input                   string
	cursor                  int
	inputMode               string
	inputErr                string
	quickLink               *quickLinkPicker
	ticketListMode          string
	ticketMemberships       map[string]ticketMembership
	ticketMembershipErr     error
	ticketMembershipsLoaded bool
	activeScope             scopeSelection
	draftScope              scopeSelection
	contextPrevView         string
	ctxSelected             int
	prevView                string
	helpOffset              int
	compareOffset           int
	ctxOffset               int
	contextProjects         []missis.EntitySummary
	contextGroups           []missis.EntitySummary
	contextCounts           map[string]int
	contextCountErr         map[string]error
	contextLoaded           bool
	draftCount              int
	draftCountErr           error
	draftCountReady         bool
	contextEffectiveAt      time.Time
}

func newModel() (*tuiModel, error) {
	svc, err := application.Open("")
	if err != nil {
		return nil, err
	}
	client := missis.NewClient(svc)
	projectValues, groupValues := []string(nil), []string(nil)
	activePath := filepath.Join(".missis.d", "active.local.md")
	if _, statErr := os.Stat(activePath); statErr != nil {
		activePath = filepath.Join(".missis.d", "active.example.md")
	}
	if data, readErr := os.ReadFile(activePath); readErr == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "project:") {
				projectValues = append(projectValues, strings.TrimSpace(strings.TrimPrefix(line, "project:")))
			}
			if strings.HasPrefix(line, "group:") {
				groupValues = append(groupValues, strings.TrimSpace(strings.TrimPrefix(line, "group:")))
			}
		}
	}
	if env := os.Getenv("MISSIS_PROJECT"); env != "" {
		projectValues = []string{env}
	}
	if env := os.Getenv("MISSIS_GROUP"); env != "" {
		groupValues = []string{env}
	}
	activeScope := scopeSelection{
		Projects: normalizeScopeList(projectValues...),
		Groups:   normalizeScopeList(groupValues...),
	}
	summaries, err := loadTicketSummariesForScope(client, activeScope)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &tuiModel{
		client:         client,
		summaries:      summaries,
		view:           "list",
		kind:           "tickets",
		ticketListMode: "compact",
		activeScope:    activeScope,
		draftScope:     activeScope,
	}, nil
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshMsg{} })
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		m.refresh()
		return m, tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshMsg{} })
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampListOffset()
		if m.view == "detail" && m.detail != nil && m.client != nil {
			if !m.detail.showRefs {
				if lines, err := ticketLines(m.client, m.detail.summary, m.renderWidth()); err == nil {
					m.detail.lines = lines
				}
			}
			m.clampDetailOffset()
		}
		if m.view == "stats" {
			m.clampStatsOffset()
		}
		if m.view == "context" {
			m.clampContextWindow()
		}
		return m, nil
	case tea.KeyMsg:
		m = m.dismissErrorForKey(msg)
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.view != "input" && m.view != "context" && m.view != "link-picker" && m.view != "link-relations" {
				return m, tea.Quit
			}
		case "?":
			if m.view != "input" && m.view != "context" && m.view != "link-picker" && m.view != "link-relations" && m.view != "help" {
				m.prevView = m.view
				m.view = "help"
				m.helpOffset = 0
				return m, nil
			}
		}
	}

	switch m.view {
	case "list":
		return m.updateList(msg)
	case "detail":
		return m.updateDetail(msg)
	case "compare":
		return m.updateCompare(msg)
	case "stats":
		return m.updateStats(msg)
	case "input":
		return m.updateInput(msg)
	case "link-picker", "link-relations":
		return m.updateQuickLink(msg)
	case "context":
		return m.updateContext(msg)
	case "help":
		return m.updateHelp(msg)
	default:
		return m, nil
	}
}

// dismissErrorForKey is the single transition point for dismissing a
// persistent page error. It runs before global handling and view dispatch so
// navigation can recover from an error screen without changing action or
// prompt-validation semantics.
func (m tuiModel) dismissErrorForKey(key tea.KeyMsg) tuiModel {
	if m.err != nil && m.isNavigationKey(key.String()) {
		m.err = nil
	}
	return m
}

// isNavigationKey is deliberately view-aware. Several navigation-looking
// runes are ordinary input while the title/create/link prompt is active, and
// action keys must preserve errors until the user chooses a navigation path.
func (m tuiModel) isNavigationKey(key string) bool {
	if m.view == "input" {
		return key == "esc"
	}
	if m.view == "link-picker" || m.view == "link-relations" {
		return key == "esc"
	}

	switch key {
	case "up", "down", "k", "j", "pgup", "pgdown", "home", "end", "G", "esc":
		return true
	case "b":
		return m.view == "detail" || m.view == "compare" || m.view == "stats" || m.view == "context" || m.view == "help"
	case "t", "p", "g":
		return true
	case "r":
		return m.view == "list" || m.view == "detail" || m.view == "stats" || m.view == "context"
	case "?":
		return m.view != "context" && m.view != "help"
	case "s":
		return m.view == "list" && !m.isEntityList()
	case "m":
		return m.view == "list" && !m.isEntityList()
	case "x":
		return m.view == "list"
	case "f":
		return (m.view == "detail" && m.detail != nil && m.detail.entity != nil) || (m.view == "list" && m.isEntityList())
	case "enter", " ":
		return m.view == "list" || m.view == "context"
	default:
		return false
	}
}

// goBack leaves the current subpage and returns to the page it was opened
// from. b is the standard back key on every subpage (esc stays as an alias);
// text-input prompts type b and q, while the ticket-filter picker uses b/esc to
// cancel and leaves q inert. q quits regular views, and ctrl+c is the hard
// exit everywhere. Errors are dismissed on the way back so an error screen
// never traps the user.
func (m tuiModel) goBack() tuiModel {
	switch m.view {
	case "detail", "compare", "stats":
		m.view = "list"
		m.detail = nil
		m.compareA = nil
		m.compareB = nil
		m.message = ""
	case "input":
		if m.inputMode == "create" || m.inputMode == "create-ticket" {
			m.view = "list"
		} else {
			m.view = "detail"
		}
		m.inputMode = ""
		m.input = ""
		m.cursor = 0
		m.inputErr = ""
		m.message = ""
	case "link-picker", "link-relations":
		m.view = "detail"
		m.quickLink = nil
		m.message = ""
	case "context":
		m.view = "list"
		if m.contextPrevView == "detail" && m.detail != nil {
			m.view = "detail"
		}
		m.draftScope = m.currentScope()
		m.contextPrevView = ""
		m.input = ""
		m.cursor = 0
		m.inputErr = ""
		m.message = ""
	case "help":
		m.view = m.prevView
		m.prevView = ""
		if m.view == "" {
			m.view = "list"
		}
	default:
		return m
	}
	m.err = nil
	return m
}

func (m tuiModel) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.clampListOffset()
		}
	case "down", "j":
		if m.selected < len(m.summaries)-1 {
			m.selected++
			m.clampListOffset()
		}
	case "pgup":
		if len(m.summaries) > 0 {
			visible := m.listVisibleRows()
			rel := m.selected - m.listOffset
			m.selected -= visible
			if m.selected < 0 {
				m.selected = 0
			}
			m.listOffset = m.selected - rel
			m.clampListOffset()
		}
	case "pgdown":
		if len(m.summaries) > 0 {
			visible := m.listVisibleRows()
			rel := m.selected - m.listOffset
			m.selected += visible
			if m.selected > len(m.summaries)-1 {
				m.selected = len(m.summaries) - 1
			}
			m.listOffset = m.selected - rel
			m.clampListOffset()
		}
	case "enter", " ":
		if m.isEntityList() {
			if len(m.entities) == 0 {
				return m, nil
			}
			ent := m.entities[m.selected].summary
			m.clearLinkNoticeIfDifferent(ent.Ref)
			lines, err := entityLines(m.client, ent, m.renderWidth())
			if err != nil {
				m.err = err
				return m, nil
			}
			m.detail = &detailState{entity: &ent, lines: lines}
			m.view = "detail"
			return m, nil
		}
		if len(m.summaries) == 0 {
			return m, nil
		}
		summary := m.summaries[m.selected]
		m.clearLinkNoticeIfDifferent(summary.Ref)
		lines, err := ticketLines(m.client, summary, m.renderWidth())
		if err != nil {
			m.err = err
			return m, nil
		}
		m.detail = &detailState{summary: summary, lines: lines}
		m.view = "detail"
	case "c":
		if len(m.summaries) == 0 {
			return m, nil
		}
		m.compareA = &m.summaries[m.selected]
		m.message = "compare A: " + m.compareA.Ref
	case "v":
		if m.compareA == nil {
			m.message = "press c first to select compare A"
			return m, nil
		}
		if len(m.summaries) == 0 {
			return m, nil
		}
		m.compareB = &m.summaries[m.selected]
		m.view = "compare"
		m.message = ""
	case "e":
		if len(m.summaries) == 0 {
			return m, nil
		}
		summary := m.summaries[m.selected]
		dst, err := exportTicket(m.client, summary)
		if err != nil {
			m.err = err
		} else {
			m.err = nil
			m.message = "exported " + summary.Ref + " -> " + dst
		}
	case "r":
		m.refresh()
	case "m":
		if m.isEntityList() {
			return m, nil
		}
		if m.ticketListMode == "ownership" {
			m.ticketListMode = "compact"
			m.ticketMembershipErr = nil
			m.message = "ticket list: compact mode"
			return m, nil
		}
		m.ticketListMode = "ownership"
		m.refreshTicketMemberships()
		if m.ticketMembershipErr == nil {
			m.message = "ticket list: ownership mode"
		}
	case "esc":
		m.err = nil
	case "t", "p", "g":
		updated, _ := m.switchListForKey(key.String())
		return updated, nil
	case "n":
		if m.isEntityList() {
			m.inputMode = "create"
			m.input = ""
			m.cursor = 0
			m.inputErr = ""
			m.view = "input"
			m.message = ""
			return m, nil
		}
		m.inputMode = "create-ticket"
		m.input = ""
		m.cursor = 0
		m.inputErr = ""
		m.view = "input"
		m.message = ""
	case "f":
		if !m.isEntityList() || len(m.entities) == 0 {
			return m, nil
		}
		ref := m.entities[m.selected].summary.Ref
		kind, id, ok := strings.Cut(ref, ":")
		if !ok || (kind != "project" && kind != "group") {
			m.message = "cannot filter: unknown entity ref"
			return m, nil
		}
		scope := scopeSelection{}
		if kind == "project" {
			scope.Projects = []string{id}
		} else {
			scope.Groups = []string{id}
		}
		return m.applyContextSelection(scope), nil
	case "s":
		if !m.isEntityList() {
			m.refreshTicketMemberships()
			m.view = "stats"
			m.statsOffset = 0
			if m.ticketMembershipErr == nil {
				m.message = ""
			}
		}
	case "x":
		m = m.openContext(m.currentScope())
	}
	return m, nil
}

func (m tuiModel) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	rendered := m.renderedDetailLines()
	maxOffset := len(rendered) - m.detailWindowRows()
	if maxOffset < 0 {
		maxOffset = 0
	}
	switch key.String() {
	case "up", "k":
		if m.detail.offset > 0 {
			m.detail.offset--
		}
	case "down", "j":
		if m.detail.offset < maxOffset {
			m.detail.offset++
		}
	case "pgup":
		m.detail.offset -= m.detailWindowRows()
		if m.detail.offset < 0 {
			m.detail.offset = 0
		}
	case "pgdown":
		m.detail.offset += m.detailWindowRows()
		if m.detail.offset > maxOffset {
			m.detail.offset = maxOffset
		}
	case "home":
		m.detail.offset = 0
	case "G", "end":
		m.detail.offset = maxOffset
	case "t", "p", "g":
		updated, _ := m.switchListForKey(key.String())
		return updated, nil
	case "b", "esc":
		m = m.goBack()
	case "e":
		if m.detail.entity != nil {
			m.message = "export is ticket-only"
			return m, nil
		}
		dst, err := exportTicket(m.client, m.detail.summary)
		if err != nil {
			m.err = err
		} else {
			m.err = nil
			m.message = "exported " + m.detail.summary.Ref + " -> " + dst
		}
	case "T":
		if m.detail.entity != nil {
			m.message = "title edit is ticket-only"
			return m, nil
		}
		m.input = m.detail.summary.Title
		m.cursor = len([]rune(m.input))
		m.inputErr = ""
		m.view = "input"
	case "f":
		if m.detail == nil || m.detail.entity == nil {
			m.message = "filter applies to projects/groups"
			return m, nil
		}
		kind, id, ok := strings.Cut(m.detail.entity.Ref, ":")
		if !ok {
			m.message = "cannot filter: unknown entity ref"
			return m, nil
		}
		scope := m.currentScope()
		switch kind {
		case "project", "group":
			scope.toggle(kind, id)
		default:
			m.message = "filter applies to projects/groups"
			return m, nil
		}
		m = m.openContext(scope)
		m.message = "scope added to draft; enter applies"
		return m, nil
	case "r":
		m.refresh()
	case "R":
		m.detail.showRefs = !m.detail.showRefs
		m.refreshDetail()
	case "l":
		if m.detail == nil {
			return m, nil
		}
		return m.openQuickLink(), nil
	}
	return m, nil
}

func (m tuiModel) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "enter":
		switch m.inputMode {
		case "create":
			return m.submitCreate()
		case "create-ticket":
			return m.submitCreateTicket()
		case "link":
			return m.submitLinkAction()
		default:
			return m.submitTitle()
		}
	case "esc":
		m = m.goBack()
	default:
		var changed bool
		m.input, m.cursor, changed = editInput(m.input, m.cursor, key)
		if changed {
			m.inputErr = ""
		}
	}
	return m, nil
}

func (m tuiModel) openQuickLink() tuiModel {
	targets, err := m.quickLinkTargets("")
	if err != nil {
		m.err = err
		return m
	}
	marked := make(map[int]bool)
	if m.detail != nil && m.detail.entity == nil {
		existing, err := m.quickLinkExistingTargets(targets)
		if err != nil {
			m.err = err
			return m
		}
		for index := range existing {
			marked[index] = true
			targets[index].existing = true
		}
	}
	m.quickLink = &quickLinkPicker{targets: targets, marked: marked}
	m.view = "link-picker"
	m.inputErr = ""
	m.message = ""
	m.clampQuickLinkSelection()
	return m
}

func (m tuiModel) openAdvancedLinkInput() tuiModel {
	m.quickLink = nil
	m.inputMode = "link"
	m.input = ""
	m.cursor = 0
	m.inputErr = ""
	m.view = "input"
	m.message = ""
	return m
}

func (m tuiModel) quickLinkTargets(relation string) ([]quickLinkTarget, error) {
	if m.detail == nil {
		return nil, errors.New("link picker requires an open detail")
	}
	now := time.Now().UTC()
	var targets []quickLinkTarget
	addEntities := func(kind model.Kind, source, linkRelation, hint string) error {
		entities, err := m.client.ListEntities(context.Background(), kind, missis.ListFilter{EffectiveAt: now, KnownAt: now})
		if err != nil {
			return err
		}
		for _, entity := range entities {
			targets = append(targets, quickLinkTarget{
				ref:      entity.Ref,
				title:    entity.Title,
				hint:     hint,
				source:   source,
				relation: linkRelation,
			})
		}
		return nil
	}
	addTickets := func(source, linkRelation, hint string) error {
		tickets, err := m.client.ListTicketSummaries(context.Background(), now)
		if err != nil {
			return err
		}
		for _, ticket := range tickets {
			if m.detail.entity == nil && ticket.Ref == m.detail.summary.Ref {
				continue
			}
			targets = append(targets, quickLinkTarget{
				ref:      ticket.Ref,
				title:    ticket.Title,
				hint:     hint,
				source:   source,
				relation: linkRelation,
			})
		}
		return nil
	}

	if relation != "" {
		if m.detail.entity != nil {
			return nil, errors.New("ticket relations are only available from ticket detail")
		}
		if err := addTickets(m.detail.summary.Ref, relation, "ticket link"); err != nil {
			return nil, err
		}
		return targets, nil
	}

	if m.detail.entity != nil {
		entity := m.detail.entity
		switch {
		case strings.HasPrefix(entity.Ref, "project:"):
			// Membership is stored from the group to the project. The picker
			// hides that storage detail and lets users act from the project.
			if err := addEntities(model.KindGroup, "", model.RelationContains, "add project to group"); err != nil {
				return nil, err
			}
			for i := range targets {
				targets[i].labelRef = targets[i].ref
				targets[i].source = targets[i].ref
				targets[i].ref = entity.Ref
				targets[i].title = entity.Title + " · " + targets[i].title
			}
		case strings.HasPrefix(entity.Ref, "group:"):
			if err := addEntities(model.KindProject, entity.Ref, model.RelationContains, "add project"); err != nil {
				return nil, err
			}
			if err := addTickets(entity.Ref, model.RelationContains, "add ticket"); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported entity ref: %s", entity.Ref)
		}
		return targets, nil
	}

	// Ticket membership is also presented from the ticket side. Selecting a
	// project assigns home; selecting a group asserts the group's contains
	// relation for this ticket.
	if err := addEntities(model.KindProject, m.detail.summary.Ref, model.RelationHasHome, "set home project"); err != nil {
		return nil, err
	}
	groupStart := len(targets)
	if err := addEntities(model.KindGroup, "", model.RelationContains, "add ticket to group"); err != nil {
		return nil, err
	}
	for i := groupStart; i < len(targets); i++ {
		targets[i].labelRef = targets[i].ref
		targets[i].source = targets[i].ref
		targets[i].ref = m.detail.summary.Ref
	}
	return targets, nil
}

// quickLinkExistingTargets returns the project/group membership targets that
// already belong to the open ticket. They are shown as checked when l opens so
// the picker reflects the ticket's current membership instead of looking like
// an empty add-only form.
func (m tuiModel) quickLinkExistingTargets(targets []quickLinkTarget) (map[int]bool, error) {
	existing := make(map[int]bool)
	if m.detail == nil || m.detail.entity != nil {
		return existing, nil
	}
	now := time.Now().UTC()
	links, err := m.client.ShowReferences(context.Background(), m.detail.summary.Ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return nil, err
	}
	for index, target := range targets {
		for _, link := range links {
			matched := false
			switch target.relation {
			case model.RelationHasHome:
				matched = link.Direction == "asserted" && link.Relation == model.RelationHasHome && link.To == target.ref
			case model.RelationContains:
				matched = link.Direction == "derived-inverse" && link.Relation == model.RelationContainedBy && link.To == target.source
			}
			if matched {
				existing[index] = true
				break
			}
		}
	}
	return existing, nil
}

// linkAlreadyExists checks the effective asserted link before the TUI emits a
// new immutable-store event. The service deliberately keeps SetLink additive;
// this UI-level guard avoids growing duplicate evidence when a user selects
// the same value again.
func (m tuiModel) linkAlreadyExists(source, relation, target string) (bool, error) {
	now := time.Now().UTC()
	links, err := m.client.ShowReferences(context.Background(), source, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return false, err
	}
	for _, link := range links {
		if link.Direction != "asserted" || link.Relation != relation {
			continue
		}
		matched, err := m.linkRefsEqual(target, link.To)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func (m tuiModel) linkRefsEqual(wanted, actual string) (bool, error) {
	if wanted == actual {
		return true, nil
	}
	if strings.HasPrefix(wanted, "ticket:") && strings.HasPrefix(actual, "ticket:") {
		return strings.TrimPrefix(wanted, "ticket:") == strings.TrimPrefix(actual, "ticket:"), nil
	}
	if !strings.HasPrefix(wanted, "#") || !strings.HasPrefix(actual, "ticket:") {
		return false, nil
	}
	tickets, err := m.client.ListTicketSummaries(context.Background(), time.Now().UTC())
	if err != nil {
		return false, err
	}
	for _, ticket := range tickets {
		if ticket.Ref == wanted {
			return ticket.ID == actual || strings.TrimPrefix(ticket.ID, "ticket:") == strings.TrimPrefix(actual, "ticket:"), nil
		}
	}
	return false, nil
}

func quickTicketRelations() []string {
	var relations []string
	for _, relation := range model.ValidRelations() {
		switch relation {
		case model.RelationContains, model.RelationContainedBy, model.RelationGoverns, model.RelationGovernedBy, model.RelationHasHome, model.RelationHomeOf, model.RelationMemberOf, model.RelationHasMember:
			continue
		default:
			relations = append(relations, relation)
		}
	}
	return relations
}

func (m tuiModel) updateQuickLink(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok || m.quickLink == nil {
		return m, nil
	}
	items := len(m.quickLink.targets)
	if m.view == "link-relations" {
		items = len(m.quickLink.relations)
	}
	if items > 0 {
		switch key.String() {
		case "up", "k":
			if m.quickLink.selected > 0 {
				m.quickLink.selected--
			}
		case "down", "j":
			if m.quickLink.selected < items-1 {
				m.quickLink.selected++
			}
		case "home":
			m.quickLink.selected = 0
		case "G", "end":
			m.quickLink.selected = items - 1
		}
	}
	switch key.String() {
	case "enter":
		if m.view == "link-relations" {
			if len(m.quickLink.relations) == 0 {
				return m, nil
			}
			m.quickLink.relation = m.quickLink.relations[m.quickLink.selected]
			targets, err := m.quickLinkTargets(m.quickLink.relation)
			if err != nil {
				m.inputErr = err.Error()
				return m, nil
			}
			m.quickLink.targets = targets
			m.quickLink.selected = 0
			m.quickLink.marked = make(map[int]bool)
			m.quickLink.selectionTouched = false
			m.view = "link-picker"
			m.inputErr = ""
			return m, nil
		}
		if len(m.quickLink.targets) == 0 {
			return m, nil
		}
		return m.submitQuickLinks()
	case " ":
		if m.view == "link-picker" && len(m.quickLink.targets) > 0 {
			m.quickLink.selectionTouched = true
			if m.quickLink.marked == nil {
				m.quickLink.marked = make(map[int]bool)
			}
			if !m.quickLink.marked[m.quickLink.selected] && m.quickLink.targets[m.quickLink.selected].relation == model.RelationHasHome {
				for index, target := range m.quickLink.targets {
					if target.relation == model.RelationHasHome {
						delete(m.quickLink.marked, index)
					}
				}
			}
			if m.quickLink.marked[m.quickLink.selected] {
				delete(m.quickLink.marked, m.quickLink.selected)
			} else {
				m.quickLink.marked[m.quickLink.selected] = true
			}
		}
	case "r":
		if m.view == "link-picker" && m.detail.entity == nil {
			m.quickLink.relations = quickTicketRelations()
			m.quickLink.selected = 0
			m.quickLink.marked = make(map[int]bool)
			m.quickLink.selectionTouched = false
			m.view = "link-relations"
			m.inputErr = ""
		}
	case "a":
		return m.openAdvancedLinkInput(), nil
	case "b", "esc":
		return m.goBack(), nil
	}
	m.clampQuickLinkSelection()
	return m, nil
}

func (m *tuiModel) clampQuickLinkSelection() {
	if m.quickLink == nil {
		return
	}
	count := len(m.quickLink.targets)
	if m.view == "link-relations" {
		count = len(m.quickLink.relations)
	}
	if count == 0 {
		m.quickLink.selected = 0
		return
	}
	if m.quickLink.selected < 0 {
		m.quickLink.selected = 0
	}
	if m.quickLink.selected >= count {
		m.quickLink.selected = count - 1
	}
}

func (m tuiModel) submitQuickLinks() (tea.Model, tea.Cmd) {
	indices := make([]int, 0, len(m.quickLink.marked))
	for index, marked := range m.quickLink.marked {
		if marked && index >= 0 && index < len(m.quickLink.targets) {
			indices = append(indices, index)
		}
	}
	if len(indices) == 0 && !m.quickLink.selectionTouched {
		indices = []int{m.quickLink.selected}
	}
	if len(indices) == 0 {
		m.inputErr = "select at least one link target"
		return m, nil
	}
	sort.Ints(indices)

	projectTargets := 0
	for _, index := range indices {
		if m.quickLink.targets[index].relation == model.RelationHasHome {
			projectTargets++
		}
	}
	if projectTargets > 1 {
		m.inputErr = "select at most one project; groups can be selected together"
		m.setLinkNotice(m.detailRef(), "link failed: "+m.inputErr, true)
		return m, nil
	}

	req := missis.RequestContext{Actor: "tui"}

	items := make([]missis.LinkBatchItem, 0, len(indices))
	home := ""
	for _, index := range indices {
		target := m.quickLink.targets[index]
		source := target.source
		if target.relation == model.RelationHasHome {
			source = m.detail.summary.Ref
			var err error
			home, err = currentHomeProject(m.client, m.detail.summary.Ref)
			if err != nil {
				m.inputErr = err.Error()
				m.setLinkNotice(m.detailRef(), "link failed: "+m.inputErr, true)
				return m, nil
			}
		}
		item := missis.LinkBatchItem{Source: source, Relation: target.relation, Target: target.ref}
		if target.relation == model.RelationHasHome && home != "" && home != strings.TrimPrefix(target.ref, "project:") {
			item.MoveFrom = home
		}
		items = append(items, item)
	}
	result, err := m.client.ApplyLinkBatch(context.Background(), req, missis.LinkBatchOptions{Items: items})
	if err != nil {
		m.inputErr = err.Error()
		m.setLinkNotice(m.detailRef(), "link failed: "+m.inputErr, true)
		return m, nil
	}
	linked := len(result.Added)
	skipped := len(result.Skipped)
	lastRef := ""
	if len(indices) > 0 {
		last := m.quickLink.targets[indices[len(indices)-1]]
		lastRef = last.labelRef
		if lastRef == "" {
			lastRef = last.ref
		}
	}
	if linked == 0 && skipped == len(indices) && len(indices) > 1 {
		m.message = "already linked to selected targets"
	} else if len(indices) == 1 && linked == 0 && skipped == 1 {
		m.message = "already linked to " + lastRef
	} else if len(indices) == 1 {
		m.message = "linked " + lastRef
	} else {
		m.message = fmt.Sprintf("linked %d target(s)", linked)
		if skipped > 0 {
			m.message += fmt.Sprintf(" (%d already linked)", skipped)
		}
	}
	m.setLinkNotice(m.detailRef(), m.message+"; press R to inspect references", false)
	m.inputErr = ""
	m.quickLink = nil
	m.view = "detail"
	m.refreshDetail()
	if m.ticketListMode == "ownership" {
		m.refreshTicketMemberships()
	}
	return m, nil
}

func (m tuiModel) submitTitle() (tea.Model, tea.Cmd) {
	if m.detail == nil || m.detail.entity != nil {
		m.view = "detail"
		m.inputMode = ""
		m.input = ""
		m.cursor = 0
		m.inputErr = ""
		return m, nil
	}
	title := strings.TrimSpace(m.input)
	if title == "" {
		m.inputErr = "title is required"
		return m, nil
	}
	if err := setTicketTitle(m.client, m.detail.summary, title); err != nil {
		m.err = err
		m.view = "detail"
		m.inputMode = ""
		m.input = ""
		m.cursor = 0
		m.inputErr = ""
		return m, nil
	}
	m.detail.summary.Title = title
	m.refreshDetail()
	m.err = nil
	m.view = "detail"
	m.inputMode = ""
	m.input = ""
	m.cursor = 0
	m.inputErr = ""
	m.message = "title updated"
	return m, nil
}

func (m tuiModel) submitCreate() (tea.Model, tea.Cmd) {
	kind, id, title, err := parseCreateEntity(m.input)
	if err != nil {
		m.inputErr = err.Error()
		return m, nil
	}
	if _, err := m.client.NewEntity(context.Background(), missis.RequestContext{Actor: "tui"}, missis.EntityOptions{Kind: kind, ID: id, Title: title}); err != nil {
		m.inputErr = err.Error()
		return m, nil
	}
	m.view = "list"
	m.inputMode = ""
	m.input = ""
	m.cursor = 0
	m.inputErr = ""
	m.message = "created " + kind + ":" + id
	m.refresh()
	return m, nil
}

func (m tuiModel) submitCreateTicket() (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(m.input)
	if !validVisibleTitle(title) {
		m.inputErr = "title must contain at least one visible character"
		return m, nil
	}
	result, err := m.client.NewTicket(context.Background(), missis.RequestContext{Actor: "tui"}, missis.NewTicketOptions{Title: title})
	if err != nil {
		m.inputErr = err.Error()
		return m, nil
	}
	m.view = "list"
	m.inputMode = ""
	m.input = ""
	m.cursor = 0
	m.inputErr = ""
	m.message = "created " + result.Ref
	m.refresh()
	return m, nil
}

func (m *tuiModel) setLinkInputError(text string) {
	m.inputErr = text
	m.setLinkNotice(m.detailRef(), "link failed: "+text, true)
}

func (m tuiModel) submitLinkAction() (tea.Model, tea.Cmd) {
	act, err := parseLinkAction(m.input)
	if err != nil {
		m.setLinkInputError(err.Error())
		return m, nil
	}
	source := ""
	target := act.Target
	if m.detail != nil && m.detail.entity != nil {
		if strings.HasPrefix(m.detail.entity.Ref, "project:") {
			if act.Action == "move" || act.Relation != model.RelationContains {
				m.setLinkInputError("project links support contains:group:<id>; use l for the quick picker")
				return m, nil
			}
			if !strings.HasPrefix(target, "group:") {
				target = "group:" + target
			}
			source = target
			target = m.detail.entity.Ref
		} else {
			if act.Action == "move" {
				m.setLinkInputError("move applies to tickets only")
				return m, nil
			}
			if act.Relation != model.RelationContains && act.Relation != model.RelationGoverns {
				m.setLinkInputError("group links support contains:<project|ticket> and governs:<project>")
				return m, nil
			}
			source = m.detail.entity.Ref
		}
	} else if m.detail != nil {
		if act.Relation == model.RelationContains || act.Relation == model.RelationGoverns || act.Relation == model.RelationHasMember {
			m.setLinkInputError("membership relations on tickets are not supported; use l on a group or move project:<id>")
			return m, nil
		}
		source = m.detail.summary.Ref
	}
	if act.Action == "add" && source != "" {
		exists, checkErr := m.linkAlreadyExists(source, act.Relation, target)
		if checkErr != nil {
			m.setLinkInputError(checkErr.Error())
			return m, nil
		}
		if exists {
			m.message = "already linked"
			m.setLinkNotice(m.detailRef(), m.message+"; press R to inspect references", false)
			m.refreshDetail()
			m.err = nil
			m.view = "detail"
			m.inputMode = ""
			m.input = ""
			m.cursor = 0
			m.inputErr = ""
			return m, nil
		}
	}
	req := missis.RequestContext{Actor: "tui"}
	switch act.Action {
	case "add", "retract":
		_, err = m.client.SetLink(context.Background(), req, missis.LinkOptions{
			Ref:      source + "/links",
			Relation: act.Relation,
			Target:   target,
			Add:      act.Action == "add",
			Retract:  act.Action == "retract",
			Reason:   act.Reason,
		})
	case "move":
		home, homeErr := currentHomeProject(m.client, m.detail.summary.Ref)
		if homeErr != nil {
			m.setLinkInputError(homeErr.Error())
			return m, nil
		}
		if home == "" {
			m.setLinkInputError("ticket has no home project; nothing to move")
			return m, nil
		}
		_, err = m.client.MoveHome(context.Background(), req, m.detail.summary.Ref, home, strings.TrimPrefix(act.Target, "project:"), act.Reason)
	}
	if err != nil {
		m.inputErr = err.Error()
		m.setLinkNotice(m.detailRef(), "link failed: "+err.Error(), true)
		if strings.Contains(err.Error(), "re-read and retry") || strings.Contains(err.Error(), "conflict") {
			m.refreshDetail()
		}
		return m, nil
	}
	m.message = "link updated"
	m.setLinkNotice(m.detailRef(), m.message+"; press R to inspect references", false)
	m.refreshDetail()
	if m.ticketListMode == "ownership" {
		m.refreshTicketMemberships()
	}
	m.err = nil
	m.view = "detail"
	m.inputMode = ""
	m.input = ""
	m.cursor = 0
	m.inputErr = ""
	return m, nil
}

func (m tuiModel) updateContext(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	rows := m.contextRows()
	m.clampContextWindow()
	switch key.String() {
	case "up", "k":
		if m.ctxSelected > 0 {
			m.ctxSelected--
		}
	case "down", "j":
		if m.ctxSelected < len(rows)-1 {
			m.ctxSelected++
		}
	case "pgup":
		m.ctxSelected -= m.contextWindowRows()
		if m.ctxSelected < 0 {
			m.ctxSelected = 0
		}
	case "pgdown":
		m.ctxSelected += m.contextWindowRows()
		if m.ctxSelected >= len(rows) {
			m.ctxSelected = len(rows) - 1
		}
	case "home":
		m.ctxSelected = 0
	case "G", "end":
		m.ctxSelected = len(rows) - 1
	case "t", "p", "g":
		updated, _ := m.switchListForKey(key.String())
		return updated, nil
	case "r":
		return m.refreshContextPicker(), nil
	case "b", "esc":
		return m.goBack(), nil
	case " ":
		row := rows[m.ctxSelected]
		switch row.kind {
		case "all":
			m.draftScope = scopeSelection{}
			m.message = "all-ticket draft selected; enter applies"
		case "unscoped":
			m.draftScope = scopeSelection{Unscoped: true}
			m.message = "unscoped draft selected; enter applies"
		case "project", "group":
			m.draftScope.toggle(row.kind, row.ref)
			m.message = "draft scope changed; enter applies"
		}
		m.refreshDraftCount()
	case "n":
		m.draftScope = scopeSelection{}
		m.message = "clean draft started; select scopes and press enter"
		m.refreshDraftCount()
	case "c":
		m.draftScope = scopeSelection{}
		m.message = "draft scope cleared; enter applies"
		m.refreshDraftCount()
	case "enter":
		row := rows[m.ctxSelected]
		switch row.kind {
		case "create-project":
			m.inputMode = "create"
			m.kind = "projects"
			m.input = ""
			m.cursor = 0
			m.inputErr = ""
			m.view = "input"
		case "create-group":
			m.inputMode = "create"
			m.kind = "groups"
			m.input = ""
			m.cursor = 0
			m.inputErr = ""
			m.view = "input"
		default:
			return m.applyContextSelection(m.draftScope), nil
		}
	}
	m.clampContextWindow()
	return m, nil
}

func (m tuiModel) updateCompare(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.compareOffset > 0 {
			m.compareOffset--
		}
	case "down", "j":
		if m.compareOffset < m.compareMaxOffset() {
			m.compareOffset++
		}
	case "pgup":
		m.compareOffset -= m.compareWindowRows()
		m.clampCompareOffset()
	case "pgdown":
		m.compareOffset += m.compareWindowRows()
		m.clampCompareOffset()
	case "home":
		m.compareOffset = 0
	case "G", "end":
		m.compareOffset = m.compareMaxOffset()
	case "t", "p", "g":
		updated, _ := m.switchListForKey(key.String())
		return updated, nil
	case "b", "esc":
		m = m.goBack()
	}
	return m, nil
}

func (m tuiModel) compareMaxOffset() int {
	max := len(m.compareLines()) - m.compareWindowRows()
	if max < 0 {
		return 0
	}
	return max
}

func (m tuiModel) updateHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	lines := m.helpContent()
	maxOffset := len(lines) - m.helpWindowRows()
	if maxOffset < 0 {
		maxOffset = 0
	}
	switch key.String() {
	case "up", "k":
		if m.helpOffset > 0 {
			m.helpOffset--
		}
	case "down", "j":
		if m.helpOffset < maxOffset {
			m.helpOffset++
		}
	case "home":
		m.helpOffset = 0
	case "G", "end":
		m.helpOffset = maxOffset
	case "t", "p", "g":
		updated, _ := m.switchListForKey(key.String())
		return updated, nil
	case "b", "esc":
		return m.goBack(), nil
	}
	return m, nil
}

func (m tuiModel) updateStats(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	lines := m.statsLines()
	maxOffset := len(lines) - m.statsWindowRows()
	if maxOffset < 0 {
		maxOffset = 0
	}
	switch key.String() {
	case "up", "k":
		if m.statsOffset > 0 {
			m.statsOffset--
		}
	case "down", "j":
		if m.statsOffset < maxOffset {
			m.statsOffset++
		}
	case "pgup":
		m.statsOffset -= m.statsWindowRows()
		m.clampStatsOffset()
	case "pgdown":
		m.statsOffset += m.statsWindowRows()
		m.clampStatsOffset()
	case "home":
		m.statsOffset = 0
	case "G", "end":
		m.statsOffset = maxOffset
	case "r":
		m.refreshTicketMemberships()
		m.statsOffset = 0
		if m.ticketMembershipErr == nil {
			m.message = "stats refreshed"
		}
	case "t", "p", "g":
		updated, _ := m.switchListForKey(key.String())
		return updated, nil
	case "b", "esc":
		m = m.goBack()
	}
	return m, nil
}

func (m tuiModel) View() string {
	if m.err != nil {
		hint := "press q to quit"
		switch m.view {
		case "detail", "compare", "stats":
			hint = "q quit | b back"
		case "input", "context":
			hint = "esc back"
		}
		return errorStyle.Render(m.err.Error()) + "\n" + hint
	}
	// On very small terminals the transient message is dropped so the help
	// bar and content never overflow the viewport.
	if height, _ := m.effectiveSize(); height < 8 {
		m.message = ""
	}
	var body string
	switch m.view {
	case "list":
		body = m.viewList()
	case "detail":
		body = m.viewDetail()
	case "compare":
		body = m.viewCompare()
	case "stats":
		body = m.viewStats()
	case "input":
		switch m.inputMode {
		case "create":
			kind := strings.TrimSuffix(m.kind, "s")
			example := kind + ":blog Blog"
			if kind == "group" {
				example = "group:eng Engineering"
			}
			body = renderInput(fmt.Sprintf("Create %s (%s:<id> <Title>, e.g. %s): ", kind, kind, example), m.input, m.cursor)
		case "create-ticket":
			body = renderInput("Create ticket (<Title>): ", m.input, m.cursor)
		case "link":
			width, _ := m.effectiveSize()
			var parts []string
			if m.detail != nil && m.detail.entity != nil && strings.HasPrefix(m.detail.entity.Ref, "group:") {
				body = renderInput(fmt.Sprintf("Group %s link (add|retract contains:<project|ticket> [reason], governs:<project> [reason]): ", m.inputRef()), m.input, m.cursor)
				parts = []string{"relations:", "contains:<project|ticket>", "governs:<project>"}
			} else {
				body = renderInput(fmt.Sprintf("Ticket %s link (add|retract relation:ref [reason], move project:<id> [reason]): ", m.inputRef()), m.input, m.cursor)
				parts = append([]string{"relations:"}, ticketRelationHints()...)
			}
			body += "\n" + helpStyle.Render(strings.Join(wrapParts(parts, width, " · "), "\n"))
		default:
			body = renderInput(fmt.Sprintf("Edit title %s: ", m.inputRef()), m.input, m.cursor)
		}
	case "link-picker", "link-relations":
		body = m.viewQuickLink()
	case "context":
		body = m.viewContext()
	case "help":
		body = m.viewHelp()
	default:
		body = "unknown view"
	}
	if m.inputErr != "" {
		body += "\n" + errorStyle.Render(m.inputErr)
	}
	help := helpStyle.Render(strings.Join(m.helpLines(), "\n"))
	if m.message != "" {
		help += "\n" + m.message
	}
	if m.shouldRenderLinkNotice() {
		notice, _ := m.visibleLinkNotice()
		style := helpStyle
		if m.linkNoticeError {
			style = errorStyle
		}
		help += "\n" + style.Render(notice)
	}
	return lipgloss.JoinVertical(lipgloss.Top, body, help)
}

type keyHint struct {
	key    string
	action string
}

// keyHints returns the shortcuts that actually do something in the current
// view and kind. It is the single source for the help bar, so a hint is never
// shown for a key that is a no-op on the current page.
func (m tuiModel) keyHints() []keyHint {
	switch m.view {
	case "list":
		hints := []keyHint{{"j/k", "move"}, {"enter", "open"}}
		if m.isEntityList() {
			hints = append(hints, keyHint{"n", "create " + strings.TrimSuffix(m.kind, "s")}, keyHint{"f", "filter tickets"})
		} else {
			hints = append(hints,
				keyHint{"n", "create ticket"},
				keyHint{"c/v", "compare"},
				keyHint{"e", "export"},
				keyHint{"s", "stats"},
				keyHint{"m", "ownership mode"},
			)
		}
		hints = append(hints,
			keyHint{"t/p/g", "lists"},
			keyHint{"r", "refresh"},
			keyHint{"x", "ticket filters"},
			keyHint{"q", "quit"},
		)
		return hints
	case "detail":
		hints := []keyHint{{"j/k", "scroll"}, {"pgup/pgdn", "page"}, {"home/G", "top/end"}, {"t/p/g", "lists"}}
		if m.detail != nil && m.detail.entity == nil {
			hints = append(hints, keyHint{"T", "edit title"}, keyHint{"l", "quick link"}, keyHint{"e", "export"})
		} else if m.detail != nil {
			hints = append(hints, keyHint{"f", "filter tickets"}, keyHint{"l", "quick link"})
		}
		hints = append(hints, keyHint{"r", "refresh"}, keyHint{"R", "refs"}, keyHint{"b", "back"})
		return hints
	case "stats":
		return []keyHint{{"j/k", "scroll"}, {"pgup/pgdn", "page"}, {"home/G", "top/end"}, {"r", "refresh"}, {"t/p/g", "lists"}, {"b", "back"}}
	case "compare":
		return []keyHint{{"j/k", "scroll"}, {"pgup/pgdn", "page"}, {"home/G", "top/end"}, {"t/p/g", "lists"}, {"b", "back"}}
	case "input", "context", "link-picker", "link-relations":
		if m.view == "input" {
			return []keyHint{{"enter", "save"}, {"esc", "cancel"}, {"←/→", "cursor"}, {"home/end", "jump"}, {"backspace", "delete"}}
		}
		if m.view == "link-picker" {
			hints := []keyHint{{"j/k", "choose"}, {"space", "select"}, {"enter", "apply"}, {"a", "advanced"}, {"b/esc", "cancel"}}
			if m.detail != nil && m.detail.entity == nil {
				hints = []keyHint{{"j/k", "choose"}, {"space", "select"}, {"enter", "apply"}, {"r", "ticket relation"}, {"a", "advanced"}, {"b/esc", "cancel"}}
			}
			return hints
		}
		if m.view == "link-relations" {
			return []keyHint{{"j/k", "choose"}, {"enter", "next"}, {"a", "advanced"}, {"b/esc", "cancel"}}
		}
		return []keyHint{{"j/k", "move"}, {"pgup/pgdn", "page"}, {"home/G", "top/end"}, {"space", "toggle"}, {"enter", "apply"}, {"n", "clean"}, {"c", "clear"}, {"r", "refresh"}, {"t/p/g", "lists"}, {"b", "back"}}
	case "help":
		return []keyHint{{"j/k", "scroll"}, {"t/p/g", "lists"}, {"b", "back"}}
	default:
		return nil
	}
}

func (m tuiModel) isEntityList() bool {
	return m.kind == "projects" || m.kind == "groups"
}

func kindLabel(kind string) string {
	switch kind {
	case "projects":
		return "projects"
	case "groups":
		return "groups"
	default:
		return "tickets"
	}
}

func (m tuiModel) currentScope() scopeSelection {
	return scopeSelection{
		Projects: normalizeScopeList(m.activeScope.Projects...),
		Groups:   normalizeScopeList(m.activeScope.Groups...),
		Unscoped: m.activeScope.Unscoped,
	}
}

func (m tuiModel) scopeNoteFor(scope scopeSelection) string {
	if scope.Unscoped {
		return " (unscoped tickets)"
	}
	if scope.empty() {
		return " (all tickets)"
	}
	projectLabel := "none"
	projectName := "project"
	if len(scope.Projects) > 0 {
		projectLabel = strings.Join(scope.Projects, ",")
	}
	if len(scope.Projects) > 1 {
		projectName = "projects"
	}
	groupLabel := "none"
	groupName := "group"
	if len(scope.Groups) > 0 {
		groupLabel = strings.Join(scope.Groups, ",")
	}
	if len(scope.Groups) > 1 {
		groupName = "groups"
	}
	return fmt.Sprintf(" (%s: %s · %s: %s)", projectName, projectLabel, groupName, groupLabel)
}

// scopeNote names the ticket-list scope, including all active projects and
// groups. It is only meaningful on the tickets list and stats, which are the
// views the context filters.
func (m tuiModel) scopeNote() string {
	return m.scopeNoteFor(m.currentScope())
}

// ticketsEmptyLine names the empty tickets-list state in the same terms as
// the scope shown in the breadcrumb, so an empty scoped list is not mistaken
// for an empty store.
func (m tuiModel) ticketsEmptyLine() string {
	scope := m.currentScope()
	if scope.empty() {
		return "no tickets yet"
	}
	if scope.Unscoped {
		return "no unscoped tickets yet"
	}
	if len(scope.Projects) == 1 && len(scope.Groups) == 0 {
		return "no tickets in project: " + scope.Projects[0] + " yet"
	}
	if len(scope.Groups) == 1 && len(scope.Projects) == 0 {
		return "no tickets in group: " + scope.Groups[0] + " yet"
	}
	return "no tickets matching" + m.scopeNoteFor(scope) + " yet"
}

func (m tuiModel) detailRef() string {
	if m.detail == nil {
		return ""
	}
	if m.detail.entity != nil {
		return m.detail.entity.Ref
	}
	return m.detail.summary.Ref
}

func (m tuiModel) visibleLinkNotice() (string, bool) {
	if m.linkNotice == "" {
		return "", false
	}
	if m.view == "list" {
		return m.linkNotice, true
	}
	if m.detailRef() == "" || m.detailRef() != m.linkNoticeRef {
		return "", false
	}
	return m.linkNotice, true
}

func (m tuiModel) shouldRenderLinkNotice() bool {
	notice, ok := m.visibleLinkNotice()
	if !ok {
		return false
	}
	return notice != m.message && notice != "link failed: "+m.inputErr
}

func (m *tuiModel) setLinkNotice(ref, text string, failed bool) {
	m.linkNoticeRef = ref
	m.linkNotice = text
	m.linkNoticeError = failed
}

func (m *tuiModel) clearLinkNotice() {
	m.linkNotice = ""
	m.linkNoticeRef = ""
	m.linkNoticeError = false
}

func (m *tuiModel) clearLinkNoticeIfDifferent(ref string) {
	if m.linkNoticeRef != "" && m.linkNoticeRef != ref {
		m.clearLinkNotice()
	}
}

// inputRef names the ticket an input prompt applies to, falling back to a
// generic label when no detail is open.
func (m tuiModel) inputRef() string {
	if ref := m.detailRef(); ref != "" {
		return ref
	}
	return "ticket"
}

func (m tuiModel) breadcrumb() string {
	switch m.view {
	case "list":
		crumb := "missis / " + kindLabel(m.kind)
		if m.kind == "tickets" {
			crumb += m.scopeNote()
			if m.ticketListMode == "ownership" {
				crumb += " · ownership"
			}
		}
		return crumb
	case "detail":
		if m.detail == nil {
			return "missis / " + kindLabel(m.kind)
		}
		title := ""
		if m.detail.entity != nil {
			title = m.detail.entity.Title
		} else {
			title = m.detail.summary.Title
		}
		if title == "" {
			title = "<no title>"
		}
		return "missis / " + kindLabel(m.kind) + " / " + m.detailRef() + " " + title
	case "compare":
		label := "compare"
		if m.compareA != nil && m.compareB != nil {
			label = "compare " + m.compareA.Ref + " vs " + m.compareB.Ref
		}
		return "missis / tickets / " + label
	case "stats":
		return "missis / tickets / stats" + m.scopeNote()
	case "input":
		switch m.inputMode {
		case "create":
			return "missis / " + kindLabel(m.kind) + " / create"
		case "create-ticket":
			return "missis / tickets / create"
		case "link":
			return "missis / tickets / " + m.inputRef() + " / links"
		default:
			return "missis / tickets / " + m.inputRef() + " / edit title"
		}
	case "link-picker", "link-relations":
		return "missis / " + kindLabel(m.kind) + " / " + m.detailRef() + " / quick link"
	case "context":
		return "missis / ticket filters"
	case "help":
		return "missis / help"
	default:
		return "missis"
	}
}

type contextRow struct {
	label    string
	kind     string // "all", "unscoped", "project", "group", "create-project", "create-group"
	ref      string // bare id for project/group rows
	countKey string
}

func contextCountKey(scope scopeSelection) string {
	if scope.Unscoped {
		return "unscoped"
	}
	if scope.empty() {
		return "all"
	}
	if len(scope.Projects) == 1 && len(scope.Groups) == 0 {
		return "project:" + scope.Projects[0]
	}
	if len(scope.Groups) == 1 && len(scope.Projects) == 0 {
		return "group:" + scope.Groups[0]
	}
	return "draft"
}

func (m tuiModel) contextCountLabel(key string) string {
	if err := m.contextCountErr[key]; err != nil {
		return "?"
	}
	if count, ok := m.contextCounts[key]; ok {
		return fmt.Sprintf("%d", count)
	}
	return "?"
}

// contextRows lists explicit all/unscoped choices, every existing project and
// group, and the create actions. Counts are loaded when the picker opens or
// refreshes; direct test callers may still obtain the entity rows lazily.
func (m tuiModel) contextRows() []contextRow {
	rows := []contextRow{
		{label: "(all tickets)", kind: "all", countKey: "all"},
		{label: "(unscoped tickets)", kind: "unscoped", countKey: "unscoped"},
	}
	projects := m.contextProjects
	groups := m.contextGroups
	var loadErrors []string
	if m.client != nil && !m.contextLoaded {
		now := time.Now().UTC()
		if projects, err := m.client.ListEntities(context.Background(), model.KindProject, missis.ListFilter{EffectiveAt: now, KnownAt: now}); err == nil {
			m.contextProjects = projects
		} else {
			loadErrors = append(loadErrors, "projects unavailable")
		}
		if groups, err := m.client.ListEntities(context.Background(), model.KindGroup, missis.ListFilter{EffectiveAt: now, KnownAt: now}); err == nil {
			m.contextGroups = groups
		} else {
			loadErrors = append(loadErrors, "groups unavailable")
		}
		projects = m.contextProjects
		groups = m.contextGroups
	}
	for _, p := range projects {
		_, id, _ := strings.Cut(p.Ref, ":")
		rows = append(rows, contextRow{label: p.Ref, kind: "project", ref: id, countKey: "project:" + id})
	}
	for _, g := range groups {
		_, id, _ := strings.Cut(g.Ref, ":")
		rows = append(rows, contextRow{label: g.Ref, kind: "group", ref: id, countKey: "group:" + id})
	}
	for _, label := range loadErrors {
		rows = append(rows, contextRow{label: "(" + label + ")", kind: "unavailable"})
	}
	rows = append(rows,
		contextRow{label: "create project…", kind: "create-project"},
		contextRow{label: "create group…", kind: "create-group"},
	)
	return rows
}

// refreshContextPicker re-reads the available scope rows without changing
// the active or draft selections, then keeps the cursor and window valid for
// the refreshed result.
func (m tuiModel) refreshContextPicker() tuiModel {
	now := time.Now().UTC()
	m.contextEffectiveAt = now
	m.contextLoaded = true
	m.contextProjects = nil
	m.contextGroups = nil
	refreshErr := ""
	if m.client != nil {
		projects, projectErr := m.client.ListEntities(context.Background(), model.KindProject, missis.ListFilter{EffectiveAt: now, KnownAt: now})
		groups, groupErr := m.client.ListEntities(context.Background(), model.KindGroup, missis.ListFilter{EffectiveAt: now, KnownAt: now})
		if projectErr == nil {
			m.contextProjects = projects
		}
		if groupErr == nil {
			m.contextGroups = groups
		}
		if projectErr != nil {
			refreshErr = "project scope refresh failed: " + projectErr.Error()
		} else if groupErr != nil {
			refreshErr = "group scope refresh failed: " + groupErr.Error()
		}
	}
	m.contextCounts = make(map[string]int)
	m.contextCountErr = make(map[string]error)
	for _, scope := range []scopeSelection{{}, {Unscoped: true}} {
		m.refreshContextCount(scope)
	}
	for _, project := range m.contextProjects {
		_, id, _ := strings.Cut(project.Ref, ":")
		m.refreshContextCount(scopeSelection{Projects: []string{id}})
	}
	for _, group := range m.contextGroups {
		_, id, _ := strings.Cut(group.Ref, ":")
		m.refreshContextCount(scopeSelection{Groups: []string{id}})
	}
	m.refreshDraftCount()
	rows := m.contextRows()
	m.clampContextWindowForRows(rows)
	if refreshErr != "" {
		m.message = refreshErr
	} else if len(m.contextCountErr) > 0 || !m.draftCountReady {
		m.message = "scope counts unavailable"
	} else {
		m.message = "scope list refreshed"
	}
	return m
}

func (m *tuiModel) refreshContextCount(scope scopeSelection) {
	if m.client == nil {
		return
	}
	filter := missis.ListFilter{EffectiveAt: m.contextEffectiveAt, KnownAt: m.contextEffectiveAt, Unscoped: scope.Unscoped}
	filter.Projects = append(filter.Projects, scope.Projects...)
	filter.Groups = append(filter.Groups, scope.Groups...)
	count, err := m.client.CountTicketsFiltered(context.Background(), filter)
	key := contextCountKey(scope)
	if err != nil {
		m.contextCountErr[key] = err
		return
	}
	m.contextCounts[key] = count
}

func (m *tuiModel) refreshDraftCount() {
	if m.client == nil {
		return
	}
	if m.contextEffectiveAt.IsZero() {
		m.contextEffectiveAt = time.Now().UTC()
	}
	filter := missis.ListFilter{EffectiveAt: m.contextEffectiveAt, KnownAt: m.contextEffectiveAt, Unscoped: m.draftScope.Unscoped}
	filter.Projects = append(filter.Projects, m.draftScope.Projects...)
	filter.Groups = append(filter.Groups, m.draftScope.Groups...)
	m.draftCount, m.draftCountErr = m.client.CountTicketsFiltered(context.Background(), filter)
	m.draftCountReady = m.draftCountErr == nil
}

func (m tuiModel) currentContextRowIndex() int {
	rows := m.contextRows()
	scope := m.draftScope
	for i, row := range rows {
		if scope.empty() && row.kind == "all" {
			return i
		}
		if scope.Unscoped && row.kind == "unscoped" {
			return i
		}
		if (row.kind == "project" || row.kind == "group") && scope.contains(row.kind, row.ref) {
			return i
		}
	}
	return 0
}

func (m tuiModel) viewContext() string {
	width, _ := m.effectiveSize()
	var b strings.Builder
	b.WriteString(titleStyle.Render(truncateCell(m.breadcrumb(), width)))
	b.WriteString("\n\n")
	rows := m.contextRows()
	selected := m.ctxSelected
	if selected < 0 {
		selected = 0
	}
	if selected >= len(rows) {
		selected = len(rows) - 1
	}
	b.WriteString("Active: " + m.scopeConfirmation(m.currentScope()) + "\n")
	b.WriteString("Draft:  " + m.scopeConfirmation(m.draftScope) + "\n")
	if m.draftCountReady {
		b.WriteString(fmt.Sprintf("Draft matches: %d\n", m.draftCount))
	} else {
		b.WriteString("Draft matches: ?\n")
	}
	if len(rows) > 0 {
		b.WriteString(fmt.Sprintf("Select ticket-list scope (%d/%d):\n", selected+1, len(rows)))
	} else {
		b.WriteString("Select ticket-list scope:\n")
	}
	visible := m.contextWindowRows()
	start, end := visibleRange(m.ctxOffset, len(rows), visible)
	for i := start; i < end; i++ {
		row := rows[i]
		cursor := "  "
		if i == selected {
			cursor = "> "
		}
		mark := "  "
		if row.kind == "all" {
			if m.draftScope.empty() {
				mark = "✓ "
			}
		} else if row.kind == "unscoped" {
			if m.draftScope.Unscoped {
				mark = "✓ "
			}
		} else if row.kind == "project" || row.kind == "group" {
			if m.draftScope.contains(row.kind, row.ref) {
				mark = "✓ "
			} else {
				mark = "· "
			}
		}
		label := row.label
		if row.countKey != "" {
			label += " — " + m.contextCountLabel(row.countKey)
		}
		b.WriteString(cursor + mark + label + "\n")
	}
	return b.String()
}

func (m tuiModel) viewQuickLink() string {
	width, height := m.effectiveSize()
	var b strings.Builder
	b.WriteString(titleStyle.Render(truncateCell(m.breadcrumb(), width)))
	b.WriteString("\n\n")
	if m.quickLink == nil {
		return b.String()
	}

	var lines []string
	if m.view == "link-relations" {
		b.WriteString("Choose a ticket relation:\n\n")
		lines = m.quickLink.relations
	} else {
		b.WriteString("Choose link targets (space selects multiple):\n\n")
		lines = make([]string, 0, len(m.quickLink.targets))
		for _, target := range m.quickLink.targets {
			ref := target.labelRef
			if ref == "" {
				ref = target.ref
			}
			label := ref
			if target.title != "" {
				label += " — " + target.title
			}
			if target.hint != "" {
				label += " (" + target.hint + ")"
			}
			lines = append(lines, label)
		}
	}
	if len(lines) == 0 {
		b.WriteString("  no available targets\n")
		return b.String()
	}

	available := height - 5 - m.helpRows()
	if available < 1 {
		available = 1
	}
	start := m.quickLink.selected - available + 1
	if start < 0 {
		start = 0
	}
	end := start + available
	if end > len(lines) {
		end = len(lines)
	}
	for i := start; i < end; i++ {
		cursor := "  "
		if i == m.quickLink.selected {
			cursor = "> "
		}
		mark := ""
		availableWidth := width - 2
		if m.view == "link-picker" {
			mark = "· "
			if m.quickLink.marked[i] {
				mark = "✓ "
			}
			availableWidth = width - 4
		}
		if availableWidth < 1 {
			availableWidth = 1
		}
		b.WriteString(cursor + mark + truncateCell(lines[i], availableWidth) + "\n")
	}
	return b.String()
}

// helpContent builds the cheatsheet from the same keyHints used by the help
// bar, so the in-app reference never drifts from the actual keybindings.
func (m tuiModel) helpContent() []string {
	width, _ := m.effectiveSize()
	if width < 20 {
		width = 20
	}
	lines := []string{titleStyle.Render(truncateCell(m.breadcrumb(), width)), ""}
	lines = append(lines, wrapParts([]string{
		"global: q quit regular views",
		"input types q",
		"ticket filters q inert",
		"ctrl+c quit",
		"b back (esc alias)",
		"? help regular views",
		"input types ?",
		"ticket filters/help ? inert",
	}, width, " · ")...)
	sections := []struct {
		title string
		hints []keyHint
	}{
		{"tickets list", tuiModel{view: "list", kind: "tickets"}.keyHints()},
		{"projects/groups list", tuiModel{view: "list", kind: "projects"}.keyHints()},
		{"ticket detail", tuiModel{view: "detail", detail: &detailState{summary: missis.TicketSummary{Ref: "#N"}}}.keyHints()},
		{"project/group detail", tuiModel{view: "detail", detail: &detailState{entity: &missis.EntitySummary{Ref: "project:x"}}}.keyHints()},
		{"quick link picker", tuiModel{view: "link-picker", detail: &detailState{summary: missis.TicketSummary{Ref: "#N"}}}.keyHints()},
		{"ticket relation picker", tuiModel{view: "link-relations", detail: &detailState{summary: missis.TicketSummary{Ref: "#N"}}}.keyHints()},
		{"compare", tuiModel{view: "compare"}.keyHints()},
		{"stats", tuiModel{view: "stats"}.keyHints()},
		{"input prompts", tuiModel{view: "input"}.keyHints()},
		{"ticket-filter picker", tuiModel{view: "context"}.keyHints()},
	}
	for _, section := range sections {
		lines = append(lines, "", section.title)
		parts := make([]string, 0, len(section.hints))
		for _, h := range section.hints {
			parts = append(parts, h.key+" "+h.action)
		}
		lines = append(lines, wrapParts(parts, width, " | ")...)
	}
	return lines
}

func (m tuiModel) helpWindowRows() int {
	_, height := m.effectiveSize()
	rows := height - 3 - m.helpRows()
	if rows < 0 {
		rows = 0
	}
	return rows
}

func (m tuiModel) viewHelp() string {
	lines := m.helpContent()
	start, end := visibleRange(m.helpOffset, len(lines), m.helpWindowRows())
	var b strings.Builder
	for _, line := range lines[start:end] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m tuiModel) viewList() string {
	width, _ := m.effectiveSize()
	var b strings.Builder
	b.WriteString(titleStyle.Render(truncateCell(m.breadcrumb(), width)))
	b.WriteString("\n\n")
	visible := m.listVisibleRows()
	start := m.listOffset
	end := start + visible
	count := len(m.summaries)
	if m.isEntityList() {
		count = len(m.entities)
	}
	if end > count {
		end = count
	}
	if m.isEntityList() {
		idWidth := m.entityIDWidth()
		membersWidth := 22
		titleWidth := width - idWidth - membersWidth - 4
		if titleWidth < 1 {
			titleWidth = 1
		}
		b.WriteString(fmt.Sprintf("  %-*s %-*s %s\n", idWidth, "ID", membersWidth, "MEMBERS", "TITLE"))
		if count == 0 {
			b.WriteString("  no " + m.kind + " yet\n")
		}
		for i := start; i < end; i++ {
			item := m.entities[i]
			ref := item.summary.Ref
			title := item.summary.Title
			if title == "" {
				title = "<no title>"
			}
			members := item.counts.label(m.kind)
			cursor := "  "
			if i == m.selected {
				cursor = "> "
			}
			b.WriteString(fmt.Sprintf("%s%-*s %-*s %s\n", cursor, idWidth, truncateCell(ref, idWidth), membersWidth, truncateCell(members, membersWidth), truncateCell(title, titleWidth)))
		}
		return b.String()
	}
	if m.ticketListMode == "ownership" {
		m.writeTicketOwnershipRows(&b, width, start, end, count)
		return b.String()
	}
	b.WriteString(fmt.Sprintf("  %-6s %-10s %s\n", "REF", "STATUS", "TITLE"))
	if count == 0 {
		b.WriteString("  " + m.ticketsEmptyLine() + "\n")
	}
	// cursor (2) + ref (6) + gap (1) + status (10) + gap (1)
	titleWidth := width - 20
	if titleWidth < 1 {
		titleWidth = 1
	}
	for i := start; i < end; i++ {
		ref, status, title := m.summaries[i].Ref, m.summaries[i].Status, m.summaries[i].Title
		ref = truncateCell(ref, 6)
		status = truncateCell(status, 10)
		if title == "" {
			title = "<no title>"
		}
		title = truncateCell(title, titleWidth)
		cursor := "  "
		if i == m.selected {
			cursor = "> "
		}
		line := fmt.Sprintf("%s%-6s %-10s %s", cursor, ref, status, title)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m tuiModel) writeTicketOwnershipRows(b *strings.Builder, width, start, end, count int) {
	projectWidth, groupWidth, statusWidth, titleWidth := ticketOwnershipWidths(width)
	b.WriteString(fmt.Sprintf("  %-6s %-*s %-*s %-*s %s\n",
		truncateCell("REF", 6),
		projectWidth, truncateCell("PROJECT", projectWidth),
		groupWidth, truncateCell("GROUP", groupWidth),
		statusWidth, truncateCell("STATUS", statusWidth),
		truncateCell("TITLE", titleWidth),
	))
	if count == 0 {
		b.WriteString("  " + m.ticketsEmptyLine() + "\n")
		return
	}
	for i := start; i < end; i++ {
		summary := m.summaries[i]
		membership, ok := m.ticketMemberships[ticketSummaryKey(summary)]
		project := "—"
		group := "—"
		if ok {
			project = strings.Join(membership.projects, ",")
			group = strings.Join(membership.groups, ",")
			if project == "" {
				project = "—"
			}
			if group == "" {
				group = "—"
			}
		}
		status := summary.Status
		if status == "" {
			status = "—"
		}
		title := summary.Title
		if title == "" {
			title = "<no title>"
		}
		cursor := "  "
		if i == m.selected {
			cursor = "> "
		}
		b.WriteString(fmt.Sprintf("%s%-6s %-*s %-*s %-*s %s\n",
			cursor,
			truncateCell(summary.Ref, 6),
			projectWidth, truncateCell(project, projectWidth),
			groupWidth, truncateCell(group, groupWidth),
			statusWidth, truncateCell(status, statusWidth),
			truncateCell(title, titleWidth),
		))
	}
}

func ticketOwnershipWidths(width int) (project, group, status, title int) {
	const refWidth = 6
	const separators = 4
	available := width - 2 - refWidth - separators
	if available < 4 {
		available = 4
	}
	status = available / 4
	if status > 8 {
		status = 8
	}
	if status < 1 {
		status = 1
	}
	remaining := available - status
	project = remaining / 3
	group = remaining / 3
	if project > 18 {
		project = 18
	}
	if group > 18 {
		group = 18
	}
	for project > 6 && group > 6 && remaining-project-group < 8 {
		project--
		group--
	}
	title = remaining - project - group
	if project < 1 {
		project = 1
	}
	if group < 1 {
		group = 1
	}
	if title < 1 {
		title = 1
	}
	return project, group, status, title
}

func (m tuiModel) entityIDWidth() int {
	width, _ := m.effectiveSize()
	maxLen := 10
	for _, e := range m.entities {
		if l := len([]rune(e.summary.Ref)); l > maxLen {
			maxLen = l
		}
	}
	if limit := width - 27; maxLen > limit {
		maxLen = limit
	}
	if maxLen < 10 {
		maxLen = 10
	}
	return maxLen
}

func (m *tuiModel) clampListOffset() {
	if m.listOffset < 0 {
		m.listOffset = 0
	}
	visible := m.listVisibleRows()
	count := len(m.summaries)
	if m.isEntityList() {
		count = len(m.entities)
	}
	maxOffset := count - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.listOffset > maxOffset {
		m.listOffset = maxOffset
	}
	if m.selected < m.listOffset {
		m.listOffset = m.selected
	}
	if m.selected >= m.listOffset+visible {
		m.listOffset = m.selected - visible + 1
	}
}

func (m tuiModel) effectiveSize() (width, height int) {
	width, height = m.width, m.height
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		height = defaultHeight
	}
	return width, height
}

// helpRows returns the number of rows the help bar plus any status message
// will occupy below the view body.
func (m tuiModel) helpRows() int {
	rows := len(m.helpLines())
	if m.message != "" {
		rows++
	}
	if m.shouldRenderLinkNotice() {
		rows++
	}
	return rows
}

// helpLines renders the key hints for the current view and kind, wrapping at
// the terminal width so no shortcut is clipped off-screen (the tickets-page
// hint line is longer than a typical terminal).
func (m tuiModel) helpLines() []string {
	width, _ := m.effectiveSize()
	if width < 20 {
		width = 20
	}
	hints := m.keyHints()
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts, h.key+" "+h.action)
	}
	return wrapParts(parts, width, " | ")
}

// wrapParts joins parts with sep and wraps at width, never splitting a part.
func wrapParts(parts []string, width int, sep string) []string {
	var lines []string
	current := ""
	for _, part := range parts {
		candidate := part
		if current != "" {
			candidate = current + sep + part
		}
		if current != "" && len(candidate) > width {
			lines = append(lines, current)
			current = part
		} else {
			current = candidate
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// ticketRelationHints lists the common relations a ticket can assert, with
// the target kind each expects. The service remains the final validator: the
// full built-in vocabulary is model.ValidRelations(), and per-ticket
// schema/links declarations may restrict targets further.
func ticketRelationHints() []string {
	return []string{
		"blocks:<ticket>",
		"caused-by:<ticket>",
		"duplicates:<ticket>",
		"supports:<ticket>",
		"contradicts:<ticket>",
		"implements:<ticket>",
		"tracks:<ticket>",
		"documents:<ticket>",
		"has-home:<project>",
	}
}

// listVisibleRows leaves room for the breadcrumb line, blank line, column
// header, the separator row before the help bar, and the help bar itself.
func (m tuiModel) listVisibleRows() int {
	_, height := m.effectiveSize()
	visible := height - 4 - m.helpRows()
	if visible < 0 {
		visible = 0
	}
	return visible
}

// contextWindowRows leaves room for the breadcrumb, blank line, active/draft
// summaries, draft count, picker heading, and help bar. Context rows are
// windowed so the picker remains usable in short terminals.
func (m tuiModel) contextWindowRows() int {
	_, height := m.effectiveSize()
	rows := height - 7 - m.helpRows()
	if rows < 0 {
		rows = 0
	}
	return rows
}

func (m *tuiModel) clampContextWindow() {
	m.clampContextWindowForRows(m.contextRows())
}

func (m *tuiModel) clampContextWindowForRows(rows []contextRow) {
	if len(rows) == 0 {
		m.ctxSelected = 0
		m.ctxOffset = 0
		return
	}
	if m.ctxSelected < 0 {
		m.ctxSelected = 0
	}
	if m.ctxSelected >= len(rows) {
		m.ctxSelected = len(rows) - 1
	}
	visible := m.contextWindowRows()
	if visible <= 0 {
		m.ctxOffset = m.ctxSelected
		return
	}
	maxOffset := len(rows) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.ctxOffset < 0 {
		m.ctxOffset = 0
	}
	if m.ctxOffset > maxOffset {
		m.ctxOffset = maxOffset
	}
	if m.ctxSelected < m.ctxOffset {
		m.ctxOffset = m.ctxSelected
	}
	if m.ctxSelected >= m.ctxOffset+visible {
		m.ctxOffset = m.ctxSelected - visible + 1
	}
	if m.ctxOffset > maxOffset {
		m.ctxOffset = maxOffset
	}
}

// detailWindowRows leaves room for the breadcrumb line, blank line, the
// "<no parts>" line when present, the separator row before the help bar, and
// the help bar itself.
func (m tuiModel) detailWindowRows() int {
	_, height := m.effectiveSize()
	rows := height - 3 - m.helpRows()
	if m.detail != nil && len(m.detail.lines) <= 1 && !m.detail.showRefs {
		rows--
	}
	if rows < 0 {
		rows = 0
	}
	return rows
}

// statsWindowRows leaves room for the stats header line, blank line, the
// separator row before the help bar, and the help bar itself.
func (m tuiModel) statsWindowRows() int {
	_, height := m.effectiveSize()
	rows := height - 3 - m.helpRows()
	if rows < 0 {
		rows = 0
	}
	return rows
}

// visibleRange returns the half-open [start, end) window into content of
// length `length` that fits in `available` rows, anchored at `offset`. The
// result always satisfies 0 <= start <= end <= length, regardless of inputs.
func visibleRange(offset, length, available int) (start, end int) {
	if available < 0 {
		available = 0
	}
	if length < 0 {
		length = 0
	}
	start = offset
	if start < 0 {
		start = 0
	}
	if start > length {
		start = length
	}
	end = start + available
	if end > length {
		end = length
	}
	if end < start {
		end = start
	}
	return start, end
}

func truncateCell(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func clampCursor(input string, cursor int) int {
	if cursor < 0 {
		return 0
	}
	if n := len([]rune(input)); cursor > n {
		return n
	}
	return cursor
}

// editInput applies cursor movement, backspace, and character insertion to
// input at cursor. It returns the updated input and cursor, and reports
// whether the text changed so callers can clear their validation error.
func editInput(input string, cursor int, key tea.KeyMsg) (string, int, bool) {
	switch key.String() {
	case "left":
		if cursor > 0 {
			cursor--
		}
		return input, cursor, false
	case "right":
		if cursor < len([]rune(input)) {
			cursor++
		}
		return input, cursor, false
	case "home":
		return input, 0, false
	case "end":
		return input, len([]rune(input)), false
	case "backspace":
		runes := []rune(input)
		if cursor > 0 {
			input = string(runes[:cursor-1]) + string(runes[cursor:])
			cursor--
			return input, cursor, true
		}
		return input, cursor, false
	default:
		if len([]rune(key.String())) == 1 && !strings.Contains(key.String(), "ctrl") {
			runes := []rune(input)
			r := []rune(key.String())
			input = string(runes[:cursor]) + string(r) + string(runes[cursor:])
			cursor++
			return input, cursor, true
		}
		return input, cursor, false
	}
}

// renderInput places the block cursor at the requested rune position instead
// of always at the end of the text.
func renderInput(prompt, input string, cursor int) string {
	cursor = clampCursor(input, cursor)
	runes := []rune(input)
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString(string(runes[:cursor]))
	b.WriteString("▌")
	b.WriteString(string(runes[cursor:]))
	return b.String()
}

func (m tuiModel) viewDetail() string {
	if m.detail == nil {
		return ""
	}
	var b strings.Builder
	label := m.breadcrumb()
	if m.detail.showRefs {
		label += " (references)"
	}
	b.WriteString(titleStyle.Render(truncateCell(label, m.renderWidth())))
	b.WriteString("\n\n")
	noParts := len(m.detail.lines) <= 1 && !m.detail.showRefs
	if noParts {
		b.WriteString("<no parts>\n")
	}
	rendered := m.renderedDetailLines()
	start, end := visibleRange(m.detail.offset, len(rendered), m.detailWindowRows())
	for _, line := range rendered[start:end] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *tuiModel) clampDetailOffset() {
	if m.detail == nil {
		return
	}
	if m.detail.offset < 0 {
		m.detail.offset = 0
	}
	rendered := m.renderedDetailLines()
	maxOffset := len(rendered) - m.detailWindowRows()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.detail.offset > maxOffset {
		m.detail.offset = maxOffset
	}
}

func (m tuiModel) renderedDetailLines() []string {
	if m.detail == nil {
		return nil
	}
	return wrapIndentedLines(m.detail.lines, m.renderWidth())
}

func (m tuiModel) renderWidth() int {
	width, _ := m.effectiveSize()
	width -= 2
	if width < 20 {
		width = 20
	}
	return width
}

func (m tuiModel) viewCompare() string {
	if m.compareA == nil || m.compareB == nil {
		return "select two tickets"
	}
	lines := m.compareLines()
	start, end := visibleRange(m.compareOffset, len(lines), m.compareWindowRows())
	var b strings.Builder
	for _, line := range lines[start:end] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m tuiModel) compareLines() []string {
	lines := []string{titleStyle.Render(truncateCell(m.breadcrumb(), m.renderWidth())), ""}
	titleA := m.compareA.Title
	if titleA == "" {
		titleA = "<no title>"
	}
	titleB := m.compareB.Title
	if titleB == "" {
		titleB = "<no title>"
	}
	lines = append(lines,
		fmt.Sprintf("A: %s  %s  %s", m.compareA.Ref, m.compareA.Status, titleA),
		fmt.Sprintf("B: %s  %s  %s", m.compareB.Ref, m.compareB.Status, titleB),
	)
	if m.compareA.ID == m.compareB.ID {
		lines = append(lines, "", "(same ticket)")
	}
	lines = append(lines, "")
	a := ticketSummaryParts(m.client, *m.compareA)
	bb := ticketSummaryParts(m.client, *m.compareB)
	paths := make(map[string]bool)
	for path := range a {
		paths[path] = true
	}
	for path := range bb {
		paths[path] = true
	}
	sortedPaths := make([]string, 0, len(paths))
	for path := range paths {
		sortedPaths = append(sortedPaths, path)
	}
	sort.Strings(sortedPaths)
	for _, path := range sortedPaths {
		lines = append(lines, path)
		for _, ln := range strings.Split(renderMarkdownValue(a[path]), "\n") {
			lines = append(lines, wrapLine("  A: "+ln, m.renderWidth())...)
		}
		for _, ln := range strings.Split(renderMarkdownValue(bb[path]), "\n") {
			lines = append(lines, wrapLine("  B: "+ln, m.renderWidth())...)
		}
	}
	return lines
}

func (m tuiModel) compareWindowRows() int {
	_, height := m.effectiveSize()
	rows := height - 3 - m.helpRows()
	if rows < 0 {
		rows = 0
	}
	return rows
}

func (m *tuiModel) clampCompareOffset() {
	if m.compareOffset < 0 {
		m.compareOffset = 0
	}
	lines := m.compareLines()
	maxOffset := len(lines) - m.compareWindowRows()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.compareOffset > maxOffset {
		m.compareOffset = maxOffset
	}
}

func loadTicketSummariesForScope(client *missis.Client, scope scopeSelection) ([]missis.TicketSummary, error) {
	scope = scopeSelection{
		Projects: normalizeScopeList(scope.Projects...),
		Groups:   normalizeScopeList(scope.Groups...),
		Unscoped: scope.Unscoped,
	}
	now := time.Now().UTC()
	if !scope.empty() {
		return client.ListTicketsFiltered(context.Background(), missis.ListFilter{
			Projects:    scope.Projects,
			Groups:      scope.Groups,
			Unscoped:    scope.Unscoped,
			EffectiveAt: now,
			KnownAt:     now,
		})
	}
	return client.ListTicketSummaries(context.Background(), now)
}

func ticketSummaryKey(summary missis.TicketSummary) string {
	if summary.ID != "" {
		return summary.ID
	}
	return summary.Ref
}

func (m tuiModel) scopeConfirmation(scope scopeSelection) string {
	if scope.Unscoped {
		return "unscoped tickets"
	}
	scope = newScopeSelection(scope.Projects, scope.Groups)
	if scope.empty() {
		return "all tickets"
	}
	return "project=" + scopeLabel(scope.Projects, "none") + " group=" + scopeLabel(scope.Groups, "none")
}

func (m tuiModel) openContext(draft scopeSelection) tuiModel {
	m.activeScope = m.currentScope()
	m.draftScope = scopeSelection{
		Projects: normalizeScopeList(draft.Projects...),
		Groups:   normalizeScopeList(draft.Groups...),
		Unscoped: draft.Unscoped,
	}
	m.contextPrevView = m.view
	m.view = "context"
	m = m.refreshContextPicker()
	m.ctxSelected = m.currentContextRowIndex()
	m.clampContextWindow()
	if strings.HasSuffix(m.message, "scope list refreshed") {
		m.message = ""
	}
	return m
}

func (m tuiModel) applyContextSelection(scope scopeSelection) tuiModel {
	scope = scopeSelection{
		Projects: normalizeScopeList(scope.Projects...),
		Groups:   normalizeScopeList(scope.Groups...),
		Unscoped: scope.Unscoped,
	}
	m.activeScope = scope
	m.draftScope = scope
	m.kind = "tickets"
	m.view = "list"
	m.contextPrevView = ""
	m.detail = nil
	m.input = ""
	m.cursor = 0
	m.inputErr = ""
	m.message = "ticket list context: " + m.scopeConfirmation(scope)
	m.refresh()
	return m
}

// switchListForKey handles the t/p/g list-switch keys from any non-input
// view: it maps the key to its list kind, reports "already on <kind>" when
// the requested list is already shown on the list view, and otherwise jumps
// to the requested list clearing subpage state.
func (m tuiModel) switchListForKey(key string) (tuiModel, bool) {
	var kind string
	switch key {
	case "t":
		kind = "tickets"
	case "p":
		kind = "projects"
	case "g":
		kind = "groups"
	default:
		return m, false
	}
	if m.view == "list" && m.kind == kind {
		m.message = "already on " + kind
		return m, true
	}
	if m.kind != kind {
		m.clearLinkNotice()
	}
	m.kind = kind
	m.view = "list"
	m.detail = nil
	m.compareA = nil
	m.compareB = nil
	m.message = ""
	m.refresh()
	return m, true
}

func (m *tuiModel) refresh() {
	if m.kind == "projects" || m.kind == "groups" {
		kind := model.KindProject
		if m.kind == "groups" {
			kind = model.KindGroup
		}
		entities, err := loadEntityItems(m.client, kind)
		if err != nil {
			m.message = "refresh failed: " + err.Error()
			return
		}
		selectedRef := ""
		if m.selected >= 0 && m.selected < len(m.entities) {
			selectedRef = m.entities[m.selected].summary.Ref
		}
		m.entities = entities
		m.summaries = nil
		m.compareA = nil
		m.compareB = nil
		m.selected = 0
		for i := range m.entities {
			if m.entities[i].summary.Ref == selectedRef {
				m.selected = i
				break
			}
		}
		if m.selected >= len(m.entities) {
			m.selected = maxInt(0, len(m.entities)-1)
		}
		m.clampListOffset()
		m.err = nil
		return
	}
	var summaries []missis.TicketSummary
	var err error
	summaries, err = loadTicketSummariesForScope(m.client, m.currentScope())
	if err != nil {
		m.message = "refresh failed: " + err.Error()
		return
	}
	selectedID := ""
	if m.selected >= 0 && m.selected < len(m.summaries) {
		selectedID = m.summaries[m.selected].ID
	}
	m.summaries = summaries
	if m.ticketListMode == "ownership" {
		m.refreshTicketMemberships()
	} else {
		m.ticketMemberships = nil
		m.ticketMembershipErr = nil
		m.ticketMembershipsLoaded = false
	}
	m.selected = 0
	for i := range m.summaries {
		if m.summaries[i].ID == selectedID {
			m.selected = i
			break
		}
	}
	if m.selected >= len(m.summaries) {
		m.selected = len(m.summaries) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	m.clampListOffset()
	// compareA/compareB hold pointers into the old slice; re-point them by ID
	// so a reload cannot leave them dangling.
	m.compareA = findSummary(m.summaries, m.compareA)
	m.compareB = findSummary(m.summaries, m.compareB)
	// Keep an open detail view live for the same ticket.
	if m.view == "detail" && m.detail != nil {
		if updated := findSummary(m.summaries, &m.detail.summary); updated != nil {
			m.detail.summary = *updated
			var lines []string
			var err error
			if m.detail.showRefs {
				lines, err = referenceLines(m.client, m.detail.summary)
			} else {
				lines, err = ticketLines(m.client, m.detail.summary, m.renderWidth())
			}
			if err == nil {
				m.detail.lines = lines
			}
			m.clampDetailOffset()
		} else {
			m.view = "list"
			m.detail = nil
		}
	}
	m.err = nil
}

func findSummary(summaries []missis.TicketSummary, want *missis.TicketSummary) *missis.TicketSummary {
	if want == nil {
		return nil
	}
	for i := range summaries {
		if summaries[i].ID == want.ID {
			return &summaries[i]
		}
	}
	return nil
}

func (m *tuiModel) refreshTicketMemberships() {
	m.ticketMemberships = make(map[string]ticketMembership, len(m.summaries))
	m.ticketMembershipErr = nil
	m.ticketMembershipsLoaded = false
	if m.client == nil || len(m.summaries) == 0 {
		m.ticketMembershipsLoaded = len(m.summaries) == 0
		return
	}
	memberships, err := loadTicketMemberships(m.client, m.summaries)
	if err != nil {
		m.ticketMembershipErr = err
		m.message = "ownership refresh failed: " + err.Error()
		return
	}
	m.ticketMemberships = memberships
	m.ticketMembershipsLoaded = true
}

func (m tuiModel) statsLines() []string {
	var lines []string
	lines = append(lines, titleStyle.Render(truncateCell(m.breadcrumb(), m.renderWidth())))
	lines = append(lines, "")
	if len(m.summaries) == 0 {
		lines = append(lines, "  "+m.ticketsEmptyLine())
		return lines
	}
	lines = append(lines, "status")
	statusOrder := []string{"open", "doing", "blocked", "done"}
	counts := make(map[string]int)
	for _, s := range m.summaries {
		st := s.Status
		if st == "" {
			st = "(none)"
		}
		counts[st]++
	}
	seen := make(map[string]bool)
	for _, st := range statusOrder {
		if counts[st] > 0 {
			lines = append(lines, fmt.Sprintf("  %-8s %d", st, counts[st]))
			seen[st] = true
		}
	}
	var rest []string
	for st := range counts {
		if !seen[st] {
			rest = append(rest, st)
		}
	}
	sort.Strings(rest)
	for _, st := range rest {
		lines = append(lines, fmt.Sprintf("  %-8s %d", st, counts[st]))
	}
	lines = append(lines, "")
	lines = append(lines, "age (since created)")
	type ageBucket struct {
		name string
		max  time.Duration
	}
	buckets := []ageBucket{
		{"<1d", 24 * time.Hour},
		{"1-7d", 7 * 24 * time.Hour},
		{"7-30d", 30 * 24 * time.Hour},
		{">30d", 0},
	}
	now := time.Now()
	ageCounts := make(map[string]int)
	for _, s := range m.summaries {
		age := now.Sub(s.RecordedAt)
		bucket := buckets[len(buckets)-1].name
		for _, bkt := range buckets {
			if bkt.max > 0 && age <= bkt.max {
				bucket = bkt.name
				break
			}
		}
		ageCounts[bucket]++
	}
	for _, bkt := range buckets {
		lines = append(lines, fmt.Sprintf("  %-8s %d", bkt.name, ageCounts[bkt.name]))
	}
	lines = appendTicketOwnershipStats(lines, m.summaries, m.ticketMemberships, m.ticketMembershipsLoaded)
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("total: %d", len(m.summaries)))
	return lines
}

func appendTicketOwnershipStats(lines []string, summaries []missis.TicketSummary, memberships map[string]ticketMembership, loaded bool) []string {
	lines = append(lines, "", "ownership")
	if !loaded {
		lines = append(lines, "  project/group data unavailable")
		return lines
	}
	projectCounts := make(map[string]int)
	groupCounts := make(map[string]int)
	for _, summary := range summaries {
		membership := memberships[ticketSummaryKey(summary)]
		if len(membership.projects) == 0 {
			projectCounts["(no project)"]++
		} else {
			for _, project := range membership.projects {
				projectCounts[project]++
			}
		}
		if len(membership.groups) == 0 {
			groupCounts["(no group)"]++
		} else {
			for _, group := range membership.groups {
				groupCounts[group]++
			}
		}
	}
	lines = appendOwnershipCounter(lines, "projects", projectCounts)
	lines = appendOwnershipCounter(lines, "groups", groupCounts)
	return lines
}

func appendOwnershipCounter(lines []string, label string, counts map[string]int) []string {
	lines = append(lines, "  "+label)
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		isNoneI := strings.HasPrefix(keys[i], "(no ")
		isNoneJ := strings.HasPrefix(keys[j], "(no ")
		if isNoneI != isNoneJ {
			return !isNoneI
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("    %-24s %d", key, counts[key]))
	}
	return lines
}

func (m tuiModel) viewStats() string {
	lines := m.statsLines()
	start, end := visibleRange(m.statsOffset, len(lines), m.statsWindowRows())
	var b strings.Builder
	for _, line := range lines[start:end] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *tuiModel) clampStatsOffset() {
	if m.statsOffset < 0 {
		m.statsOffset = 0
	}
	lines := m.statsLines()
	maxOffset := len(lines) - m.statsWindowRows()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.statsOffset > maxOffset {
		m.statsOffset = maxOffset
	}
}

func ticketLines(client *missis.Client, summary missis.TicketSummary, width int) ([]string, error) {
	now := time.Now().UTC()
	proj, err := client.ShowTicket(context.Background(), summary.Ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return nil, err
	}
	lines := []string{
		"status: " + summary.Status,
	}
	var roots []string
	for path := range proj.Parts {
		if path == "title" || path == "status" {
			continue
		}
		if strings.Contains(path, "/") {
			continue
		}
		roots = append(roots, path)
	}
	sort.Strings(roots)
	for _, path := range roots {
		lines = append(lines, partSubtree(proj, path, 0, width)...)
	}
	return lines, nil
}

func referenceLines(client *missis.Client, summary missis.TicketSummary) ([]string, error) {
	return referenceLinesForRef(client, summary.Ref)
}

func referenceLinesForRef(client *missis.Client, ref string) ([]string, error) {
	now := time.Now().UTC()
	links, err := client.ShowReferences(context.Background(), ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return []string{"<no references>"}, nil
	}
	lines := make([]string, 0, len(links))
	for _, link := range links {
		lines = append(lines, fmt.Sprintf("%s %s %s", link.Direction, link.Relation, link.To))
	}
	return lines, nil
}

func entityLines(client *missis.Client, ent missis.EntitySummary, width int) ([]string, error) {
	now := time.Now().UTC()
	proj, err := client.ShowEntity(context.Background(), ent.Ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return nil, err
	}
	kind := "projects"
	if strings.HasPrefix(ent.Ref, "group:") {
		kind = "groups"
	}
	counts, err := membershipCounts(client, ent.Ref)
	if err != nil {
		return nil, err
	}
	lines := []string{
		"title: " + proj.Title,
		"status: " + proj.Status,
		"recorded: " + proj.RecordedAt.UTC().Format(time.RFC3339),
		"members: " + counts.label(kind),
	}
	var roots []string
	for path := range proj.Parts {
		if path == "title" || path == "status" {
			continue
		}
		if strings.Contains(path, "/") {
			continue
		}
		roots = append(roots, path)
	}
	sort.Strings(roots)
	for _, path := range roots {
		lines = append(lines, partSubtree(proj, path, 0, width)...)
	}
	return lines, nil
}

func currentHomeProject(client *missis.Client, ticketRef string) (string, error) {
	now := time.Now().UTC()
	links, err := client.ShowReferences(context.Background(), ticketRef, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return "", err
	}
	for _, link := range links {
		if link.Relation == model.RelationHasHome && link.Direction == "asserted" {
			if project, ok := strings.CutPrefix(link.To, "project:"); ok {
				return project, nil
			}
			return link.To, nil
		}
	}
	return "", nil
}

func (m *tuiModel) refreshDetail() {
	if m.detail == nil {
		return
	}
	var lines []string
	var err error
	if m.detail.entity != nil {
		if m.detail.showRefs {
			lines, err = referenceLinesForRef(m.client, m.detail.entity.Ref)
		} else {
			lines, err = entityLines(m.client, *m.detail.entity, m.renderWidth())
		}
	} else if m.detail.showRefs {
		lines, err = referenceLinesForRef(m.client, m.detail.summary.Ref)
	} else {
		lines, err = ticketLines(m.client, m.detail.summary, m.renderWidth())
	}
	if err != nil {
		m.err = err
	} else {
		m.detail.lines = lines
		m.clampDetailOffset()
	}
}

func partSubtree(proj missis.TicketProjection, path string, depth int, width int) []string {
	part, ok := proj.Parts[path]
	if !ok {
		return nil
	}
	name := part.DisplayName
	if name == "" {
		name = part.Name
	}
	if part.ValueKind == "markdown" {
		name = part.Name
		if name == "" {
			name = part.DisplayName
		}
	}
	if name == "" {
		name = path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			name = path[idx+1:]
		}
	}
	indent := strings.Repeat("  ", depth)
	lines := []string{indent + "▸ " + name}
	if part.Value != nil {
		value := valueText(part.Value)
		if part.ValueKind == "markdown" {
			available := width - depth*2 - 3
			if available < 20 {
				available = 20
			}
			value = renderMarkdownBody(value, available)
		} else {
			value = renderMarkdownValue(value)
		}
		for _, valueLine := range strings.Split(value, "\n") {
			lines = append(lines, indent+"   "+valueLine)
		}
	}
	var children []string
	prefix := path + "/"
	for candidate := range proj.Parts {
		if !strings.HasPrefix(candidate, prefix) {
			continue
		}
		if strings.Contains(candidate[len(prefix):], "/") {
			continue
		}
		children = append(children, candidate)
	}
	sort.Strings(children)
	for _, childPath := range children {
		lines = append(lines, partSubtree(proj, childPath, depth+1, width)...)
	}
	return lines
}

func renderMarkdownBody(markdown string, width int) string {
	if width < 20 {
		width = 20
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("notty"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return markdown
	}
	rendered, err := renderer.Render(markdown)
	if err != nil {
		return markdown
	}
	return ansi.Strip(strings.TrimRight(rendered, "\n"))
}

func renderMarkdownValue(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "### "):
			lines[i] = "▸ " + strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
		case strings.HasPrefix(trimmed, "## "):
			lines[i] = "▸▸ " + strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
		case strings.HasPrefix(trimmed, "# "):
			lines[i] = "▸▸▸ " + strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		case strings.HasPrefix(trimmed, "- "):
			lines[i] = "  • " + strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		case strings.HasPrefix(trimmed, "* "):
			lines[i] = "  • " + strings.TrimSpace(strings.TrimPrefix(trimmed, "* "))
		case strings.HasPrefix(trimmed, "> "):
			lines[i] = "  " + strings.TrimSpace(strings.TrimPrefix(trimmed, "> "))
		case strings.HasPrefix(trimmed, "```"):
			lines[i] = "  ─ code fence ─"
		default:
			lines[i] = trimmed
		}
	}
	return strings.Join(alignTableRows(lines), "\n")
}

func alignTableRows(lines []string) []string {
	type tableRow []string
	var (
		rows   []tableRow
		widths []int
		out    []string
	)
	flush := func() {
		for _, row := range rows {
			separator := true
			for _, cell := range row {
				trimmed := strings.Trim(cell, "-")
				if trimmed != "" {
					separator = false
					break
				}
			}
			var b strings.Builder
			b.WriteString("|")
			for ci, cell := range row {
				b.WriteString(" ")
				if separator {
					cell = strings.Repeat("-", maxInt(widths[ci], 3))
				}
				b.WriteString(cell)
				for k := len(cell); k < widths[ci]; k++ {
					b.WriteString(" ")
				}
				b.WriteString(" |")
			}
			out = append(out, b.String())
		}
		rows = nil
		widths = nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") && strings.Count(trimmed, "|") >= 3 {
			cells := strings.Split(trimmed, "|")
			row := make(tableRow, 0, len(cells)-2)
			for _, cell := range cells[1 : len(cells)-1] {
				row = append(row, strings.TrimSpace(cell))
			}
			rows = append(rows, row)
			for ci, cell := range row {
				if ci >= len(widths) {
					widths = append(widths, 0)
				}
				if len(cell) > widths[ci] {
					widths[ci] = len(cell)
				}
			}
			continue
		}
		if len(rows) > 0 {
			flush()
		}
		out = append(out, line)
	}
	if len(rows) > 0 {
		flush()
	}
	return out
}

func ticketSummaryParts(client *missis.Client, summary missis.TicketSummary) map[string]string {
	now := time.Now().UTC()
	proj, err := client.ShowTicket(context.Background(), summary.Ref, missis.ShowOptions{EffectiveAt: now, KnownAt: now})
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	out := make(map[string]string)
	for path, part := range proj.Parts {
		if path == "title" || path == "status" {
			continue
		}
		if part.Value == nil {
			continue
		}
		out[path] = valueText(part.Value)
	}
	return out
}

func valueText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []string:
		return strings.Join(v, ", ")
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func exportTicket(client *missis.Client, summary missis.TicketSummary) (string, error) {
	dir := filepath.Join(".", ".missis.d", "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	parts := ticketSummaryParts(client, summary)
	var b strings.Builder
	b.WriteString("# " + summary.Title + "\n\n")
	b.WriteString("status: " + summary.Status + "\n\n")
	paths := make([]string, 0, len(parts))
	for path := range parts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		b.WriteString("## " + path + "\n\n")
		b.WriteString(parts[path] + "\n\n")
	}
	dst := filepath.Join(dir, summary.Ref[1:]+".md")
	return dst, os.WriteFile(dst, []byte(b.String()), 0o644)
}

func setTicketTitle(client *missis.Client, summary missis.TicketSummary, title string) error {
	_, err := client.Set(context.Background(), missis.RequestContext{Actor: "tui"}, missis.SetValue{Target: summary.Ref + "/title", Value: title})
	return err
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func wrapIndentedLines(lines []string, width int) []string {
	var out []string
	var tablePrefix string
	var tableRows []string
	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}
		indent := len(tablePrefix) - len(strings.TrimLeft(tablePrefix, " "))
		prefix := tablePrefix[:indent]
		out = append(out, renderTable(tableRows, width-indent, prefix)...)
		tableRows = nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isTableLine(trimmed) {
			if len(tableRows) == 0 {
				tablePrefix = line
			}
			tableRows = append(tableRows, trimmed)
			continue
		}
		flushTable()
		out = append(out, wrapLine(line, width)...)
	}
	flushTable()
	return out
}

func wrapLine(line string, width int) []string {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	prefix := line[:indent]
	content := line[indent:]
	available := width - indent
	if available < 1 {
		available = 1
	}
	runes := []rune(content)
	if len(runes) <= available {
		return []string{line}
	}
	var out []string
	for len(runes) > available {
		out = append(out, prefix+string(runes[:available]))
		runes = runes[available:]
	}
	if len(runes) > 0 {
		out = append(out, prefix+string(runes))
	}
	return out
}

func isTableLine(line string) bool {
	return strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|") && strings.Count(line, "|") >= 3
}

func renderTable(rows []string, available int, prefix string) []string {
	if available < 1 {
		available = 1
	}
	var (
		cells   [][]string
		columns int
	)
	for _, row := range rows {
		parts := strings.Split(row, "|")
		rowCells := make([]string, 0, len(parts)-2)
		for _, cell := range parts[1 : len(parts)-1] {
			rowCells = append(rowCells, strings.TrimSpace(cell))
		}
		if len(rowCells) > columns {
			columns = len(rowCells)
		}
		cells = append(cells, rowCells)
	}
	if columns == 0 {
		return nil
	}
	widths := make([]int, columns)
	for _, row := range cells {
		for ci := 0; ci < columns; ci++ {
			cell := ""
			if ci < len(row) {
				cell = row[ci]
			}
			if l := len([]rune(cell)); l > widths[ci] {
				widths[ci] = l
			}
		}
	}
	// "| " + " | " between columns + " |"
	padding := 2 + 3*(columns-1) + 2
	budget := available - padding
	if budget < columns {
		budget = columns
	}
	for sumInts(widths) > budget {
		idx := maxIndex(widths)
		if widths[idx] <= 1 {
			break
		}
		widths[idx]--
	}
	for ci := range widths {
		if widths[ci] < 1 {
			widths[ci] = 1
		}
	}
	var out []string
	for _, row := range cells {
		if isSeparatorRow(row) {
			var b strings.Builder
			b.WriteString("|")
			for ci := 0; ci < columns; ci++ {
				b.WriteString(" ")
				b.WriteString(strings.Repeat("-", widths[ci]))
				b.WriteString(" |")
			}
			out = append(out, prefix+b.String())
			continue
		}
		wrapped := make([][]string, columns)
		maxLines := 0
		for ci := 0; ci < columns; ci++ {
			cell := ""
			if ci < len(row) {
				cell = row[ci]
			}
			lines := wrapCell(cell, widths[ci])
			wrapped[ci] = lines
			if len(lines) > maxLines {
				maxLines = len(lines)
			}
		}
		for li := 0; li < maxLines; li++ {
			var b strings.Builder
			b.WriteString("|")
			for ci := 0; ci < columns; ci++ {
				text := ""
				if li < len(wrapped[ci]) {
					text = wrapped[ci][li]
				}
				b.WriteString(" ")
				b.WriteString(text)
				for k := len([]rune(text)); k < widths[ci]; k++ {
					b.WriteString(" ")
				}
				b.WriteString(" |")
			}
			out = append(out, prefix+b.String())
		}
	}
	return out
}

func wrapCell(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var out []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		current := ""
		flush := func() {
			if current != "" {
				out = append(out, current)
				current = ""
			}
		}
		for _, word := range words {
			if current == "" {
				current = word
			} else if candidate := current + " " + word; len([]rune(candidate)) <= width {
				current = candidate
			} else {
				flush()
				current = word
			}
			for len([]rune(current)) > width {
				out = append(out, string([]rune(current)[:width]))
				current = string([]rune(current)[width:])
			}
		}
		flush()
	}
	return out
}

func sumInts(values []int) int {
	total := 0
	for _, v := range values {
		total += v
	}
	return total
}

func maxIndex(values []int) int {
	idx := 0
	for i := 1; i < len(values); i++ {
		if values[i] > values[idx] {
			idx = i
		}
	}
	return idx
}

func isSeparatorRow(row []string) bool {
	for _, cell := range row {
		if strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func validVisibleTitle(title string) bool {
	visible := 0
	for _, r := range title {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return false
		}
		if !unicode.IsSpace(r) {
			visible++
		}
	}
	return visible > 0
}

func parseCreateEntity(input string) (kind, id, title string, err error) {
	kind, rest, ok := strings.Cut(strings.TrimSpace(input), ":")
	if !ok {
		return "", "", "", errors.New("format: kind:id Title")
	}
	rest = strings.TrimSpace(rest)
	split := strings.IndexFunc(rest, unicode.IsSpace)
	if split < 0 {
		return "", "", "", errors.New("format: kind:id Title")
	}
	id = strings.TrimSpace(rest[:split])
	title = strings.TrimSpace(rest[split:])
	if kind != "project" && kind != "group" {
		return "", "", "", fmt.Errorf("invalid kind: %s; expected project or group", kind)
	}
	if err := model.ValidatePathSegments([]string{id}); err != nil {
		return "", "", "", fmt.Errorf("invalid id: %v", err)
	}
	if !validVisibleTitle(title) {
		return "", "", "", errors.New("title must contain at least one visible character")
	}
	return kind, id, title, nil
}

type linkAction struct {
	Action   string
	Relation string
	Target   string
	Reason   string
}

func parseLinkAction(input string) (linkAction, error) {
	fields := strings.Fields(input)
	if len(fields) < 2 {
		return linkAction{}, errors.New("format: add|retract relation:target [reason], or move project:<id> [reason]")
	}
	action := fields[0]
	rest := fields[1:]
	reason := strings.Join(rest[1:], " ")
	switch action {
	case "add", "retract":
		relation, target, ok := strings.Cut(rest[0], ":")
		if !ok || relation == "" || target == "" {
			return linkAction{}, errors.New("relation:target required")
		}
		return linkAction{Action: action, Relation: relation, Target: target, Reason: reason}, nil
	case "move":
		target := rest[0]
		if !strings.HasPrefix(target, "project:") {
			target = "project:" + target
		}
		return linkAction{Action: "move", Relation: model.RelationHasHome, Target: target, Reason: reason}, nil
	default:
		return linkAction{}, fmt.Errorf("unknown action: %s; expected add, retract, or move", action)
	}
}

func Run(args []string, input io.Reader, output, errorOutput io.Writer) int {
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	if errorOutput == nil {
		errorOutput = os.Stderr
	}
	m, err := newModel()
	if err != nil {
		fmt.Fprintln(errorOutput, err)
		return 1
	}
	// --smoke renders one frame to stdout and exits. It is the hermetic
	// CI smoke (ticket #55 C2): it exercises the real binary's store open,
	// model init, and View rendering without requiring a terminal, which
	// headless runners cannot provide.
	if len(args) > 0 && args[0] == "--smoke" {
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--view":
				if i+1 < len(args) {
					i++
					m.view = args[i]
				}
			case "--kind":
				if i+1 < len(args) {
					i++
					m.kind = args[i]
				}
			case "--input":
				if i+1 < len(args) {
					i++
					m.input = args[i]
					m.cursor = len([]rune(m.input))
				}
			}
		}
		m.refresh()
		switch m.view {
		case "detail":
			if m.isEntityList() && len(m.entities) > 0 {
				ent := m.entities[0].summary
				if lines, err := entityLines(m.client, ent, m.renderWidth()); err == nil {
					m.detail = &detailState{entity: &ent, lines: lines}
				}
			} else if len(m.summaries) > 0 {
				summary := m.summaries[0]
				if lines, err := ticketLines(m.client, summary, m.renderWidth()); err == nil {
					m.detail = &detailState{summary: summary, lines: lines}
				}
			}
			if m.detail == nil {
				m.view = "list"
			}
		case "create":
			m.view = "input"
			m.inputMode = "create"
		case "link":
			m.view = "input"
			m.inputMode = "link"
		}
		_, _ = fmt.Fprint(output, m.View())
		return 0
	}
	p := tea.NewProgram(m, tea.WithInput(input), tea.WithOutput(output), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(errorOutput, err)
		return 1
	}
	return 0
}
