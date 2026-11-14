package providers

import (
	"cmp"

	"github.com/Kamuyin/kleinanzeigenFinder/providers/ebay"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/immoscout"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/kleinanzeigen"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/wggesucht"
	"net/url"
	"strings"
)

// LegacyPlatform is the fallback for old monitors whose URL cannot identify a marketplace.
const LegacyPlatform = kleinanzeigen.ID

func NormalizePlatform(id string) string { return cmp.Or(id, LegacyPlatform) }

// DefaultRegistry is the application's composition point for marketplace integrations.
func DefaultRegistry() *Registry {
	registry, err := NewRegistry(kleinanzeigen.ID,
		Definition{
			ID: kleinanzeigen.ID, Name: kleinanzeigen.Name, Hosts: []string{"www.kleinanzeigen.de"},
			ValidateSearchURL: kleinanzeigen.ValidateSearchURL, DraftInstructions: kleinanzeigen.DraftInstructions,
			Open: func(cfg Settings) (ManagedProvider, error) { return kleinanzeigen.New(cfg.Browser) },
		},
		Definition{
			ID: immoscout.ID, Name: immoscout.Name,
			Hosts:             []string{"www.immobilienscout24.de", "immobilienscout24.de", "api.mobile.immobilienscout24.de"},
			ValidateSearchURL: immoscout.ValidateSearchURL, DraftInstructions: immoscout.DraftInstructions,
			Open: func(cfg Settings) (ManagedProvider, error) { return immoscout.New(cfg.ImmoScout), nil },
		},
		Definition{
			ID: ebay.ID, Name: ebay.Name, Hosts: []string{"www.ebay.de", "ebay.de", "www.ebay.com", "ebay.com"},
			ValidateSearchURL: ebay.ValidateSearchURL, DraftInstructions: ebay.DraftInstructions,
			Open: func(cfg Settings) (ManagedProvider, error) { return ebay.New(cfg.Browser, cfg.Ebay), nil },
		},
		Definition{ID: wggesucht.ID, Name: wggesucht.Name, Hosts: []string{"www.wg-gesucht.de", "wg-gesucht.de"}},
	)
	// A failure here is a programmer error in the built-in catalog.
	if err != nil {
		panic(err)
	}
	return registry
}

// MonitorPlatform recovers the URL-based routing used by monitors predating the platform field.
// Explicit platform identities are preserved even if their current search URL is invalid.
func MonitorPlatform(id, searchURL string) string {
	if id != "" {
		return id
	}
	parsed, err := url.Parse(searchURL)
	if err == nil {
		for _, definition := range DefaultRegistry().definitions {
			for _, host := range definition.Hosts {
				if strings.EqualFold(parsed.Hostname(), host) {
					return definition.ID
				}
			}
		}
	}
	return LegacyPlatform
}
