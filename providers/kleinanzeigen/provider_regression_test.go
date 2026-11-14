package kleinanzeigen

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/health"
	"github.com/playwright-community/playwright-go"
)

func fixtureProvider(t *testing.T, handler func(playwright.Route)) *KleinanzeigenScraper {
	t.Helper()
	if os.Getenv("KF_BROWSER_TESTS") != "1" {
		t.Skip("set KF_BROWSER_TESTS=1 with Playwright Chromium installed")
	}
	pw, err := playwright.Run()
	if err != nil {
		t.Fatal(err)
	}
	s := &KleinanzeigenScraper{pw: pw, config: config.ScraperSettings{Headless: true, TimeoutMS: 1000}}
	t.Cleanup(func() { _ = s.Close() })
	s.browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: new(true)})
	if err != nil {
		t.Fatal(err)
	}
	s.context, err = s.browser.NewContext(playwright.BrowserNewContextOptions{ServiceWorkers: playwright.ServiceWorkerPolicyBlock})
	if err != nil {
		t.Fatal(err)
	}
	// Every request is intercepted, including images and unexpected third-party URLs.
	if err := s.context.Route("**/*", handler); err != nil {
		t.Fatal(err)
	}
	return s
}

func fulfill(t *testing.T, route playwright.Route, status int, body string) {
	t.Helper()
	if route.Request().ResourceType() != "document" {
		_ = route.Abort()
		return
	}
	if err := route.Fulfill(playwright.RouteFulfillOptions{Status: new(status), ContentType: new("text/html; charset=utf-8"), Body: body}); err != nil {
		t.Error(err)
	}
}

func TestBrowserProviderContract(t *testing.T) {
	search, err := os.ReadFile("testdata/search.html")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := os.ReadFile("testdata/detail.html")
	if err != nil {
		t.Fatal(err)
	}
	var secondPageRequests atomic.Int32
	s := fixtureProvider(t, func(route playwright.Route) {
		body := string(search)
		if strings.Contains(route.Request().URL(), "/s-anzeige/") {
			body = string(detail)
		} else if strings.Contains(route.Request().URL(), "/seite:2/") {
			secondPageRequests.Add(1)
			body = `<main id="srchrslt-results"><article data-adid="456"><a href="/s-anzeige/second/456-203-1">Zweite Wohnung</a><div class="price">900 EUR</div></article></main>`
		}
		fulfill(t, route, 200, body)
	})
	searchURL := "https://www.kleinanzeigen.de/s-wohnung-mieten/berlin/c203l3331"
	for _, test := range []struct {
		name                       string
		maxResults, maxPages, want int
	}{
		{"page bound", 5, 1, 1},
		{"result bound", 1, 2, 1},
		{"pagination", 5, 2, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			secondPageRequests.Store(0)
			listings, err := s.FetchListingsPages(searchURL, test.maxResults, test.maxPages)
			if err != nil {
				t.Fatal(err)
			}
			if len(listings) != test.want || secondPageRequests.Load() != int32(test.want-1) {
				t.Fatalf("listings=%d, second-page requests=%d", len(listings), secondPageRequests.Load())
			}
			first := listings[0]
			if first.ID != "123" || first.Platform != ID || first.PriceAmount == nil || *first.PriceAmount != 850 || first.Location != "10115 Berlin Mitte" {
				t.Fatalf("search extraction changed: %+v", first)
			}
		})
	}
	listing, err := s.FetchDetail(model.Listing{ID: "123", Platform: ID, URL: "https://www.kleinanzeigen.de/s-anzeige/wohnung/123-203-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !listing.DetailLoaded || listing.ID != "123" || listing.Title != "Helle Wohnung mit Balkon" || listing.PriceAmount == nil || *listing.PriceAmount != 950 || !strings.Contains(listing.Description, "Einbauküche") || len(listing.ImageURLs) != 2 {
		t.Fatalf("detail extraction changed: %+v", listing)
	}
}

func TestBrowserAccessFailures(t *testing.T) {
	tests := []struct {
		name, body, kind string
		status           int
	}{
		{"captcha", `<title>CAPTCHA</title><h1>Verify you are human</h1>`, "challenge", 200},
		{"challenge iframe", `<iframe src="https://geo.captcha-delivery.com/captcha/"></iframe>`, "challenge", 200},
		{"forbidden", `<h1>Access denied</h1>`, "access_denied", 403},
		{"soft forbidden", `<h1>Zugriff verweigert</h1>`, "access_denied", 200},
		{"rate limited", `<h1>Too many requests</h1>`, "rate_limited", 429},
		{"removed", `<h1>Gone</h1>`, "not_found", 410},
		{"server error", `<h1>Unavailable</h1>`, "http_error", 503},
	}
	s := fixtureProvider(t, func(route playwright.Route) {
		u, _ := url.Parse(route.Request().URL())
		for _, test := range tests {
			if u.Query().Get("case") == test.name {
				fulfill(t, route, test.status, test.body)
				return
			}
		}
		_ = route.Abort()
	})
	for _, test := range tests {
		for _, operation := range []string{"search", "detail"} {
			t.Run(test.name+"/"+operation, func(t *testing.T) {
				query := "?case=" + url.QueryEscape(test.name)
				var err error
				if operation == "search" {
					_, err = s.FetchListingsPages("https://www.kleinanzeigen.de/s-test/k0"+query, 5, 1)
				} else {
					_, err = s.FetchDetail(model.Listing{URL: "https://www.kleinanzeigen.de/s-anzeige/test/123-203-1" + query})
				}
				failure, ok := errors.AsType[*health.Failure](err)
				if !ok || failure.Kind != test.kind || failure.Operation != operation || failure.Status != test.status {
					t.Fatalf("expected %s %s HTTP %d; got %v", operation, test.kind, test.status, err)
				}
			})
		}
	}
}

func TestBrowserEmptySearchAndChangedDetail(t *testing.T) {
	s := fixtureProvider(t, func(route playwright.Route) {
		fulfill(t, route, 200, `<main id="srchrslt-results">Keine Ergebnisse</main>`)
	})
	listings, err := s.FetchListingsPages("https://www.kleinanzeigen.de/s-test/k0", 5, 1)
	if err != nil || len(listings) != 0 {
		t.Fatalf("genuine empty results should be accepted: %v", err)
	}
	_, err = s.FetchDetail(model.Listing{URL: "https://www.kleinanzeigen.de/s-anzeige/test/123-203-1"})
	if failure, ok := errors.AsType[*health.Failure](err); !ok || failure.Kind != "contract_changed" {
		t.Fatalf("blank detail must not be marked loaded: %v", err)
	}
}
