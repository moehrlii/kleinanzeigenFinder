package ebay

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Kamuyin/kleinanzeigenFinder/model"
)

type rawListing struct {
	URL              string            `json:"url"`
	Title            string            `json:"title"`
	Price            string            `json:"price"`
	Currency         string            `json:"currency"`
	Location         string            `json:"location"`
	PostedAt         string            `json:"posted_at"`
	Seller           string            `json:"seller"`
	Description      string            `json:"description"`
	DescriptionFrame string            `json:"description_frame"`
	Images           []string          `json:"images"`
	Attributes       map[string]string `json:"attributes"`
	Ended            bool              `json:"ended"`
}

var amountPattern = regexp.MustCompile(`[0-9]+(?:[.,\x{00a0}\x{202f} ][0-9]+)*`)
var imageSize = regexp.MustCompile(`/s-l[0-9]+\.[a-zA-Z]+$`)

// Prices are in the displayed currency. Never perform an implicit FX conversion.
func priceAmount(text string) *float64 {
	value := amountPattern.FindString(text)
	value = strings.NewReplacer(" ", "", "\u00a0", "", "\u202f", "").Replace(value)
	if value == "" {
		return nil
	}
	last := strings.LastIndexAny(value, ",.")
	if last >= 0 {
		decimals := len(value) - last - 1
		if decimals == 1 || decimals == 2 {
			value = strings.NewReplacer(",", "", ".", "").Replace(value[:last]) + "." + value[last+1:]
		} else {
			value = strings.NewReplacer(",", "", ".", "").Replace(value)
		}
	}
	amount, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &amount
}

func displayedCurrency(text, fallback string) string {
	upper := strings.ToUpper(text)
	for _, currency := range []string{"EUR", "USD", "GBP", "CAD", "AUD", "CHF"} {
		if strings.Contains(upper, currency) {
			return currency
		}
	}
	for _, symbol := range []struct{ value, currency string }{{"€", "EUR"}, {"£", "GBP"}, {"AU $", "AUD"}, {"C $", "CAD"}, {"$", "USD"}} {
		if strings.Contains(upper, symbol.value) {
			return symbol.currency
		}
	}
	return fallback
}

func (raw rawListing) listing() (model.Listing, error) {
	canonical, id, err := CanonicalItemURL(raw.URL)
	if err != nil {
		return model.Listing{}, err
	}
	if strings.TrimSpace(raw.Title) == "" {
		return model.Listing{}, fmt.Errorf("listing title is missing")
	}
	attrs := raw.Attributes
	if attrs == nil {
		attrs = map[string]string{}
	}
	currency := displayedCurrency(raw.Price, raw.Currency)
	if currency != "" {
		attrs["Currency"] = currency
	}
	if strings.Contains(raw.Price, " bis ") || strings.Contains(raw.Price, " to ") {
		attrs["Price basis"] = "Lowest displayed variant price; selected options may cost more"
	} else if strings.Contains(strings.ToLower(attrs["Buying format"]), "auction") {
		attrs["Price basis"] = "Current auction bid; final price is unknown"
	} else {
		attrs["Price basis"] = "Displayed item price; shipping and import charges are separate"
	}
	var images []string
	seen := map[string]bool{}
	for _, image := range raw.Images {
		u, err := url.Parse(image)
		if err != nil || u.Scheme != "https" || u.User != nil || !(u.Hostname() == "ebayimg.com" || strings.HasSuffix(u.Hostname(), ".ebayimg.com")) {
			continue
		}
		key := u.Hostname() + imageSize.ReplaceAllString(u.Path, "")
		if seen[key] {
			continue
		}
		seen[key] = true
		images = append(images, image)
	}
	listing := model.Listing{Platform: ID, ID: id, URL: canonical, Title: strings.TrimSpace(raw.Title), Price: raw.Price,
		PriceAmount: priceAmount(raw.Price), Location: raw.Location, PostedAt: raw.PostedAt, Seller: raw.Seller,
		Description: raw.Description, ImageURLs: images, Attributes: attrs,
		RawText: strings.Join([]string{raw.Title, raw.Price, raw.Location, raw.Seller, raw.Description}, "\n")}
	if len(images) > 0 {
		listing.ImageURL = images[0]
	}
	return listing, nil
}
