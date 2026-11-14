package model

import (
	"slices"
)

type Listing struct {
	Platform     string            `json:"platform,omitempty"`
	ImageURLs    []string          `json:"image_urls,omitempty"`
	ID           string            `json:"id"`
	URL          string            `json:"url"`
	Title        string            `json:"title"`
	Price        string            `json:"price,omitempty"`
	Location     string            `json:"location,omitempty"`
	PostedAt     string            `json:"posted_at,omitempty"`
	Description  string            `json:"description,omitempty"`
	ImageURL     string            `json:"image_url,omitempty"`
	Seller       string            `json:"seller,omitempty"`
	RawText      string            `json:"raw_text,omitempty"`
	PriceAmount  *float64          `json:"price_amount,omitempty"`
	Attributes   map[string]string `json:"attributes"`
	Monitor      string            `json:"monitor,omitempty"`
	DetailLoaded bool              `json:"detail_loaded,omitempty"`
}

func (l *Listing) Identity() string {
	if l.ID != "" {
		return l.ID
	}
	return l.URL
}

func (l *Listing) MergedWith(detail Listing) Listing {
	attrs := make(map[string]string)
	for k, v := range l.Attributes {
		attrs[k] = v
	}
	for k, v := range detail.Attributes {
		if v != "" {
			attrs[k] = v
		}
	}

	priceAmount := l.PriceAmount
	if detail.PriceAmount != nil {
		priceAmount = detail.PriceAmount
	}

	return Listing{
		Platform:     coalesce(detail.Platform, l.Platform),
		ImageURLs:    mergeImages(detail.ImageURLs, l.ImageURLs),
		ID:           coalesce(detail.ID, l.ID),
		URL:          coalesce(detail.URL, l.URL),
		Title:        coalesce(detail.Title, l.Title),
		Price:        coalesce(detail.Price, l.Price),
		Location:     coalesce(detail.Location, l.Location),
		PostedAt:     coalesce(detail.PostedAt, l.PostedAt),
		Description:  coalesce(detail.Description, l.Description),
		ImageURL:     coalesce(detail.ImageURL, l.ImageURL),
		Seller:       coalesce(detail.Seller, l.Seller),
		RawText:      coalesce(detail.RawText, l.RawText),
		PriceAmount:  priceAmount,
		Attributes:   attrs,
		Monitor:      coalesce(l.Monitor, detail.Monitor),
		DetailLoaded: l.DetailLoaded || detail.DetailLoaded,
	}
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type Classification struct {
	Suitable         bool           `json:"suitable"`
	Score            int            `json:"score"`
	Reason           string         `json:"reason"`
	Summary          string         `json:"summary,omitempty"`
	RedFlags         []string       `json:"red_flags,omitempty"`
	Raw              map[string]any `json:"raw,omitempty"`
}

func mergeImages(groups ...[]string) []string {
	var images []string
	for _, group := range groups {
		for _, image := range group {
			if image != "" && !slices.Contains(images, image) {
				images = append(images, image)
			}
		}
	}
	return images
}

func AcceptedWithoutLLM(reason string) Classification {
	if reason == "" {
		reason = "Matched local rules."
	}
	return Classification{
		Suitable: true,
		Score:    100,
		Reason:   reason,
	}
}
