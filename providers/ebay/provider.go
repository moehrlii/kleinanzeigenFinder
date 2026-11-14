// Package ebay discovers public eBay listings with an isolated Chrome session.
package ebay

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const ID = "ebay"
const Name = "eBay"

const DraftInstructions = `Create an eBay search on https://www.ebay.de/sch/i.html
or https://www.ebay.com/sch/i.html. Preserve a supplied search URL exactly.
For generated searches encode keywords in _nkw and use _sop=10 for newly listed items.
Use _udlo and _udhi for item-price bounds, LH_BIN=1 for Buy It Now, LH_Auction=1 for
auctions, LH_ItemCondition=1000 for new or 3000 for used. Never invent category IDs.
Do not silently exclude auctions unless requested. On ebay.de LH_PrefLoc=1 restricts
item location to Germany. Other geography constraints belong in criteria when uncertain.
Distinguish current auction bids, fixed prices, and the cheapest variant in a price range.
Shipping, import charges and optional variants are not included in the item price.
Search price bounds use the marketplace currency (normally EUR on .de, USD on .com),
but international detail pages can show the seller's original currency instead.
Leave min_price and max_price local rules null for eBay; use search URL bounds and
explicit budget/currency criteria instead of comparing amounts across currencies.
Retain delivered-budget, condition, seller, authenticity, visual
and compatibility constraints in criteria. An offer may be an accessory rather than
the requested product. Do not treat stock images as evidence of the actual used item's condition.`

var itemPath = regexp.MustCompile(`^/itm/(?:[^/]+/)?([0-9]{9,15})/?$`)
var searchPath = regexp.MustCompile(`^/sch/(?:[0-9]+/)?i\.html$`)
var variationID = regexp.MustCompile(`^[0-9]+$`)

func marketplaceURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return nil, fmt.Errorf("expected an HTTPS eBay URL without credentials or a port")
	}
	switch strings.ToLower(u.Host) {
	case "ebay.de", "www.ebay.de", "ebay.com", "www.ebay.com":
		return u, nil
	default:
		return nil, fmt.Errorf("unsupported eBay marketplace host")
	}
}

func ValidateSearchURL(raw string) error {
	u, err := marketplaceURL(raw)
	if err != nil {
		return err
	}
	if !searchPath.MatchString(u.Path) {
		return fmt.Errorf("expected an eBay /sch/i.html or /sch/<category>/i.html search URL")
	}
	return nil
}

// CanonicalItemURL drops tracking parameters but retains an explicitly selected variation.
func CanonicalItemURL(raw string) (string, string, error) {
	u, err := marketplaceURL(raw)
	if err != nil {
		return "", "", err
	}
	match := itemPath.FindStringSubmatch(u.Path)
	if match == nil {
		return "", "", fmt.Errorf("expected an eBay item URL with a numeric item ID")
	}
	query := url.Values{}
	if variant := u.Query().Get("var"); variant != "" {
		if !variationID.MatchString(variant) {
			return "", "", fmt.Errorf("invalid eBay variation ID")
		}
		query.Set("var", variant)
	}
	u.Host = "www." + strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	u.Path, u.RawPath, u.RawQuery, u.Fragment = "/itm/"+match[1], "", query.Encode(), ""
	return u.String(), "ebay:" + match[1], nil
}
