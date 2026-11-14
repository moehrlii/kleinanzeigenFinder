package ebay

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/health"
	"github.com/mxschmitt/playwright-go"
)

func browserTests(t *testing.T) {
	t.Helper()
	if os.Getenv("KF_BROWSER_TESTS") != "1" {
		t.Skip("set KF_BROWSER_TESTS=1 with Playwright Chromium installed; Linux requires xvfb-run")
	}
}

func fixtureProvider(t *testing.T, handler func(playwright.Route)) *Scraper {
	t.Helper()
	browserTests(t)
	pw, err := playwright.Run()
	if err != nil {
		t.Fatal(err)
	}
	s := New(config.ScraperSettings{TimeoutMS: 1000}, config.EbaySettings{})
	s.config.DetailDelaySeconds = 0 // Local intercepted fixtures do not need pacing.
	s.pw = pw
	t.Cleanup(func() { _ = s.Close() })
	s.browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: new(true)})
	if err != nil {
		t.Fatal(err)
	}
	s.browserContext, err = s.browser.NewContext(playwright.BrowserNewContextOptions{ServiceWorkers: playwright.ServiceWorkerPolicyBlock})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.browserContext.Route("**/*", handler); err != nil {
		t.Fatal(err)
	}
	s.page, err = s.browserContext.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func fulfill(t *testing.T, route playwright.Route, status int, body string) {
	t.Helper()
	u, err := url.Parse(route.Request().URL())
	_, marketplaceErr := marketplaceURL(route.Request().URL())
	if route.Request().ResourceType() != "document" || err != nil || (marketplaceErr != nil && !descriptionHost(u)) {
		_ = route.Abort()
		return
	}
	if err := route.Fulfill(playwright.RouteFulfillOptions{Status: new(status), ContentType: new("text/html; charset=utf-8"), Body: body}); err != nil {
		t.Error(err)
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBrowserSearchAndDetail(t *testing.T) {
	search, detail := fixture(t, "search.html"), fixture(t, "detail.html")
	var pageTwo atomic.Int32
	s := fixtureProvider(t, func(route playwright.Route) {
		u, _ := url.Parse(route.Request().URL())
		body := search
		if u.Host == "itm.ebaydesc.com" {
			body = `<body>Complete LEGO set with instructions. One scratched panel is shown in the gallery.</body>`
		} else if strings.HasPrefix(u.Path, "/itm/") {
			body = detail
		} else if u.Query().Get("_pgn") == "2" {
			pageTwo.Add(1)
			body = `<ul class="srp-results"><li class="s-item"><a class="s-item__link" href="/itm/223456789012"><h3 class="s-item__title">New Listing LEGO crane</h3></a><span class="s-item__price">$110.00</span><span class="s-item__detail">3 bids</span><span class="s-item__detail">Located in United States</span><span class="s-item__detail">+$5.00 shipping</span></li></ul><a class="pagination__next" href="/sch/i.html?_nkw=lego&amp;LH_BIN=1&amp;_pgn=2">Next</a>`
		}
		fulfill(t, route, 200, body)
	})
	searchURL := "https://www.ebay.de/sch/i.html?_nkw=lego&LH_BIN=1"
	for _, tc := range []struct {
		name                 string
		results, pages, want int
	}{{"page limit", 5, 1, 1}, {"result limit", 1, 2, 1}, {"pagination and loop", 5, 0, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			pageTwo.Store(0)
			listings, err := s.FetchListingsPages(searchURL, tc.results, tc.pages)
			if err != nil {
				t.Fatal(err)
			}
			if len(listings) != tc.want || pageTwo.Load() != int32(tc.want-1) {
				t.Fatalf("count=%d page2=%d", len(listings), pageTwo.Load())
			}
			first := listings[0]
			if first.Title != "LEGO Technic" || first.PriceAmount == nil || *first.PriceAmount != 42.34 || first.Location != "" || first.Attributes["Buying format"] != "Buy It Now" || first.URL != "https://www.ebay.de/itm/123456789012?var=998877665544" {
				t.Fatalf("bad search extraction: %+v", first)
			}
			if tc.want == 2 && (listings[1].Location != "United States" || listings[1].Attributes["Currency"] != "USD" || listings[1].Attributes["Buying format"] != "Auction") {
				t.Fatalf("bad legacy extraction: %+v", listings[1])
			}
		})
	}
	base := model.Listing{ID: "ebay:123456789012", URL: "https://www.ebay.de/itm/123456789012?var=998877665544", ImageURLs: []string{"https://i.ebayimg.com/images/g/first/s-l500.webp"}, Monitor: "lego"}
	listing, err := s.FetchDetail(base)
	if err != nil {
		t.Fatal(err)
	}
	if !listing.DetailLoaded || listing.ID != base.ID || listing.URL != base.URL || listing.Monitor != "lego" || listing.Seller != "fixture-seller" || listing.Location != "Berlin, Deutschland" || !strings.Contains(listing.Description, "scratched panel") || listing.PriceAmount == nil || *listing.PriceAmount != 45 {
		t.Fatalf("bad detail: %+v", listing)
	}
	if len(listing.ImageURLs) != 2 || !strings.Contains(listing.ImageURLs[0], "s-l1600") || listing.Attributes["LEGO Set Nummer"] != "42126" || listing.Attributes["Condition"] != "Gebraucht" {
		t.Fatalf("bad gallery/specifics: %+v", listing)
	}
}

func TestBrowserAccessAndChangedContracts(t *testing.T) {
	type reply struct {
		status int
		body   string
	}
	var current atomic.Value
	current.Store(reply{200, ""})
	s := fixtureProvider(t, func(route playwright.Route) { r := current.Load().(reply); fulfill(t, route, r.status, r.body) })
	for _, tc := range []struct {
		name, body, kind string
		status           int
	}{
		{"captcha", `<title>CAPTCHA</title><h1>Verify you are human</h1>`, "challenge", 200},
		{"eBay browser check", `<title>Bitte entschuldigen Sie die Störung...</title>`, "challenge", 200},
		{"visible iframe", `<iframe src="https://captcha.example/captcha"></iframe>`, "challenge", 200},
		{"forbidden", `<h1>Access denied</h1>`, "access_denied", 403},
		{"limited", `<h1>Too many requests</h1>`, "rate_limited", 429},
		{"removed", `<h1>Gone</h1>`, "not_found", 410},
		{"server", `<h1>Unavailable</h1>`, "http_error", 503},
		{"changed", `<main>New upstream layout</main>`, "contract_changed", 200},
	} {
		for _, operation := range []string{"search", "detail"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				s.blockedUntil = time.Time{} // Each case represents an independent session.
				current.Store(reply{tc.status, tc.body})
				var err error
				if operation == "search" {
					_, err = s.FetchListingsPages("https://www.ebay.de/sch/i.html", 5, 1)
				} else {
					_, err = s.FetchDetail(model.Listing{URL: "https://www.ebay.de/itm/123456789012"})
				}
				failure, ok := errors.AsType[*health.Failure](err)
				if !ok || failure.Kind != tc.kind || failure.Operation != operation {
					t.Fatalf("expected %s/%s, got %v", operation, tc.kind, err)
				}
			})
		}
	}
	for _, tc := range []struct {
		name, body string
		empty      bool
	}{
		{"zero with empty container", `<h1 class="srp-controls__count-heading">0 results for nonsense</h1><ul class="srp-results"></ul>`, true},
		{"explicit empty", `<h1 class="srp-controls__count-heading">0 Ergebnisse für nonsense</h1><ul class="srp-results"><li class="s-card">recommendation</li></ul>`, true},
		{"empty layout", `<ul class="srp-results"></ul>`, false},
		{"unsafe next", strings.ReplaceAll(fixture(t, "search.html"), `/sch/i.html?_nkw=lego&amp;LH_BIN=1&amp;_pgn=2`, `https://evil.test/sch/i.html`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current.Store(reply{200, tc.body})
			listings, err := s.FetchListingsPages("https://www.ebay.de/sch/i.html", 5, 2)
			if tc.empty {
				if err != nil || len(listings) != 0 {
					t.Fatalf("%v %v", listings, err)
				}
			} else if f, ok := errors.AsType[*health.Failure](err); !ok || f.Kind != "contract_changed" {
				t.Fatalf("expected contract failure, got %v", err)
			}
		})
	}
	current.Store(reply{200, `<div class="x-ended-listing">This listing has ended</div>`})
	if _, err := s.FetchDetail(model.Listing{URL: "https://www.ebay.de/itm/123456789012"}); err == nil {
		t.Fatal("ended listing accepted")
	}
	// A transient browser check can clear itself; do not prematurely mark it blocked.
	current.Store(reply{200, `<title>Checking your browser</title><script>setTimeout(()=>{document.title='eBay';document.body.innerHTML='<div class="srp-save-null-search">No results</div>'},30)</script>`})
	if _, err := s.FetchListingsPages("https://www.ebay.de/sch/i.html", 5, 1); err != nil {
		t.Fatal(err)
	}
}

