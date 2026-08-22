package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

const ProtocolV1 = "missis.plugin/v1"

// ExternalManifest is the admission contract for a future out-of-process
// plugin. It is validated before a command can be considered for loading.
// This file defines the protocol and capability boundary; it intentionally
// does not execute the entrypoint or claim to provide a sandbox.
type ExternalManifest struct {
	Manifest
	Protocol     string   `json:"protocol"`
	Entrypoint   string   `json:"entrypoint"`
	Capabilities []string `json:"capabilities"`
}

func (m ExternalManifest) Validate() error {
	if m.ID == "" || m.Version == "" {
		return errors.New("external plugin ID and version are required")
	}
	if m.Protocol != ProtocolV1 {
		return fmt.Errorf("unsupported plugin protocol %q", m.Protocol)
	}
	if strings.TrimSpace(m.Entrypoint) == "" || strings.ContainsAny(m.Entrypoint, "\x00\r\n") {
		return errors.New("external plugin entrypoint is invalid")
	}
	if !validHash(m.CodeHash) || !validHash(m.ConfigHash) {
		return errors.New("external plugin code_hash and config_hash must be lowercase sha256 hex")
	}
	if err := validateCapabilities(m.Capabilities); err != nil {
		return err
	}
	return nil
}

func validHash(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateCapabilities(capabilities []string) error {
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == "" || strings.TrimSpace(capability) != capability {
			return errors.New("plugin capabilities must be non-empty and trimmed")
		}
		if _, ok := seen[capability]; ok {
			return fmt.Errorf("duplicate plugin capability: %s", capability)
		}
		seen[capability] = struct{}{}
	}
	return nil
}

// CapabilityPolicy is host-side authority. A plugin declaration never grants
// itself access; the host must explicitly allow each capability.
type CapabilityPolicy struct {
	Allowed map[string]bool
}

func (p CapabilityPolicy) Grant(manifest ExternalManifest, requested []string) ([]string, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	declared := make(map[string]struct{}, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		declared[capability] = struct{}{}
	}
	granted := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, capability := range requested {
		if _, duplicate := seen[capability]; duplicate {
			continue
		}
		seen[capability] = struct{}{}
		if _, ok := declared[capability]; !ok || !p.Allowed[capability] {
			return nil, fmt.Errorf("capability %q is not both declared by plugin and allowed by host", capability)
		}
		granted = append(granted, capability)
	}
	sort.Strings(granted)
	return granted, nil
}

// Envelope is the JSON-lines protocol boundary for a future external plugin.
// Payload is opaque to the transport and interpreted only after operation and
// protocol validation by the host.
type Envelope struct {
	Protocol     string          `json:"protocol"`
	RequestID    string          `json:"request_id"`
	Operation    string          `json:"operation"`
	InvocationID string          `json:"invocation_id"`
	Capabilities []string        `json:"capabilities,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	Error        string          `json:"error,omitempty"`
}

func (e Envelope) Validate() error {
	if e.Protocol != ProtocolV1 {
		return fmt.Errorf("unsupported plugin protocol %q", e.Protocol)
	}
	if strings.TrimSpace(e.RequestID) == "" || strings.TrimSpace(e.Operation) == "" {
		return errors.New("plugin envelope request_id and operation are required")
	}
	if err := validateCapabilities(e.Capabilities); err != nil {
		return err
	}
	return nil
}

// HashFile returns the content hash used by ExternalManifest.CodeHash. The
// caller must compare it to the manifest before any future process launch.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
