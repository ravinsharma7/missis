package model

import "encoding/json"

// UnmarshalJSON restores the core structured payload types after an event is
// read from SQLite. Without this, Data would become map[string]any and callers
// would have to guess whether a map was a CodeRef, GitRef, media descriptor,
// or artifact descriptor.
func (v *Value) UnmarshalJSON(data []byte) error {
	var wire struct {
		Kind      ValueKind       `json:"Kind"`
		Text      string          `json:"Text"`
		Data      json.RawMessage `json:"Data"`
		List      []string        `json:"List"`
		Ref       *Ref            `json:"Ref"`
		Retracted bool            `json:"Retracted"`
		OrderKey  string          `json:"OrderKey"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*v = Value{
		Kind:      wire.Kind,
		Text:      wire.Text,
		List:      wire.List,
		Ref:       wire.Ref,
		Retracted: wire.Retracted,
		OrderKey:  wire.OrderKey,
	}
	if len(wire.Data) == 0 || string(wire.Data) == "null" {
		return nil
	}

	switch wire.Kind {
	case ValueKindCodeRef:
		var value CodeRef
		if err := json.Unmarshal(wire.Data, &value); err != nil {
			return err
		}
		v.Data = value
	case ValueKindGitRef:
		var value GitRef
		if err := json.Unmarshal(wire.Data, &value); err != nil {
			return err
		}
		v.Data = value
	case ValueKindArtifact:
		var value ArtifactDescriptor
		if err := json.Unmarshal(wire.Data, &value); err != nil {
			return err
		}
		v.Data = value
	case ValueKindImage, ValueKindVideo, ValueKindAudio, ValueKindEmbed:
		var value MediaDescriptor
		if err := json.Unmarshal(wire.Data, &value); err != nil {
			return err
		}
		v.Data = value
	case ValueKindInlineSequence:
		var value InlineSequence
		if err := json.Unmarshal(wire.Data, &value); err != nil {
			return err
		}
		v.Data = value
	default:
		var value any
		if err := json.Unmarshal(wire.Data, &value); err != nil {
			return err
		}
		v.Data = value
	}
	return nil
}