func TestNativeBrowserLaunchAndProfileOwnership(t *testing.T) {
	browserTests(t)
	profile := t.TempDir()
	settings := config.EbaySettings{ProfileDir: profile}
	s := New(config.ScraperSettings{}, settings)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.startBrowser(); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := s.page.Evaluate(`() => ({automated:navigator.webdriver,ua:navigator.userAgent})`)
	if err != nil {
		t.Fatal(err)
	}
	var fp struct {
		Automated bool   `json:"automated"`
		UA        string `json:"ua"`
	}
	if err := decodeResult(fingerprint, &fp); err != nil {
		t.Fatal(err)
	}
	if fp.Automated || strings.Contains(fp.UA, "Headless") || !strings.Contains(fp.UA, "Chrome/") {
		t.Fatalf("unexpected fingerprint: %+v", fp)
	}
	second := New(config.ScraperSettings{}, settings)
	t.Cleanup(func() { _ = second.Close() })
	if err := second.startBrowser(); err == nil || !strings.Contains(err.Error(), "profile is in use") {
		t.Fatalf("profile collision: %v", err)
	}
	if _, err := os.Stat(filepath.Join(profile, "kf-provider.lock")); err != nil {
		t.Fatal("failed second owner removed first owner's lock")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatal("configured profile removed")
	}
	if _, err := os.Stat(filepath.Join(profile, "kf-provider.lock")); !os.IsNotExist(err) {
		t.Fatal("profile lock retained after shutdown")
	}
	// A new provider can reuse the profile after a clean shutdown.
	third := New(config.ScraperSettings{}, settings)
	t.Cleanup(func() { _ = third.Close() })
	if err := third.startBrowser(); err != nil {
		t.Fatal(err)
	}
}

