package ebay

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/health"
	"github.com/mxschmitt/playwright-go"
)

// Scraper serializes requests through one browser tab and keeps the session alive.
// Browser startup is lazy so unused eBay monitors do not require a display.
type Scraper struct {
	config                        config.ScraperSettings
	settings                      config.EbaySettings
	mu                            sync.Mutex
	closed                        bool
	nextRequest                   time.Time
	blockedUntil                  time.Time
	accessFailure                 *health.Failure
	lastSearchURL                 string
	pw                            *playwright.Playwright
	browser                       playwright.Browser
	browserContext                playwright.BrowserContext
	page                          playwright.Page
	process                       *exec.Cmd
	processDone                   chan struct{}
	temporaryProfile, profileLock string
}

func New(cfg config.ScraperSettings, settings config.EbaySettings) *Scraper {
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = 30000
	}
	if cfg.DetailDelaySeconds <= 0 {
		cfg.DetailDelaySeconds = 2
	}
	return &Scraper{config: cfg, settings: settings}
}

func (s *Scraper) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.closeBrowser()
	return nil
}

func (s *Scraper) navigate(rawURL, operation string) error {
	if s.closed {
		return fmt.Errorf("eBay provider is closed")
	}
	if s.accessFailure != nil && time.Now().Before(s.blockedUntil) {
		return &health.Failure{Provider: ID, Operation: operation, Kind: s.accessFailure.Kind, Status: s.accessFailure.Status, Access: s.accessFailure.Access,
			Detail: fmt.Sprintf("session cooldown until %s after an access block; no request sent", s.blockedUntil.UTC().Format(time.RFC3339))}
	}
	if err := s.startBrowser(); err != nil {
		return err
	}
	if wait := time.Until(s.nextRequest); wait > 0 {
		time.Sleep(wait)
	}
	defer func() {
		s.nextRequest = time.Now().Add(time.Duration(max(0, s.config.DetailDelaySeconds) * float64(time.Second)))
	}()
	options := playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: new(float64(s.config.TimeoutMS))}
	if operation == "detail" && s.lastSearchURL != "" {
		options.Referer = &s.lastSearchURL
	}
	response, err := s.page.Goto(rawURL, options)
	if err != nil {
		return fmt.Errorf("eBay %s navigation: %w", operation, err)
	}
	if response == nil {
		return health.Contract(ID, operation, "navigation returned no HTTP response")
	}
	if response.Status() >= 400 {
		return s.accessError(operation, response)
	}
	selector := `.srp-results, .srp-save-null-search, .srp-main--isEmpty`
	if operation == "detail" {
		selector = `.x-item-title__mainTitle, #itemTitle, [data-testid="x-item-title"], .x-ended-listing, #vi-cvipEndedMessage`
	}
	// eBay may briefly show an automatic browser check. Let normal page navigation
	// finish; do not solve interactive CAPTCHAs, reload blocked pages, or retry them.
	if _, err := s.page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{State: playwright.WaitForSelectorStateAttached, Timeout: new(float64(s.config.TimeoutMS))}); err != nil {
		if accessErr := s.accessError(operation, response); accessErr != nil {
			return accessErr
		}
		return health.Contract(ID, operation, "expected page content did not appear before the timeout")
	}
	if err := s.accessError(operation, response); err != nil {
		return err
	}
	if _, err := marketplaceURL(s.page.URL()); err != nil {
		return health.Contract(ID, operation, "navigation left the supported eBay marketplace")
	}
	return nil
}

