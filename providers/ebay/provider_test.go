package ebay

import (
	"strings"
	"testing"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
)

func TestURLs(t *testing.T) {
	for _, raw := range []string{"https://www.ebay.de/sch/i.html?_nkw=lego&_udhi=200", "https://ebay.com/sch/293/i.html?LH_BIN=1"} {
		if err := ValidateSearchURL(raw); err != nil {
			t.Error(err)
		}
	}
	for _, raw := range []string{"http://www.ebay.de/sch/i.html", "https://www.ebay.de.evil.test/sch/i.html", "https://user@www.ebay.de/sch/i.html", "https://www.ebay.de:443/sch/i.html", "https://www.ebay.de/itm/123456789012", "https://www.ebay.co.uk/sch/i.html", "https://www.ebay.de/", "https://www.ebay.de/sch/foo/i.html"} {
		if ValidateSearchURL(raw) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	canonical, id, err := CanonicalItemURL("https://ebay.com/itm/LEGO-Set/123456789012?var=998877665544&tracking=abc#item")
	if err != nil || canonical != "https://www.ebay.com/itm/123456789012?var=998877665544" || id != "ebay:123456789012" {
		t.Fatalf("%s %s %v", canonical, id, err)
	}
	for _, raw := range []string{"https://www.ebay.de/itm/123456", "https://www.ebay.de/itm/123456789012/more", "https://www.ebay.de/itm/123456789012?var=bad", "https://other.test/itm/123456789012"} {
		if _, _, err := CanonicalItemURL(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestPriceAndImageNormalization(t *testing.T) {
	for _, test := range []struct {
		text, currency string
		want           float64
	}{
		{"EUR 1.234,56", "EUR", 1234.56}, {"$1,234.56", "USD", 1234.56},
		{"€ 20,93 bis € 26,73", "EUR", 20.93}, {"US $50.00 to $70.00", "USD", 50},
		{"£1,200", "GBP", 1200}, {"EUR 0,99", "EUR", .99}, {"CHF 1\u202f200,50", "CHF", 1200.5},
	} {
		t.Run(test.text, func(t *testing.T) {
			got := priceAmount(test.text)
			if got == nil || *got != test.want || displayedCurrency(test.text, "") != test.currency {
				t.Fatalf("amount=%v currency=%s", got, displayedCurrency(test.text, ""))
			}
		})
	}
	if priceAmount("Preis auf Anfrage") != nil {
		t.Fatal("invented price")
	}
	raw := rawListing{URL: "https://www.ebay.de/itm/123456789012", Title: "LEGO", Price: "EUR 20,00", Attributes: map[string]string{"Buying format": "Auction"}, Images: []string{
		"https://i.ebayimg.com/images/g/first/s-l1600.webp", "https://i.ebayimg.com/images/g/first/s-l500.jpg",
		"https://i.ebayimg.com/images/g/second/s-l1600.webp", "http://i.ebayimg.com/unsafe.jpg", "https://i.ebayimg.com.evil.test/image.jpg",
	}}
	listing, err := raw.listing()
	if err != nil || len(listing.ImageURLs) != 2 || listing.Platform != ID || listing.Attributes["Currency"] != "EUR" || !strings.Contains(listing.Attributes["Price basis"], "auction") {
		t.Fatalf("%+v %v", listing, err)
	}
}

func TestLazyLifecycle(t *testing.T) {
	s := New(config.ScraperSettings{}, config.EbaySettings{})
	if s.page != nil || s.process != nil {
		t.Fatal("constructor launched a browser")
	}
	if _, err := s.FetchDetail(model.Listing{ID: "ebay:999999999999", URL: "https://www.ebay.de/itm/123456789012"}); err == nil {
		t.Fatal("mismatched ID accepted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FetchListingsPages("https://www.ebay.de/sch/i.html", 1, 1); err == nil {
		t.Fatal("closed provider reopened")
	}
}
