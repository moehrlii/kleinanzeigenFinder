package llm

import (
	"fmt"
	"strings"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/providers"
)

func ValidateDraft(draft config.MonitorConfig) error {
	return validateDraft(draft, providers.DefaultRegistry())
}

func (c *Classifier) ValidateDraft(draft config.MonitorConfig) error {
	return validateDraft(draft, c.providers)
}

func (c *Classifier) ProviderForURL(searchURL string) (providers.Definition, error) {
	return c.providers.Resolve("", searchURL)
}

func validateDraft(draft config.MonitorConfig, registry *providers.Registry) error {
	// Older drafts selected their marketplace through the search URL.
	if _, err := registry.Resolve(providers.MonitorPlatform(draft.Platform, draft.SearchURL), draft.SearchURL); err != nil {
		return err
	}
	if draft.SearchURL == "" {
		return fmt.Errorf("search URL is required")
	}
	if strings.TrimSpace(draft.Name) == "" || strings.TrimSpace(draft.Criteria) == "" {
		return fmt.Errorf("monitor name and criteria are required")
	}
	if draft.BackfillIntervalSeconds < 60 || draft.WatchIntervalSeconds < 60 || draft.WatchPages < 1 || draft.MaxDetailPages < 1 || draft.MaxResults < 0 {
		return fmt.Errorf("invalid monitor scan limits")
	}
	if draft.Rules.MinPrice != nil && *draft.Rules.MinPrice < 0 || draft.Rules.MaxPrice != nil && *draft.Rules.MaxPrice < 0 {
		return fmt.Errorf("price limits must be nonnegative")
	}
	if draft.Rules.MinPrice != nil && draft.Rules.MaxPrice != nil && *draft.Rules.MinPrice > *draft.Rules.MaxPrice {
		return fmt.Errorf("minimum price exceeds maximum price")
	}
	return nil
}
