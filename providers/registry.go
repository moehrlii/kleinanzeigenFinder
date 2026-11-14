package providers

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Registry is immutable after construction and safe to share between the runner and wizard.
type Registry struct {
	definitions []Definition
	defaultID   string
}

func NewRegistry(defaultID string, definitions ...Definition) (*Registry, error) {
	seen := make(map[string]bool)
	hosts := make(map[string]bool)
	owned := slices.Clone(definitions)
	for i, definition := range owned {
		if definition.ID == "" || definition.Name == "" || seen[definition.ID] {
			return nil, fmt.Errorf("invalid or duplicate provider %q", definition.ID)
		}
		if definition.Open != nil && (definition.ValidateSearchURL == nil || definition.DraftInstructions == "") {
			return nil, fmt.Errorf("provider %q requires URL validation and draft instructions", definition.ID)
		}
		seen[definition.ID] = true
		owned[i].Hosts = slices.Clone(definition.Hosts)
		for _, host := range definition.Hosts {
			if host == "" || hosts[strings.ToLower(host)] {
				return nil, fmt.Errorf("invalid or duplicate provider host %q", host)
			}
			hosts[strings.ToLower(host)] = true
		}
	}
	registry := &Registry{definitions: owned, defaultID: defaultID}
	if _, err := registry.Resolve(defaultID, ""); err != nil {
		return nil, err
	}
	return registry, nil
}

// Available returns implemented providers in registration order without initializing them.
func (r *Registry) Available() []Definition {
	var available []Definition
	for _, definition := range r.definitions {
		if definition.Open != nil {
			definition.Hosts = slices.Clone(definition.Hosts)
			available = append(available, definition)
		}
	}
	return available
}

// Resolve identifies a provider from an explicit ID or search URL, validates that URL,
// and rejects planned integrations before any browser or AI request is made.
func (r *Registry) Resolve(id, searchURL string) (Definition, error) {
	if id == "" && searchURL != "" {
		u, err := url.Parse(searchURL)
		if err != nil || u.Scheme != "https" || u.User != nil {
			return Definition{}, fmt.Errorf("invalid HTTPS search URL")
		}
		for _, definition := range r.definitions {
			for _, host := range definition.Hosts {
				if strings.EqualFold(u.Host, host) {
					id = definition.ID
					break
				}
			}
			if id != "" {
				break
			}
		}
		if id == "" {
			return Definition{}, fmt.Errorf("no provider registered for search host %q", u.Host)
		}
	}
	id = cmp.Or(id, r.defaultID)
	for _, definition := range r.definitions {
		if definition.ID != id {
			continue
		}
		if definition.Open == nil {
			return Definition{}, fmt.Errorf("%s integration is not implemented yet", definition.Name)
		}
		if searchURL != "" {
			if err := definition.ValidateSearchURL(searchURL); err != nil {
				return Definition{}, err
			}
		}
		definition.Hosts = slices.Clone(definition.Hosts)
		return definition, nil
	}
	return Definition{}, fmt.Errorf("unknown provider %q", id)
}
