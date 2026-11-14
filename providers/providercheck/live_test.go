// Package providercheck tests the public contract through the real provider factories.
package providercheck

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/providers"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/health"
)

//go:embed probes.json
var probeJSON []byte

type probe struct {
	ID        string `json:"id"`
	Browser   bool   `json:"browser"`
	SearchURL string `json:"search_url"`
	IDPattern string `json:"id_pattern"`
}

func probes(t *testing.T) []probe {
	t.Helper()
	var result []probe
	if err := json.Unmarshal(probeJSON, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// This offline test prevents silently omitting future integrations from the schedule.
func TestLiveProbeCoverage(t *testing.T) {
	registry := providers.DefaultRegistry()
	seen := map[string]bool{}
	for _, p := range probes(t) {
		if seen[p.ID] {
			t.Errorf("duplicate live probe: %s", p.ID)
		}
		seen[p.ID] = true
		if _, err := registry.Resolve(p.ID, p.SearchURL); err != nil {
			t.Error(err)
		}
		if p.IDPattern == "" {
			t.Errorf("missing identity contract for %s", p.ID)
		} else if _, err := regexp.Compile(p.IDPattern); err != nil {
			t.Error(err)
		}
	}
	for _, provider := range registry.Available() {
		if !seen[provider.ID] {
			t.Errorf("add %s to probes.json before enabling it", provider.ID)
		}
	}
}

type report struct {
	Provider         string    `json:"provider"`
	Started          time.Time `json:"started"`
	DurationSeconds  float64   `json:"duration_seconds"`
	Outcome          string    `json:"outcome"`
	Stage            string    `json:"stage"`
	Kind             string    `json:"kind,omitempty"`
	Message          string    `json:"message,omitempty"`
	HTTPStatus       int       `json:"http_status,omitzero"`
	Listings         int       `json:"listings"`
	DetailsAttempted int       `json:"details_attempted"`
	RemovedListings  int       `json:"removed_listings"`
	DescriptionChars int       `json:"description_chars"`
	GalleryImages    int       `json:"gallery_images"`
}

func TestLiveProviders(t *testing.T) {
	if os.Getenv("KF_LIVE_PROVIDERS") != "1" {
		t.Skip("set KF_LIVE_PROVIDERS=1 to contact public marketplaces")
	}
	selected := os.Getenv("KF_LIVE_PROVIDER")
	matched := false
	for _, p := range probes(t) {
		if selected != "" && selected != p.ID {
			continue
		}
		matched = true
		t.Run(p.ID, func(t *testing.T) { runLiveProbe(t, p) })
	}
	if !matched {
		t.Fatalf("unknown KF_LIVE_PROVIDER %q; no checks ran", selected)
	}
}

func runLiveProbe(t *testing.T, p probe) {
	t.Helper()
	if p.Browser {
		// The provider installs its pinned browser; keep other local projects' versions.
		t.Setenv("PLAYWRIGHT_SKIP_BROWSER_GC", "1")
	}
	r := report{Provider: p.ID, Started: time.Now().UTC(), Stage: "initialize", Outcome: "running"}
	t.Cleanup(func() {
		r.DurationSeconds = time.Since(r.Started).Seconds()
		r.Outcome = "passed"
		if t.Failed() {
			r.Outcome = "failed"
		}
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		t.Log(string(data))
		if dir := os.Getenv("KF_PROVIDER_REPORT_DIR"); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Error(err)
				return
			}
			if err := os.WriteFile(filepath.Join(dir, p.ID+".json"), data, 0600); err != nil {
				t.Error(err)
			}
		}
	})
	fail := func(err error) {
		t.Helper()
		r.Kind, r.Message = "network_or_runtime", err.Error()
		if failure, ok := errors.AsType[*health.Failure](err); ok {
			r.Kind, r.HTTPStatus = failure.Kind, failure.Status
		}
		t.Fatal(err)
	}
	definition, err := providers.DefaultRegistry().Resolve(p.ID, p.SearchURL)
	if err != nil {
		fail(err)
	}
	provider, err := definition.Open(providers.Settings{
		Browser:   config.ScraperSettings{Headless: true, TimeoutMS: 25000, DetailDelaySeconds: 2},
		ImmoScout: config.ImmoScoutSettings{PageSize: 10},
	})
	if err != nil {
		fail(err)
	}
	t.Cleanup(func() {
		if err := provider.Close(); err != nil {
			t.Error(err)
		}
	})
	r.Stage = "search"
	// A broad rental search avoids dependence on one listing surviving until next week.
	listings, err := provider.FetchListingsPages(p.SearchURL, 5, 1)
	if err != nil {
		fail(err)
	}
	r.Listings = len(listings)
	if err := validateSearch(p, definition.Hosts, listings); err != nil {
		fail(err)
	}
	r.Stage = "detail"
	for _, listing := range listings[:min(3, len(listings))] {
		r.DetailsAttempted++
		// Prevent the merge from hiding a broken detail extractor behind search snippets/photos.
		listing.Description, listing.ImageURL, listing.ImageURLs = "", "", nil
		listing.DetailLoaded = false
		detail, err := provider.FetchDetail(listing)
		if err != nil {
			if failure, ok := errors.AsType[*health.Failure](err); ok && failure.Kind == "not_found" {
				r.RemovedListings++
				continue
			}
			fail(err)
		}
		if !detail.DetailLoaded || detail.ID != listing.ID || detail.Platform != p.ID || detail.URL != listing.URL {
			fail(health.Contract(p.ID, "detail", "loaded flag or listing identity changed"))
		}
		// Sparse individual listings are allowed; the bounded sample must include real detail content.
		if len([]rune(strings.TrimSpace(detail.Description))) < 40 || len(detail.ImageURLs) == 0 {
			continue
		}
		seen := map[string]bool{}
		for _, photo := range detail.ImageURLs {
			if !validHTTPS(photo) || seen[photo] {
				fail(health.Contract(p.ID, "detail", "gallery contains invalid or duplicate image URLs"))
			}
			seen[photo] = true
		}
		r.DescriptionChars, r.GalleryImages = len([]rune(detail.Description)), len(detail.ImageURLs)
		r.Stage = "image"
		if err := checkImage(t, p.ID, detail.ImageURLs[0]); err != nil {
			fail(err)
		}
		r.Stage = "complete"
		return
	}
	fail(health.Contract(p.ID, "detail", "no sampled listing returned both a fresh description and gallery; inspect parser or sample availability"))
}

