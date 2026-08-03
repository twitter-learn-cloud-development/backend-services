package model

import (
	"errors"
	"fmt"
	"strings"
)

type Capability string

const (
	CapabilityChat      Capability = "chat"
	CapabilityToolCall  Capability = "tool_call"
	CapabilityJSON      Capability = "json"
	CapabilityVision    Capability = "vision"
	CapabilityEmbedding Capability = "embedding"
)

var (
	ErrModelNotFound     = errors.New("model not found")
	ErrCapabilityMissing = errors.New("model capability missing")
)

type Pricing struct {
	InputMicrosPerMillionTokens  int64
	OutputMicrosPerMillionTokens int64
	Version                      string
}

type Definition struct {
	ID              string
	Provider        string
	ContextWindow   int
	MaxOutputTokens int
	Pricing         Pricing
	Capabilities    []Capability
	Fallbacks       []string
}

func (definition Definition) Supports(required ...Capability) bool {
	available := make(map[Capability]struct{}, len(definition.Capabilities))
	for _, capability := range definition.Capabilities {
		available[capability] = struct{}{}
	}
	for _, capability := range required {
		if _, ok := available[capability]; !ok {
			return false
		}
	}
	return true
}

type Catalog struct {
	models map[string]Definition
}

func NewCatalog(definitions []Definition) (*Catalog, error) {
	catalog := &Catalog{models: make(map[string]Definition, len(definitions))}
	for _, definition := range definitions {
		definition.ID = strings.TrimSpace(definition.ID)
		definition.Provider = strings.TrimSpace(definition.Provider)
		if definition.ID == "" || definition.Provider == "" {
			return nil, errors.New("model id and provider are required")
		}
		if definition.ContextWindow <= 0 {
			return nil, fmt.Errorf("model %q context window must be positive", definition.ID)
		}
		if definition.MaxOutputTokens < 0 || definition.MaxOutputTokens > definition.ContextWindow {
			return nil, fmt.Errorf("model %q max output tokens are invalid", definition.ID)
		}
		if definition.Pricing.InputMicrosPerMillionTokens < 0 || definition.Pricing.OutputMicrosPerMillionTokens < 0 {
			return nil, fmt.Errorf("model %q pricing cannot be negative", definition.ID)
		}
		definition.Pricing.Version = strings.TrimSpace(definition.Pricing.Version)
		if (definition.Pricing.InputMicrosPerMillionTokens > 0 || definition.Pricing.OutputMicrosPerMillionTokens > 0) &&
			definition.Pricing.Version == "" {
			return nil, fmt.Errorf("model %q priced definition requires a pricing version", definition.ID)
		}
		if _, exists := catalog.models[definition.ID]; exists {
			return nil, fmt.Errorf("duplicate model %q", definition.ID)
		}
		catalog.models[definition.ID] = cloneDefinition(definition)
	}
	for _, definition := range catalog.models {
		for _, fallback := range definition.Fallbacks {
			if _, exists := catalog.models[fallback]; !exists {
				return nil, fmt.Errorf("model %q references unknown fallback %q", definition.ID, fallback)
			}
		}
	}
	return catalog, nil
}

func (catalog *Catalog) Lookup(id string) (Definition, bool) {
	if catalog == nil {
		return Definition{}, false
	}
	definition, ok := catalog.models[strings.TrimSpace(id)]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(definition), true
}

// Resolve returns the requested model when possible, otherwise traversing its
// explicit fallback graph in declaration order. It never silently selects an
// unrelated provider model.
func (catalog *Catalog) Resolve(id string, required ...Capability) (Definition, error) {
	candidates, err := catalog.Candidates(id, required...)
	if err != nil {
		return Definition{}, err
	}
	return candidates[0], nil
}

// Candidates returns every capability-compatible model reachable from the
// requested model's explicit fallback graph, preserving declaration order.
func (catalog *Catalog) Candidates(id string, required ...Capability) ([]Definition, error) {
	requested := strings.TrimSpace(id)
	if _, ok := catalog.Lookup(requested); !ok {
		return nil, fmt.Errorf("%w: %s", ErrModelNotFound, requested)
	}
	queue := []string{requested}
	visited := make(map[string]struct{}, len(catalog.models))
	candidates := make([]Definition, 0, len(catalog.models))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if _, seen := visited[current]; seen {
			continue
		}
		visited[current] = struct{}{}
		definition, _ := catalog.Lookup(current)
		if definition.Supports(required...) {
			candidates = append(candidates, definition)
		}
		queue = append(queue, definition.Fallbacks...)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: model %s requires %v", ErrCapabilityMissing, requested, required)
	}
	return candidates, nil
}

func cloneDefinition(definition Definition) Definition {
	definition.Capabilities = append([]Capability(nil), definition.Capabilities...)
	definition.Fallbacks = append([]string(nil), definition.Fallbacks...)
	return definition
}
