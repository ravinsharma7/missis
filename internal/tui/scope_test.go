package tui

import (
	"strings"
	"testing"
)

func TestParseCreateEntity(t *testing.T) {
	kind, id, title, err := parseCreateEntity("project:missis Missis")
	if err != nil {
		t.Fatal(err)
	}
	if kind != "project" || id != "missis" || title != "Missis" {
		t.Fatalf("got %q %q %q", kind, id, title)
	}
	kind, id, title, err = parseCreateEntity("group:eng Engineering")
	if err != nil || kind != "group" || id != "eng" || title != "Engineering" {
		t.Fatalf("group parse: %q %q %q err=%v", kind, id, title, err)
	}
	if _, _, _, err := parseCreateEntity("team:eng Engineering"); err == nil || !strings.Contains(err.Error(), "invalid kind") {
		t.Fatalf("bad kind must fail, got %v", err)
	}
	if _, _, _, err := parseCreateEntity("project:missis"); err == nil {
		t.Fatal("missing title must fail")
	}
	if _, _, _, err := parseCreateEntity("project:missis \x01"); err == nil || !strings.Contains(err.Error(), "visible") {
		t.Fatalf("control-only title must fail, got %v", err)
	}
	if _, _, _, err := parseCreateEntity("project:missis \u200b"); err == nil {
		t.Fatal("zero-width-only title must fail")
	}
	if _, _, _, err := parseCreateEntity("project:Bad*ID Title"); err == nil {
		t.Fatal("invalid id must fail")
	}
}

func TestValidVisibleTitle(t *testing.T) {
	for _, tt := range []struct {
		title string
		want  bool
	}{
		{"Missis", true},
		{"", false},
		{"   ", false},
		{"\x01", false},
		{"\u200b", false},
		{"a\u200b", false},
		{"a b", true},
	} {
		if got := validVisibleTitle(tt.title); got != tt.want {
			t.Fatalf("validVisibleTitle(%q) = %v, want %v", tt.title, got, tt.want)
		}
	}
}

func TestParseLinkAction(t *testing.T) {
	act, err := parseLinkAction("add contains:#1 reorg")
	if err != nil {
		t.Fatal(err)
	}
	if act.Action != "add" || act.Relation != "contains" || act.Target != "#1" || act.Reason != "reorg" {
		t.Fatalf("add action = %+v", act)
	}
	act, err = parseLinkAction("retract has-home:project:a")
	if err != nil {
		t.Fatal(err)
	}
	if act.Action != "retract" || act.Relation != "has-home" || act.Target != "project:a" {
		t.Fatalf("retract action = %+v", act)
	}
	act, err = parseLinkAction("move b reorg")
	if err != nil {
		t.Fatal(err)
	}
	if act.Action != "move" || act.Relation != "has-home" || act.Target != "project:b" || act.Reason != "reorg" {
		t.Fatalf("move action = %+v", act)
	}
	if _, err := parseLinkAction("bogus project:b"); err == nil {
		t.Fatal("unknown action must fail")
	}
	if _, err := parseLinkAction("add"); err == nil {
		t.Fatal("missing target must fail")
	}
}