func validateSearch(p probe, hosts []string, listings []model.Listing) error {
	invalid := func(message string) error { return health.Contract(p.ID, "search", message) }
	if len(listings) == 0 || len(listings) > 5 {
		return invalid("expected 1..5 listings from the broad rental search")
	}
	pattern, err := regexp.Compile(p.IDPattern)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	priced, located := false, false
	for _, listing := range listings {
		u, err := url.Parse(listing.URL)
		if err != nil || !validHTTPS(listing.URL) || !slices.Contains(hosts, u.Hostname()) {
			return invalid("invalid listing URL or unexpected marketplace host")
		}
		if listing.Platform != p.ID || !pattern.MatchString(listing.ID) || seen[listing.ID] {
			return invalid("invalid platform, changed ID format, or duplicate listing")
		}
		seen[listing.ID] = true
		if strings.TrimSpace(listing.Title) == "" || listing.Title == listing.URL {
			return invalid("title extraction failed")
		}
		priced = priced || (listing.PriceAmount != nil && *listing.PriceAmount > 0)
		located = located || strings.TrimSpace(listing.Location) != ""
	}
	if !priced || !located {
		return invalid("no parsed rent or location in the sampled search results")
	}
	return nil
}

func validHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}

func checkImage(t *testing.T, provider, rawURL string) error {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := health.CheckAccess(provider, "image", resp.StatusCode, ""); err != nil {
		return err
	}
	// Read only a file signature, never store or download a full listing photo.
	prefix, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(http.DetectContentType(prefix), "image/") {
		return health.Contract(provider, "image", fmt.Sprintf("gallery URL returned %s instead of image bytes", http.DetectContentType(prefix)))
	}
	return nil
}
