package main

import "testing"

func TestNextPatch(t *testing.T) {
	got, err := nextPatch([]string{"v0.1.0", "v0.2.1", "not-a-version", "v0.3.0-rc.1"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.2.2" {
		t.Fatalf("next patch = %s", got)
	}
}