func TestDescriptionFrameFailures(t *testing.T) {
	detail := fixture(t, "detail.html")
	var frameStatus atomic.Int32
	frameStatus.Store(403)
	s := fixtureProvider(t, func(route playwright.Route) {
		if strings.Contains(route.Request().URL(), "itm.ebaydesc.com/") {
			status := int(frameStatus.Load())
			body := "<h1>Access denied</h1>"
			if status == 200 {
				body = "<title>CAPTCHA</title><h1>Verify you are human</h1>"
			}
			fulfill(t, route, status, body)
		} else {
			fulfill(t, route, 200, detail)
		}
	})
	for _, tc := range []struct {
		status int
		kind   string
	}{{403, "access_denied"}, {200, "challenge"}} {
		s.blockedUntil = time.Time{}
		frameStatus.Store(int32(tc.status))
		_, err := s.FetchDetail(model.Listing{URL: "https://www.ebay.de/itm/123456789012"})
		if failure, ok := errors.AsType[*health.Failure](err); !ok || failure.Kind != tc.kind {
			t.Fatalf("description block accepted: %v", err)
		}
	}
}

func TestStructuredDetailFallback(t *testing.T) {
	s := fixtureProvider(t, func(route playwright.Route) {
		fulfill(t, route, 200, `<h1 class="x-item-title__mainTitle">LEGO</h1><script type="application/ld+json">[{"@type":"Product","name":"LEGO","description":"Complete <b>used</b> set with instructions and original box.","image":["https://i.ebayimg.com/images/g/fixture/s-l1600.jpg"],"offers":{"url":"https://www.ebay.com/itm/123456789012","price":"1234.56","priceCurrency":"USD","itemCondition":"https://schema.org/UsedCondition"}}]</script><iframe style="display:none" src="https://captcha.example/hidden-captcha"></iframe>`)
	})
	listing, err := s.FetchDetail(model.Listing{URL: "https://www.ebay.com/itm/123456789012"})
	if err != nil {
		t.Fatal(err)
	}
	if listing.PriceAmount == nil || *listing.PriceAmount != 1234.56 || listing.Attributes["Currency"] != "USD" || listing.Attributes["Condition"] != "UsedCondition" || !strings.Contains(listing.Description, "Complete used set") || len(listing.ImageURLs) != 1 {
		t.Fatalf("bad structured fallback: %+v", listing)
	}
}

