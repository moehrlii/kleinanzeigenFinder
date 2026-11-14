// Package kleinanzeigen implements Kleinanzeigen discovery, extraction and search planning.
package kleinanzeigen

import (
	"fmt"
	"net/url"
	"strings"
)

const ID = "kleinanzeigen"
const Name = "Kleinanzeigen"

const DraftInstructions = `Create a Kleinanzeigen search. Do not invent location IDs.
If a search URL is provided, preserve it exactly. Otherwise use a broad keyword search URL
of the form https://www.kleinanzeigen.de/s-<keywords>/k0 and retain location, radius,
and budget requirements in the assessment criteria.`

func ValidateSearchURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "www.kleinanzeigen.de" || u.User != nil || !strings.HasPrefix(u.Path, "/s-") {
		return fmt.Errorf("expected a https://www.kleinanzeigen.de/s- search URL")
	}
	return nil
}
