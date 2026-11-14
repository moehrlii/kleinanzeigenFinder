package kleinanzeigen

import (
	"fmt"
	"sync"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/health"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/kleinanzeigen/parsing"
	"github.com/playwright-community/playwright-go"
	"github.com/sirupsen/logrus"
)

type KleinanzeigenScraper struct {
	pw      *playwright.Playwright
	browser playwright.Browser
	context playwright.BrowserContext
	config  config.ScraperSettings

	requestMu    sync.Mutex
	nextDetailAt time.Time
}

func New(cfg config.ScraperSettings) (*KleinanzeigenScraper, error) {
	err := playwright.Install(&playwright.RunOptions{
		Browsers: []string{"chromium"},
	})
	if err != nil {
		logrus.Warnf("playwright.Install() error: %v", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("could not start playwright: %w", err)
	}

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(cfg.Headless),
	})
	if err != nil {
		return nil, fmt.Errorf("could not launch browser: %w", err)
	}

	ctxOpts := playwright.BrowserNewContextOptions{
		Locale:     playwright.String("de-DE"),
		TimezoneId: playwright.String("Europe/Berlin"),
		Viewport: &playwright.Size{
			Width:  1280,
			Height: 900,
		},
	}
	if cfg.UserAgent != "" {
		ctxOpts.UserAgent = playwright.String(cfg.UserAgent)
	}

	context, err := browser.NewContext(ctxOpts)
	if err != nil {
		return nil, fmt.Errorf("could not create context: %w", err)
	}

	return &KleinanzeigenScraper{
		pw:      pw,
		browser: browser,
		context: context,
		config:  cfg,
	}, nil
}

func (s *KleinanzeigenScraper) Close() error {
	if s.context != nil {
		_ = s.context.Close()
	}
	if s.browser != nil {
		_ = s.browser.Close()
	}
	if s.pw != nil {
		_ = s.pw.Stop()
	}
	return nil
}

func (s *KleinanzeigenScraper) FetchListings(searchURL string, maxResults int) ([]model.Listing, error) {
	return s.FetchListingsPages(searchURL, maxResults, 0)
}

func (s *KleinanzeigenScraper) FetchListingsPages(searchURL string, maxResults, maxPages int) ([]model.Listing, error) {
	if err := ValidateSearchURL(searchURL); err != nil {
		return nil, err
	}
	page, err := s.context.NewPage()
	if err != nil {
		return nil, err
	}
	defer page.Close()

	var listings []model.Listing
	currentURL := searchURL
	pageCount := 0
	unlimited := maxResults <= 0
	visitedPages := make(map[string]bool)

	for currentURL != "" && (unlimited || len(listings) < maxResults) && (maxPages <= 0 || pageCount < maxPages) {
		if visitedPages[currentURL] {
			logrus.Warnf("Stopping pagination because page URL was already visited: %s", currentURL)
			break
		}
		visitedPages[currentURL] = true
		pageCount++
		logrus.Debugf("Fetching page %d: %s", pageCount, currentURL)

		response, err := page.Goto(currentURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			Timeout:   playwright.Float(float64(s.config.TimeoutMS)),
		})
		if err != nil {
			return listings, err
		}
		if response == nil {
			return listings, health.Contract(ID, "search", "navigation returned no HTTP response")
		}
		if err := checkPageAccess(page, response.Status(), "search"); err != nil {
			return listings, err
		}

		if pageCount == 1 {
			s.dismissCookieDialog(page)
		}

		// Wait for results
		if _, err := page.WaitForSelector("article[data-adid], article.aditem, li.ad-listitem, #srchrslt-results", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(float64(s.config.TimeoutMS)),
		}); err != nil {
			page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
				State:   playwright.LoadStateNetworkidle,
				Timeout: playwright.Float(6000),
			})
		}

		if err := checkPageAccess(page, response.Status(), "search"); err != nil {
			return listings, err
		}
		rawOutput, err := page.Evaluate(CardExtractScript)
		if err != nil {
			return listings, err
		}

		outputMap, ok := rawOutput.(map[string]interface{})
		if !ok {
			return listings, health.Contract(ID, "search", "extractor returned a non-object")
		}

		itemsArray, ok := outputMap["listings"].([]interface{})
		if !ok {
			return listings, health.Contract(ID, "search", "extractor omitted the listings array")
		}

		nextPageStr := ""
		if next, ok := outputMap["next_page"].(string); ok && next != "" {
			nextPageStr = next
		}

		pageListings := 0
		for _, item := range itemsArray {
			if !unlimited && len(listings) >= maxResults {
				break
			}
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			listing := s.listingFromRaw(itemMap)
			if listing.ID != "" && listing.URL != "" {
				listings = append(listings, listing)
				pageListings++
			}
		}

		logrus.Debugf("Extracted %d valid listing(s) from page %d. Total so far: %d", pageListings, pageCount, len(listings))

		if !unlimited && len(listings) >= maxResults {
			if nextPageStr != "" {
				logrus.Infof("Reached max_results=%d before exhausting search pages; next page would be %s", maxResults, nextPageStr)
			}
			break
		}
		if nextPageStr == "" {
			break
		}
		if visitedPages[nextPageStr] {
			logrus.Warnf("Stopping pagination because next page URL was already visited: %s", nextPageStr)
			break
		}

		time.Sleep(time.Duration(s.config.DetailDelaySeconds * float64(time.Second)))
		currentURL = nextPageStr
	}

	return listings, nil
}

