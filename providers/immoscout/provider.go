// Package immoscout implements ImmoScout24 discovery through its mobile API.
package immoscout

import (
	"fmt"
	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"net/url"
	"strings"
)

const ID = "immoscout"
const Name = "ImmoScout24"

const DraftInstructions = `Create an ImmoScout24 property search. Preserve a supplied URL exactly.
Supported regional searches use https://www.immobilienscout24.de/Suche/de/<state>/<city>/wohnung-mieten
(or haus-mieten, wohnung-kaufen, haus-kaufen). Use actual geographic slugs only when known.
For nationwide apartment rental searches use /Suche/de/wohnung-mieten and retain uncertain location
constraints in criteria. Radius searches use /Suche/radius/wohnung-mieten with
geocoordinates=latitude;longitude;radius, only when coordinates are supplied or known.
Supported filters include price=min-max, numberofrooms=min-, livingspace=min-,
equipment=balcony,builtinkitchen and exclusioncriteria=swapflat. sorting=2 means newest first.
For a warm-rent price limit specify pricetype=calculatedtotalrent. Never silently equate cold and warm rent.
Shape searches are unsupported. Preserve constraints that cannot be encoded in the assessment criteria.`

func validateSearchHost(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return fmt.Errorf("expected an HTTPS ImmoScout24 search URL")
	}
	switch strings.ToLower(u.Host) {
	case "www.immobilienscout24.de", "immobilienscout24.de", "api.mobile.immobilienscout24.de":
		return nil
	default:
		return fmt.Errorf("unsupported ImmoScout24 search host %q", u.Host)
	}
}

func ValidateSearchURL(raw string) error {
	_, err := New(config.ImmoScoutSettings{}).translateSearchURL(raw)
	return err
}
