package plugin

import "github.com/ravinsharma7/missis/internal/model"

// KindRegistration describes a plugin-owned value kind. The schema identity
// is explicit so a plugin kind is not accepted merely because a renderer knows
// how to display it. Validate is run by the application boundary before a
// value can be appended.
type KindRegistration struct {
	Manifest Manifest
	Kind     model.ValueKind
	Schema   string
	Validate func(model.Value) error
}