func (s *KleinanzeigenScraper) FetchDetail(listing model.Listing) (model.Listing, error) {
	s.waitBeforeDetailRequest()

	page, err := s.context.NewPage()
	if err != nil {
		return listing, err
	}
	defer page.Close()

	response, err := page.Goto(listing.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(float64(s.config.TimeoutMS)),
	})
	if err != nil {
		return listing, err
	}
	if response == nil {
		return listing, health.Contract(ID, "detail", "navigation returned no HTTP response")
	}
	if err := checkPageAccess(page, response.Status(), "detail"); err != nil {
		return listing, err
	}

	s.dismissCookieDialog(page)
	page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State:   playwright.LoadStateNetworkidle,
		Timeout: playwright.Float(6000),
	})

	if err := checkPageAccess(page, response.Status(), "detail"); err != nil {
		return listing, err
	}
	raw, err := page.Evaluate(DetailExtractScript)
	if err != nil {
		return listing, err
	}

	rawMap, ok := raw.(map[string]interface{})
	if ok {
		if strOrEmpty(rawMap["title"]) == "" || strOrEmpty(rawMap["id"]) == "" {
			return listing, health.Contract(ID, "detail", "listing title or identity missing from detail page")
		}
		detail := s.listingFromRaw(rawMap)
		detail.DetailLoaded = true
		return listing.MergedWith(detail), nil
	}
	return listing, health.Contract(ID, "detail", "extractor returned a non-object")
}

func (s *KleinanzeigenScraper) waitBeforeDetailRequest() {
	delay := time.Duration(s.config.DetailDelaySeconds * float64(time.Second))
	if delay <= 0 {
		return
	}

	s.requestMu.Lock()
	now := time.Now()
	wait := s.nextDetailAt.Sub(now)
	if wait < 0 {
		wait = 0
	}
	s.nextDetailAt = now.Add(wait + delay)
	s.requestMu.Unlock()

	if wait > 0 {
		time.Sleep(wait)
	}
}

func (s *KleinanzeigenScraper) dismissCookieDialog(page playwright.Page) {
	candidates := []string{
		"Alle Cookies und Tracking akzeptieren",
		"Alle akzeptieren",
		"Akzeptieren",
	}

	for _, text := range candidates {
		button := page.GetByRole("button", playwright.PageGetByRoleOptions{
			Name:  text,
			Exact: playwright.Bool(false),
		})
		count, err := button.Count()
		if err == nil && count > 0 {
			button.First().Click(playwright.LocatorClickOptions{
				Timeout: playwright.Float(3000),
			})
			time.Sleep(500 * time.Millisecond)
			logrus.Debugf("Dismissed cookie dialog with button text: %s", text)
			return
		}
	}
}

func (s *KleinanzeigenScraper) listingFromRaw(item map[string]interface{}) model.Listing {
	url := strOrEmpty(item["url"])
	id := strOrEmpty(item["id"])
	if id == "" {
		id = parsing.ExtractIDFromURL(url)
	}
	if id == "" {
		id = url
	}

	rawText := strOrEmpty(item["raw_text"])
	price := strOrEmpty(item["price"])

	attributes := make(map[string]string)
	if attrs, ok := item["attributes"].(map[string]interface{}); ok {
		for k, v := range attrs {
			if v != nil {
				attributes[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	var images []string
	if rawImages, ok := item["image_urls"].([]any); ok {
		for _, image := range rawImages {
			if value, ok := image.(string); ok && value != "" {
				images = append(images, value)
			}
		}
	}
	listing := model.Listing{
		Platform:    ID,
		ImageURLs:   images,
		ID:          id,
		URL:         url,
		Title:       strOrFallback(item["title"], url),
		Price:       price,
		Location:    strOrEmpty(item["location"]),
		PostedAt:    strOrEmpty(item["posted_at"]),
		Description: strOrEmpty(item["description"]),
		ImageURL:    strOrEmpty(item["image_url"]),
		Seller:      strOrEmpty(item["seller"]),
		RawText:     rawText,
		Attributes:  attributes,
	}

	// Calculate PriceAmount
	priceAmount := parsing.ParsePriceAmount(attributes["Warmmiete"])
	if priceAmount == nil {
		priceAmount = parsing.ParsePriceAmount(price)
	}
	if priceAmount == nil {
		priceAmount = parsing.ParsePriceAmount(rawText)
	}
	listing.PriceAmount = priceAmount

	rooms := parsing.ParseRooms(rawText)
	size := parsing.ParseSizeM2(rawText)

	if rooms != nil {
		if _, exists := listing.Attributes["Zimmer"]; !exists {
			listing.Attributes["Zimmer"] = fmt.Sprintf("%g", *rooms)
		}
	}
	if size != nil {
		if _, exists := listing.Attributes["Wohnfläche"]; !exists {
			listing.Attributes["Wohnfläche"] = fmt.Sprintf("%g m²", *size)
		}
	}

	return listing
}

func strOrEmpty(val interface{}) string {
	if val == nil {
		return ""
	}
	return fmt.Sprintf("%v", val)
}

func strOrFallback(val interface{}, fallback string) string {
	str := strOrEmpty(val)
	if str == "" {
		return fallback
	}
	return str
}
