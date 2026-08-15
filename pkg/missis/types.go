package missis

import "time"

type TicketSummary struct {
	Ref        string
	ID         string
	Title      string
	Status     string
	RecordedAt time.Time
}

type PartView struct {
	ID        string
	Path      string
	Value     any
	ValueKind string
	ParentID  any
	CreatedBy string
}

type TicketProjection struct {
	Ref        string
	ID         string
	Title      string
	Status     string
	RecordedAt time.Time
	Parts      map[string]PartView
}

type EventView struct {
	ID          string
	Alias       string
	Sequence    uint64
	Operation   string
	Target      string
	Value       any
	RecordedAt  time.Time
	EffectiveAt time.Time
	Actor       string
	Reason      string
}

type LinkView struct {
	From      string
	Relation  string
	To        string
	Direction string
	Origin    string
	CreatedBy string
}

type LineageEdge struct {
	From      string
	Relation  string
	To        string
	Direction string
	Depth     int
	Origin    string
	CreatedBy string
}