func (s *Scraper) accessError(operation string, response playwright.Response) error {
	raw, err := s.page.Evaluate(`() => {
 const visible = e => e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden';
 if ([...document.querySelectorAll('iframe[src*="captcha"],#captcha_form,.g-recaptcha,.h-captcha')].some(visible)) return 'captcha';
 if (document.querySelector('.srp-results li.s-card, .srp-results li.s-item, .x-item-title__mainTitle, #itemTitle')) return '';
 if (/Bitte entschuldigen Sie die Störung|Pardon Our Interruption/i.test(document.title)) return 'checking your browser';
 return document.title + '\n' + [...document.querySelectorAll('h1')].filter(visible).map(e=>e.innerText).join('\n');
}`)
	if err != nil {
		return err
	}
	text, _ := raw.(string)
	if strings.Contains(s.page.URL(), "/splashui/captcha") {
		text += " captcha"
	}
	err = s.checkAccess(operation, response.Status(), text)
	if failure, ok := errors.AsType[*health.Failure](err); ok {
		headers := response.Headers()
		failure.Access = &health.AccessDiagnostics{Server: safeServer(headers["server"]), BrowserVersion: s.browser.Version(), Runtime: runtime.GOOS}
		if response.Status() == http.StatusForbidden && failure.Access.Server == "AkamaiGHost" {
			// Retain the edge request reference, never the body that contains it.
			rawRef, refErr := s.page.Evaluate(`() => document.body?.innerText.match(/\b0\.[0-9a-f]{1,16}\.[0-9]{8,12}\.[0-9a-f]{1,16}\b/i)?.[0] || ''`)
			if refErr == nil {
				failure.Access.Reference, _ = rawRef.(string)
			}
		}
		if s.accessFailure == failure {
			if until := retryAfter(headers["retry-after"], time.Now()); until.After(s.blockedUntil) {
				s.blockedUntil = until
			}
			failure.Access.RetryAt = s.blockedUntil.UTC()
		}
	}
	return err
}

func safeServer(value string) string {
	if len(value) > 80 {
		return ""
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(" ._/-", r)) {
			return ""
		}
	}
	return value
}

func retryAfter(value string, now time.Time) time.Time {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if when, err := http.ParseTime(value); err == nil {
		return when
	}
	return time.Time{}
}

// Share access backoff across all monitors using this session. Return immediately
// during the cooldown instead of holding the runner or sending another request.
func (s *Scraper) checkAccess(operation string, status int, text string) error {
	err := health.CheckAccess(ID, operation, status, text)
	if failure, ok := errors.AsType[*health.Failure](err); ok {
		switch failure.Kind {
		case "challenge", "access_denied", "rate_limited":
			s.accessFailure = failure
			s.blockedUntil = time.Now().Add(15 * time.Minute)
		}
	}
	return err
}

