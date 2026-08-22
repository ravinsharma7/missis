package schema

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/plugin"
)

// Catalog contains the built-in declaration kinds plus explicitly registered
// plugin kinds. Registration is a composition-time operation; writes only use
// validators from this catalog and never call plugin code implicitly.
type Catalog struct {
	mu      sync.RWMutex
	plugins map[string]plugin.KindRegistration
}

func NewCatalog() *Catalog {
	return &Catalog{plugins: make(map[string]plugin.KindRegistration)}
}

// Register adds one plugin kind and rejects collisions with built-ins or
// another plugin's kind/schema identity. A validator is mandatory: a plugin
// kind without write validation would bypass the core boundary.
func (c *Catalog) Register(reg plugin.KindRegistration) error {
	if reg.Manifest.ID == "" || reg.Manifest.Version == "" {
		return fmt.Errorf("plugin kind manifest ID and version are required")
	}
	if reg.Kind == "" || strings.TrimSpace(string(reg.Kind)) != string(reg.Kind) {
		return fmt.Errorf("plugin %s kind must be non-empty and trimmed", reg.Manifest.ID)
	}
	if reg.Schema == "" || strings.TrimSpace(reg.Schema) != reg.Schema {
		return fmt.Errorf("plugin %s schema identity must be non-empty and trimmed", reg.Manifest.ID)
	}
	if reg.Validate == nil {
		return fmt.Errorf("plugin %s kind %s validator is required", reg.Manifest.ID, reg.Kind)
	}
	if _, ok := baseKinds[string(reg.Kind)]; ok {
		return fmt.Errorf("plugin %s cannot replace built-in kind %s", reg.Manifest.ID, reg.Kind)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.plugins == nil {
		c.plugins = make(map[string]plugin.KindRegistration)
	}
	if _, ok := c.plugins[string(reg.Kind)]; ok {
		return fmt.Errorf("duplicate plugin value kind: %s", reg.Kind)
	}
	if existing, ok := c.plugins[reg.Schema]; ok {
		return fmt.Errorf("duplicate plugin schema %s (owned by %s)", reg.Schema, existing.Manifest.ID)
	}
	c.plugins[string(reg.Kind)] = reg
	c.plugins[reg.Schema] = reg
	return nil
}

// ParseKind is the catalog-aware form of ParseKind. It supports a plugin kind
// anywhere a base kind is accepted, including list[K] and map[K:V].
func (c *Catalog) ParseKind(text string) (KindSpec, error) {
	c.mu.RLock()
	plugins := make(map[string]plugin.KindRegistration, len(c.plugins))
	for key, registration := range c.plugins {
		plugins[key] = registration
	}
	c.mu.RUnlock()
	return parseKind(text, plugins)
}

func parseKind(text string, plugins map[string]plugin.KindRegistration) (KindSpec, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return KindSpec{}, fmt.Errorf("declaration kind must not be empty")
	}
	if base, ok := baseKinds[trimmed]; ok {
		return KindSpec{Base: base}, nil
	}
	if registration, ok := plugins[trimmed]; ok {
		return KindSpec{Base: registration.Kind}, nil
	}
	if inner, ok := compositeInner(trimmed, "list["); ok {
		if inner == "" {
			return KindSpec{}, fmt.Errorf("list[K] requires an element kind")
		}
		elem, err := parseKind(inner, plugins)
		if err != nil {
			return KindSpec{}, err
		}
		if !elem.isBase() {
			return KindSpec{}, fmt.Errorf("list[K] element kind must be a base kind in v1")
		}
		return KindSpec{Base: model.ValueKindList, Elements: &elem}, nil
	}
	if inner, ok := compositeInner(trimmed, "map["); ok {
		parts := strings.Split(inner, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return KindSpec{}, fmt.Errorf("map[K:V] requires exactly one key kind and one value kind")
		}
		keyKind, err := parseKind(parts[0], plugins)
		if err != nil {
			return KindSpec{}, err
		}
		valKind, err := parseKind(parts[1], plugins)
		if err != nil {
			return KindSpec{}, err
		}
		if !keyKind.isBase() || !valKind.isBase() {
			return KindSpec{}, fmt.Errorf("map[K:V] kinds must be base kinds in v1")
		}
		return KindSpec{Base: model.ValueKindMap, KeyKind: &keyKind, ValKind: &valKind}, nil
	}
	if inner, ok := compositeInner(trimmed, "ref["); ok {
		// Plugin target kinds are deliberately not accepted here yet. Ref target
		// authorization needs a separate entity-kind registration contract.
		return ParseKind("ref[" + inner + "]")
	}
	return KindSpec{}, fmt.Errorf("unknown declaration kind: %s", trimmed)
}

func (c *Catalog) ValidBaseKind(kind model.ValueKind) bool {
	if ValidBaseKind(kind) {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.plugins[string(kind)]
	return ok
}

// ValidateValue runs a registered plugin validator. Built-in values remain
// governed by the existing core validation rules. Unknown kinds are rejected
// by ValidBaseKind before this method is normally reached.
func (c *Catalog) ValidateValue(value model.Value) error {
	c.mu.RLock()
	registration, ok := c.plugins[string(value.Kind)]
	c.mu.RUnlock()
	if !ok {
		return nil
	}
	if err := registration.Validate(value); err != nil {
		return fmt.Errorf("plugin %s rejected %s: %w", registration.Manifest.ID, value.Kind, err)
	}
	return nil
}
