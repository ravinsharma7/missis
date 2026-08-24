package onboarding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentSetupGenerated(t *testing.T) {
	want := AgentSetup()
	got, err := os.ReadFile(filepath.Join("..", "..", "docs", "agent-setup.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("docs/agent-setup.md is stale; run: go run ./tools/generate-onboarding")
	}
}
