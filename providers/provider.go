// Package providers defines the marketplace contract and provider registry.
package providers

import (
	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
)

// Provider returns normalized listings with stable IDs and photos belonging to the offer.
// Implementations are shared by monitor goroutines and must support concurrent calls.
type Provider interface {
	FetchListingsPages(searchURL string, maxResults, maxPages int) ([]model.Listing, error)
	FetchDetail(listing model.Listing) (model.Listing, error)
}

// ManagedProvider owns resources that application startup must close.
type ManagedProvider interface {
	Provider
	Close() error
}

// Definition groups marketplace-specific behavior. A nil Open marks a planned integration.
type Definition struct {
	ID                string
	Name              string
	Hosts             []string
	ValidateSearchURL func(string) error
	DraftInstructions string
	Open              func(Settings) (ManagedProvider, error)
}

// Settings supplies the configuration needed to initialize registered marketplaces.
type Settings struct {
	Browser   config.ScraperSettings
	ImmoScout config.ImmoScoutSettings
}
