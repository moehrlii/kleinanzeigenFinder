// Package scraper retains the original Kleinanzeigen entry points for compatibility.
// New integrations belong in providers/<marketplace>.
package scraper

import (
	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/kleinanzeigen"
)

type KleinanzeigenScraper = kleinanzeigen.KleinanzeigenScraper

func New(cfg config.ScraperSettings) (*KleinanzeigenScraper, error) { return kleinanzeigen.New(cfg) }
