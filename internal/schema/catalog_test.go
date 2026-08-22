package schema

import (
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/plugin"
)

func testPluginKind() plugin.KindRegistration {
	return plugin.KindRegistration{
		Manifest: plugin.Manifest{ID: "plugin.cards", Version: "1"},
		Kind:     model.ValueKind("plugin/card"),
		Schema:   "plugin/card/v1",
		Validate: func(value model.Value) error {
			if value.Text == "" {
				return &testValidationError{"text is required"}
			}
			return nil
		},
	}
}

type testValidationError struct{ message string }

func (e *testValidationError) Error() string { return e.message }

func TestCatalogRegistersAndParsesPluginKinds(t *testing.T) {
	catalog := NewCatalog()
	if err := catalog.Register(testPluginKind()); err != nil {
		t.Fatal(err)
	}
	for _, declaration := range []string{"plugin/card", "plugin/card/v1", "list[plugin/card]", "map[text:plugin/card]"} {
		spec, err := catalog.ParseKind(declaration)
		if err != nil {
			t.Fatalf("ParseKind(%q): %v", declaration, err)
		}
		if declaration == "plugin/card" || declaration == "plugin/card/v1" {
			if spec.Base != model.ValueKind("plugin/card") {
				t.Fatalf("%q base = %q", declaration, spec.Base)
			}
		}
	}
	if !catalog.ValidBaseKind(model.ValueKind("plugin/card")) {
		t.Fatal("registered plugin kind is not a valid base kind")
	}
	if err := catalog.ValidateValue(model.Value{Kind: model.ValueKind("plugin/card"), Text: "card"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ValidateValue(model.Value{Kind: model.ValueKind("plugin/card")}); err == nil || !strings.Contains(err.Error(), "text is required") {
		t.Fatalf("invalid plugin value error = %v", err)
	}
}

func TestCatalogRequiresValidatorAndRejectsCollisions(t *testing.T) {
	catalog := NewCatalog()
	missingValidator := testPluginKind()
	missingValidator.Validate = nil
	if err := catalog.Register(missingValidator); err == nil {
		t.Fatal("plugin kind without validator was accepted")
	}
	if err := catalog.Register(testPluginKind()); err != nil {
		t.Fatal(err)
	}
	duplicate := testPluginKind()
	duplicate.Manifest.ID = "plugin.other"
	if err := catalog.Register(duplicate); err == nil {
		t.Fatal("duplicate plugin kind was accepted")
	}
	builtIn := testPluginKind()
	builtIn.Kind = model.ValueKindText
	builtIn.Schema = "plugin/text/v1"
	if err := catalog.Register(builtIn); err == nil {
		t.Fatal("plugin replacement for built-in kind was accepted")
	}
}
