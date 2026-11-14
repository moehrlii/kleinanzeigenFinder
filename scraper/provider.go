package scraper

import (
	"github.com/Kamuyin/kleinanzeigenFinder/providers"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/kleinanzeigen"
)

type Provider = providers.Provider

func ValidateSearchURL(raw string) error { return kleinanzeigen.ValidateSearchURL(raw) }
