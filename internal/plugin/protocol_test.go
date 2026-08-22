package plugin

import (
	"strings"
	"testing"
)

func validExternalManifest() ExternalManifest {
	return ExternalManifest{
		Manifest: Manifest{
			ID:         "plugin.external",
			Version:    "1",
			CodeHash:   strings.Repeat("a", 64),
			ConfigHash: strings.Repeat("b", 64),
		},
		Protocol:     ProtocolV1,
		Entrypoint:   "/opt/missis/plugin",
		Capabilities: []string{"terminal.render", "artifact.read"},
	}
}

func TestExternalManifestRequiresHashesAndDeclaredCapabilities(t *testing.T) {
	manifest := validExternalManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	granted, err := (CapabilityPolicy{Allowed: map[string]bool{"terminal.render": true, "artifact.read": false}}).Grant(manifest, []string{"terminal.render"})
	if err != nil || len(granted) != 1 || granted[0] != "terminal.render" {
		t.Fatalf("granted = %v, err = %v", granted, err)
	}
	if _, err := (CapabilityPolicy{Allowed: map[string]bool{"terminal.render": true}}).Grant(manifest, []string{"artifact.read"}); err == nil {
		t.Fatal("host denied capability was granted")
	}
	manifest.CodeHash = "not-a-hash"
	if err := manifest.Validate(); err == nil {
		t.Fatal("invalid code hash was accepted")
	}
}

func TestEnvelopeRejectsProtocolOrCapabilityConfusion(t *testing.T) {
	envelope := Envelope{Protocol: ProtocolV1, RequestID: "request:1", Operation: "render", Capabilities: []string{"terminal.render"}}
	if err := envelope.Validate(); err != nil {
		t.Fatal(err)
	}
	envelope.Protocol = "other/v1"
	if err := envelope.Validate(); err == nil {
		t.Fatal("unknown protocol was accepted")
	}
}
