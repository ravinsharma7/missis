// Package externalref owns the storage-neutral ExternalRefV1 wire contract.
// It deliberately has no filesystem, transport, credential, or Missis domain
// dependency so durable values and public SDK aliases use one strict codec.
package externalref

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ravinsharma7/missis/internal/storeidentity"
)

const VersionV1 = "external-ref-v1"

type ReferenceV1 struct {
	Version     string      `json:"version"`
	StoreID     string      `json:"store_id"`
	Namespace   string      `json:"namespace"`
	Kind        string      `json:"kind"`
	EntityID    string      `json:"entity_id"`
	SubentityID string      `json:"subentity_id,omitempty"`
	Pin         *PinV1      `json:"pin,omitempty"`
	Observation *ObservedV1 `json:"observation,omitempty"`
	DisplayHint string      `json:"display_hint,omitempty"`
}

type PinV1 struct {
	EventID          string `json:"event_id,omitempty"`
	CheckpointDigest string `json:"checkpoint_digest,omitempty"`
}

type ObservedV1 struct {
	StreamRevision *uint64    `json:"stream_revision,omitempty"`
	CurrentEventID string     `json:"current_event_id,omitempty"`
	ObservedAt     *time.Time `json:"observed_at,omitempty"`
}

func (r ReferenceV1) Validate() error {
	if r.Version != VersionV1 {
		return fmt.Errorf("unsupported external reference version %q", r.Version)
	}
	if err := validateToken("store_id", r.StoreID); err != nil {
		return err
	}
	if err := storeidentity.ValidateStoreID(r.StoreID); err != nil {
		return fmt.Errorf("external reference store_id: %w", err)
	}
	if err := validateToken("namespace", r.Namespace); err != nil {
		return err
	}
	if err := validateToken("kind", r.Kind); err != nil {
		return err
	}
	if err := validateToken("entity_id", r.EntityID); err != nil {
		return err
	}
	if r.SubentityID != "" {
		if err := validateToken("subentity_id", r.SubentityID); err != nil {
			return err
		}
	}
	if r.Pin != nil {
		if r.Pin.EventID == "" && r.Pin.CheckpointDigest == "" {
			return fmt.Errorf("external reference pin must name an event or checkpoint")
		}
		if r.Pin.EventID != "" {
			if err := validateToken("pin.event_id", r.Pin.EventID); err != nil {
				return err
			}
		}
		if r.Pin.CheckpointDigest != "" {
			if err := validateToken("pin.checkpoint_digest", r.Pin.CheckpointDigest); err != nil {
				return err
			}
		}
	}
	if r.Observation != nil {
		if r.Observation.StreamRevision == nil && r.Observation.CurrentEventID == "" && r.Observation.ObservedAt == nil {
			return fmt.Errorf("external reference observation must contain evidence")
		}
		if r.Observation.CurrentEventID != "" {
			if err := validateToken("observation.current_event_id", r.Observation.CurrentEventID); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateToken(name, value string) error {
	if value == "" {
		return fmt.Errorf("external reference %s is required", name)
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t ") {
		return fmt.Errorf("external reference %s contains whitespace or control characters", name)
	}
	return nil
}

// IdentityKey excludes mutable observation and display fields.
func (r ReferenceV1) IdentityKey() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	fields := []string{r.Version, r.StoreID, r.Namespace, r.Kind, r.EntityID, r.SubentityID}
	if r.Pin != nil {
		fields = append(fields, r.Pin.EventID, r.Pin.CheckpointDigest)
	} else {
		fields = append(fields, "", "")
	}
	var key strings.Builder
	for _, field := range fields {
		key.WriteString(strconv.Itoa(len(field)))
		key.WriteByte(':')
		key.WriteString(field)
	}
	return key.String(), nil
}

// ParseV1 rejects unknown fields and trailing JSON. Paths and locators are
// therefore rejected rather than retained as inert but misleading data.
func ParseV1(raw []byte) (ReferenceV1, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var ref ReferenceV1
	if err := decoder.Decode(&ref); err != nil {
		return ReferenceV1{}, fmt.Errorf("decode external reference: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ReferenceV1{}, fmt.Errorf("decode external reference: trailing JSON value")
		}
		return ReferenceV1{}, fmt.Errorf("decode external reference: %w", err)
	}
	if err := ref.Validate(); err != nil {
		return ReferenceV1{}, err
	}
	return ref, nil
}

// CoerceV1 applies strict unknown-field validation even when the caller
// supplied a map through JSON/SDK data rather than a typed ReferenceV1.
func CoerceV1(value any) (ReferenceV1, error) {
	switch ref := value.(type) {
	case ReferenceV1:
		return ref, ref.Validate()
	case *ReferenceV1:
		if ref == nil {
			return ReferenceV1{}, fmt.Errorf("external reference is nil")
		}
		return *ref, ref.Validate()
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ReferenceV1{}, err
	}
	return ParseV1(raw)
}