func decodeResult(raw any, target any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func (s *Scraper) FetchListingsPages(searchURL string, maxResults, maxPages int) ([]model.Listing, error) {
	if err := ValidateSearchURL(searchURL); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var listings []model.Listing
	seenItems, seenPages := map[string]bool{}, map[string]bool{}
	current := searchURL
	for pageNumber := 0; current != "" && (maxPages <= 0 || pageNumber < maxPages); pageNumber++ {
		if seenPages[current] {
			break
		}
		seenPages[current] = true
		if err := s.navigate(current, "search"); err != nil {
			return listings, err
		}
		s.lastSearchURL = s.page.URL()
		raw, err := s.page.Evaluate(SearchExtractScript)
		if err != nil {
			return listings, err
		}
		var result struct {
			Recognized bool         `json:"recognized"`
			Empty      bool         `json:"empty"`
			Listings   []rawListing `json:"listings"`
			Next       string       `json:"next_page"`
		}
		if err := decodeResult(raw, &result); err != nil {
			return listings, health.Contract(ID, "search", "invalid extractor output")
		}
		if !result.Recognized {
			return listings, health.Contract(ID, "search", "search result layout was not recognized")
		}
		if result.Empty {
			return listings, nil
		}
		if len(result.Listings) == 0 {
			return listings, health.Contract(ID, "search", "result page contained no parseable listings")
		}
		added := 0
		for _, item := range result.Listings {
			listing, err := item.listing()
			if err != nil {
				return listings, health.Contract(ID, "search", err.Error())
			}
			if seenItems[listing.ID] {
				continue
			}
			seenItems[listing.ID] = true
			listings = append(listings, listing)
			added++
			if maxResults > 0 && len(listings) >= maxResults {
				return listings, nil
			}
		}
		if added == 0 {
			break
		} // Repeated pages must not loop indefinitely.
		if result.Next != "" {
			if err := ValidateSearchURL(result.Next); err != nil {
				return listings, health.Contract(ID, "search", "invalid next-page link")
			}
			before, _ := url.Parse(current)
			after, _ := url.Parse(result.Next)
			if !strings.EqualFold(before.Hostname(), after.Hostname()) {
				return listings, health.Contract(ID, "search", "pagination changed marketplace")
			}
		}
		current = result.Next
	}
	return listings, nil
}

func (s *Scraper) FetchDetail(base model.Listing) (model.Listing, error) {
	canonical, id, err := CanonicalItemURL(base.URL)
	if err != nil {
		return base, err
	}
	if base.ID != "" && base.ID != id {
		return base, fmt.Errorf("eBay listing ID does not match its URL")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startBrowser(); err != nil {
		return base, err
	}
	var descriptionStatus atomic.Int32
	onResponse := func(response playwright.Response) {
		u, err := url.Parse(response.URL())
		if err == nil && descriptionHost(u) && u.Path == "/itmdesc/"+strings.TrimPrefix(id, "ebay:") && response.Request().ResourceType() == "document" {
			descriptionStatus.Store(int32(response.Status()))
		}
	}
	s.page.OnResponse(onResponse)
	defer s.page.RemoveListener("response", onResponse)
	if err := s.navigate(canonical, "detail"); err != nil {
		return base, err
	}
	_, actualID, err := CanonicalItemURL(s.page.URL())
	if err != nil || actualID != id {
		return base, health.Contract(ID, "detail", "detail navigation changed item identity")
	}
	raw, err := s.page.Evaluate(DetailExtractScript)
	if err != nil {
		return base, err
	}
	var detail rawListing
	if err := decodeResult(raw, &detail); err != nil {
		return base, health.Contract(ID, "detail", "invalid extractor output")
	}
	if detail.Ended {
		return base, &health.Failure{Provider: ID, Operation: "detail", Kind: "not_found", Detail: "listing ended or is unavailable"}
	}
	if detail.DescriptionFrame != "" {
		u, err := url.Parse(detail.DescriptionFrame)
		if err != nil || !descriptionHost(u) {
			return base, health.Contract(ID, "detail", "unexpected seller-description frame host")
		}
		text, err := s.page.FrameLocator("iframe#desc_ifr").Locator("body").InnerText(playwright.LocatorInnerTextOptions{Timeout: new(float64(s.config.TimeoutMS))})
		if err != nil {
			return base, health.Contract(ID, "detail", "seller-description frame did not load")
		}
		frame := s.page.FrameLocator("iframe#desc_ifr")
		heading, err := frame.Locator("title, h1").AllTextContents()
		if err != nil {
			return base, health.Contract(ID, "detail", "seller-description frame could not be inspected")
		}
		status := int(descriptionStatus.Load())
		if status == 0 {
			return base, health.Contract(ID, "detail", "seller-description response was not observed")
		}
		if err := s.checkAccess("detail", status, strings.Join(heading, "\n")); err != nil {
			return base, err
		}
		if strings.TrimSpace(text) != "" {
			detail.Description = strings.TrimSpace(text)
		}
	}
	detail.URL = canonical
	listing, err := detail.listing()
	if err != nil {
		return base, health.Contract(ID, "detail", err.Error())
	}
	listing.DetailLoaded = true
	// The detail gallery supersedes search thumbnails of the same photographs.
	if len(listing.ImageURLs) > 0 {
		base.ImageURL, base.ImageURLs = "", nil
	}
	return base.MergedWith(listing), nil
}

func descriptionHost(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && u.Port() == "" && (u.Hostname() == "ebaydesc.com" || strings.HasSuffix(u.Hostname(), ".ebaydesc.com"))
}
