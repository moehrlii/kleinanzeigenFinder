package providercheck

import (
	"testing"

	"github.com/Kamuyin/kleinanzeigenFinder/model"
)

func TestSearchContractDetectsExtractionRegressions(t *testing.T) {
	p := probe{ID: "kleinanzeigen", IDPattern: "^[0-9]+$"}
	valid := model.Listing{
		Platform: p.ID, ID: "123", URL: "https://www.kleinanzeigen.de/s-anzeige/wohnung/123-203-1",
		Title: "Apartment", Location: "Berlin", PriceAmount: new(850.0),
	}
	for _, test := range []struct {
		name   string
		change func(*model.Listing)
	}{
		{"wrong platform", func(l *model.Listing) { l.Platform = "other" }},
		{"changed identity", func(l *model.Listing) { l.ID = "https://example.com" }},
		{"blank title", func(l *model.Listing) { l.Title = " " }},
		{"URL fallback title", func(l *model.Listing) { l.Title = l.URL }},
		{"login redirect", func(l *model.Listing) { l.URL = "https://login.example.com/" }},
		{"insecure URL", func(l *model.Listing) { l.URL = "http://www.kleinanzeigen.de/" }},
		{"lost location", func(l *model.Listing) { l.Location = "" }},
		{"lost price", func(l *model.Listing) { l.PriceAmount = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			listing := valid
			test.change(&listing)
			if err := validateSearch(p, []string{"www.kleinanzeigen.de"}, []model.Listing{listing}); err == nil {
				t.Fatal("broken extraction passed the live contract")
			}
		})
	}
	for _, listings := range [][]model.Listing{nil, {valid, valid}} {
		if err := validateSearch(p, []string{"www.kleinanzeigen.de"}, listings); err == nil {
			t.Fatal("empty or duplicate results passed")
		}
	}
	if err := validateSearch(p, []string{"www.kleinanzeigen.de"}, []model.Listing{valid}); err != nil {
		t.Fatal(err)
	}
}
