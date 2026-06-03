package rules

import (
	"fmt"
	"strings"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
)

type RuleDecision struct {
	Accepted bool
	Reasons  []string
}

func (d *RuleDecision) Reject(reason string) {
	d.Accepted = false
	d.Reasons = append(d.Reasons, reason)
}

func ApplyRules(listing model.Listing, rules config.RulesConfig) RuleDecision {
	decision := RuleDecision{Accepted: true}
	text := combinedText(listing)

	if len(rules.ExcludeKeywords) > 0 {
		for _, keyword := range rules.ExcludeKeywords {
			if strings.Contains(text, strings.ToLower(keyword)) {
				decision.Reject(fmt.Sprintf("Excluded keyword: %s", keyword))
			}
		}
	}

	if len(rules.IncludeKeywords) > 0 {
		for _, keyword := range rules.IncludeKeywords {
			if !strings.Contains(text, strings.ToLower(keyword)) {
				decision.Reject(fmt.Sprintf("Missing required keyword: %s", keyword))
			}
		}
	}

	if len(rules.IncludeAny) > 0 {
		found := false
		for _, keyword := range rules.IncludeAny {
			if strings.Contains(text, strings.ToLower(keyword)) {
				found = true
				break
			}
		}
		if !found {
			decision.Reject(fmt.Sprintf("None of these keywords found: %s", strings.Join(rules.IncludeAny, ", ")))
		}
	}

	if rules.MaxPrice != nil && listing.PriceAmount != nil {
		if *listing.PriceAmount > *rules.MaxPrice {
			decision.Reject(fmt.Sprintf("Price %g exceeds max %g", *listing.PriceAmount, *rules.MaxPrice))
		}
	}

	if rules.MinPrice != nil && listing.PriceAmount != nil {
		if *listing.PriceAmount < *rules.MinPrice {
			decision.Reject(fmt.Sprintf("Price %g below min %g", *listing.PriceAmount, *rules.MinPrice))
		}
	}

	if len(decision.Reasons) == 0 {
		decision.Reasons = append(decision.Reasons, "Matched local rules.")
	}

	return decision
}

func combinedText(listing model.Listing) string {
	parts := []string{
		listing.Title,
		listing.Price,
		listing.Location,
		listing.Description,
		listing.RawText,
	}

	for k, v := range listing.Attributes {
		parts = append(parts, fmt.Sprintf("%s %s", k, v))
	}

	var validParts []string
	for _, p := range parts {
		if p != "" {
			validParts = append(validParts, p)
		}
	}

	return strings.ToLower(strings.Join(validParts, " "))
}
