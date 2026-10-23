package provider

import (
	"fmt"
	"strings"
)

// Registry is an immutable, explicitly constructed provider registry. It does
// not use reflection, init() side effects, or dynamic loading.
type Registry struct {
	byID  map[ID]Provider
	order []ID
}

// NewRegistry validates and registers providers. It rejects nil providers,
// empty IDs, empty display names, and duplicate IDs. An empty registry is
// allowed; production composition must explicitly register its providers.
func NewRegistry(providers ...Provider) (*Registry, error) {
	registry := &Registry{byID: make(map[ID]Provider, len(providers))}
	for index, provider := range providers {
		if provider == nil {
			return nil, fmt.Errorf("provider: registry entry %d is nil", index)
		}
		metadata := provider.Metadata()
		id := metadata.ID
		if strings.TrimSpace(string(id)) == "" {
			return nil, fmt.Errorf("provider: registry entry %d has an empty id", index)
		}
		if string(id) != strings.TrimSpace(string(id)) {
			return nil, fmt.Errorf("provider: registry entry %d has an id with surrounding whitespace", index)
		}
		if strings.TrimSpace(metadata.DisplayName) == "" {
			return nil, fmt.Errorf("provider: registry entry %q has an empty display name", id)
		}
		if _, exists := registry.byID[id]; exists {
			return nil, fmt.Errorf("provider: duplicate id %q", id)
		}
		registry.byID[id] = provider
		registry.order = append(registry.order, id)
	}
	return registry, nil
}

// Lookup returns the provider registered under id.
func (r *Registry) Lookup(id ID) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	provider, ok := r.byID[id]
	return provider, ok
}

// List returns metadata in explicit registration order. The returned slice is
// a copy; callers cannot mutate registry state.
func (r *Registry) List() []Metadata {
	if r == nil {
		return nil
	}
	result := make([]Metadata, 0, len(r.order))
	for _, id := range r.order {
		provider := r.byID[id]
		if provider == nil {
			continue
		}
		result = append(result, provider.Metadata())
	}
	return result
}
