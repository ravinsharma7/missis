package externalref

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

const vectorStoreID = "store:v1:sha256:120311f35ac84b69682c5b5be1dbe7ab96994ef4a8db9d43473d8d0f1f379867"

func validReference() ReferenceV1 {
	revision := uint64(7)
	observed := time.Date(2026, 8, 27, 1, 2, 3, 0, time.UTC)
	return ReferenceV1{
		Version: VersionV1, StoreID: vectorStoreID, Namespace: "missis", Kind: "ticket",
		EntityID: "ticket:target", SubentityID: "part:problem",
		Pin:         &PinV1{EventID: "event:pinned"},
		Observation: &ObservedV1{StreamRevision: &revision, CurrentEventID: "event:observed", ObservedAt: &observed},
		DisplayHint: "project#42/problem",
	}
}

func TestStrictReferenceRoundTripAndIdentityBoundary(t *testing.T) {
	vectorBytes, err := os.ReadFile("../../specs/vectors/external-ref-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		AcceptedJSON string `json:"accepted_json"`
		IdentityKey  string `json:"identity_key"`
	}
	if err := json.Unmarshal(vectorBytes, &vector); err != nil {
		t.Fatal(err)
	}
	raw := []byte(vector.AcceptedJSON)
	parsed, err := ParseV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := validReference()
	first, err := parsed.IdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	want.DisplayHint = "moved#9"
	want.Observation.CurrentEventID = "event:new"
	second, err := want.IdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("display/observation changed identity: %q != %q", first, second)
	}
	if first != vector.IdentityKey {
		t.Fatalf("identity key = %q, vector = %q", first, vector.IdentityKey)
	}
}

func TestStrictReferenceRejectsLocatorUnknownVersionAndOldStoreID(t *testing.T) {
	tests := []struct {
		name, raw, want string
	}{
		{"locator", `{"version":"external-ref-v1","store_id":"` + vectorStoreID + `","namespace":"missis","kind":"ticket","entity_id":"ticket:x","path":"/tmp/store.db"}`, "unknown field"},
		{"version", `{"version":"external-ref-v2","store_id":"` + vectorStoreID + `","namespace":"missis","kind":"ticket","entity_id":"ticket:x"}`, "unsupported"},
		{"old identity", `{"version":"external-ref-v1","store_id":"store:old","namespace":"missis","kind":"ticket","entity_id":"ticket:x"}`, "store:v1:sha256"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseV1([]byte(test.raw)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want %q", err, test.want)
			}
		})
	}
}