func TestBlockedSessionDoesNotRepeatRequests(t *testing.T) {
	var requests atomic.Int32
	s := fixtureProvider(t, func(route playwright.Route) { requests.Add(1); fulfill(t, route, 403, "<h1>Access denied</h1>") })
	if _, err := s.FetchListingsPages("https://www.ebay.de/sch/i.html", 5, 1); err == nil {
		t.Fatal("expected denial")
	}
	if _, err := s.FetchDetail(model.Listing{URL: "https://www.ebay.de/itm/123456789012"}); err == nil || !strings.Contains(err.Error(), "no request sent") {
		t.Fatalf("missing cooldown: %v", err)
	}
	if _, err := s.FetchListingsPages("https://www.ebay.com/sch/i.html", 5, 1); err == nil {
		t.Fatal("another monitor bypassed cooldown")
	}
	if requests.Load() != 1 {
		t.Fatalf("blocked session sent %d requests", requests.Load())
	}
	if time.Until(s.blockedUntil) < 14*time.Minute {
		t.Fatal("cooldown too short")
	}
}

func TestAccessDiagnosticsAndRetryAfter(t *testing.T) {
	var requests atomic.Int32
	var delay atomic.Value
	delay.Store("3600")
	const reference = "0.abcdef12.1794672000.1234abcd"
	s := fixtureProvider(t, func(route playwright.Route) {
		if route.Request().ResourceType() != "document" {
			_ = route.Abort()
			return
		}
		requests.Add(1)
		if err := route.Fulfill(playwright.RouteFulfillOptions{
			Status: new(403), ContentType: new("text/html"),
			Headers: map[string]string{"server": "AkamaiGHost", "retry-after": delay.Load().(string), "set-cookie": "private-cookie=secret-value"},
			Body:    "<title>Error Page | eBay</title><h1>SORRY</h1><p>" + reference + "</p><p>private-body-text</p>",
		}); err != nil {
			t.Error(err)
		}
	})
	for _, tc := range []struct {
		name, header string
		minimum      time.Duration
	}{{"longer server delay", "3600", time.Hour}, {"retain minimum cooldown", "1", 15 * time.Minute}} {
		t.Run(tc.name, func(t *testing.T) {
			s.blockedUntil = time.Time{}
			delay.Store(tc.header)
			before := time.Now()
			_, err := s.FetchListingsPages("https://www.ebay.de/sch/i.html", 5, 1)
			failure, ok := errors.AsType[*health.Failure](err)
			if !ok || failure.Access == nil {
				t.Fatalf("missing diagnostics: %v", err)
			}
			access := failure.Access
			if access.Server != "AkamaiGHost" || access.Reference != reference || access.Runtime != runtime.GOOS || access.BrowserVersion == "" || access.RetryAt.Before(before.Add(tc.minimum)) {
				t.Fatalf("incorrect diagnostics: %+v", access)
			}
			encoded, err := json.Marshal(failure)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"private-cookie", "secret-value", "private-body-text"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("diagnostics retained %s", secret)
				}
			}
			count := requests.Load()
			_, err = s.FetchDetail(model.Listing{URL: "https://www.ebay.de/itm/123456789012"})
			blocked, ok := errors.AsType[*health.Failure](err)
			if !ok || blocked.Access != access || requests.Load() != count {
				t.Fatalf("cooldown lost diagnostics or sent another request: %v", err)
			}
		})
	}
}
