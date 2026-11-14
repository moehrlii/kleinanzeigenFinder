package immoscout

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"reflect"
	"strconv"
)

func TestImmoScoutTranslateSearchURLRegion(t *testing.T) {
	s := New(config.ImmoScoutSettings{})

	apiURL, err := s.translateSearchURL("https://www.immobilienscout24.de/Suche/de/berlin/berlin/wohnung-mieten?numberofrooms=2.0-&price=-1000&pricetype=calculatedtotalrent&equipment=balcony,builtinkitchen&sorting=2&enteredFrom=result_list")
	if err != nil {
		t.Fatalf("translateSearchURL returned error: %v", err)
	}

	query := apiURL.Query()
	assertQueryValue(t, query, "realestatetype", "apartmentrent")
	assertQueryValue(t, query, "searchType", "region")
	assertQueryValue(t, query, "geocodes", "/de/berlin/berlin")
	assertQueryValue(t, query, "numberofrooms", "2.0-")
	assertQueryValue(t, query, "price", "-1000")
	assertQueryValue(t, query, "pricetype", "calculatedtotalrent")
	assertQueryValue(t, query, "sorting", "-firstactivation")
	assertQueryValues(t, query, "equipment", []string{"balcony", "builtInKitchen"})
	if query.Get("enteredFrom") != "" {
		t.Fatalf("unexpected enteredFrom param: %q", query.Get("enteredFrom"))
	}
}

func TestImmoScoutTranslateSearchURLRadius(t *testing.T) {
	s := New(config.ImmoScoutSettings{})

	apiURL, err := s.translateSearchURL("https://www.immobilienscout24.de/Suche/radius/haus-mieten?geocoordinates=52.5200;13.4050;5.0&price=-2500")
	if err != nil {
		t.Fatalf("translateSearchURL returned error: %v", err)
	}

	query := apiURL.Query()
	assertQueryValue(t, query, "realestatetype", "houserent")
	assertQueryValue(t, query, "searchType", "radius")
	assertQueryValue(t, query, "geocoordinates", "52.5200;13.4050;5.0")
	assertQueryValue(t, query, "price", "-2500")
	if query.Get("geocodes") != "" {
		t.Fatalf("radius search should not set geocodes, got %q", query.Get("geocodes"))
	}
}

func TestImmoScoutTranslateSearchURLPathEquipment(t *testing.T) {
	s := New(config.ImmoScoutSettings{})

	apiURL, err := s.translateSearchURL("https://www.immobilienscout24.de/Suche/de/hamburg/hamburg/wohnung-mit-einbaukueche-mieten")
	if err != nil {
		t.Fatalf("translateSearchURL returned error: %v", err)
	}

	query := apiURL.Query()
	assertQueryValue(t, query, "realestatetype", "apartmentrent")
	assertQueryValue(t, query, "geocodes", "/de/hamburg/hamburg")
	assertQueryValues(t, query, "equipment", []string{"builtInKitchen"})
}

func TestImmoScoutTranslateSearchURLMietwohnungenMitTauschwohnungen(t *testing.T) {
	s := New(config.ImmoScoutSettings{})

	apiURL, err := s.translateSearchURL("https://www.immobilienscout24.de/Suche/de/baden-wuerttemberg/karlsruhe/mietwohnungen-mit-tauschwohnungen?price=550.0-850.0&pricetype=calculatedtotalrent&sorting=2")
	if err != nil {
		t.Fatalf("translateSearchURL returned error: %v", err)
	}

	query := apiURL.Query()
	assertQueryValue(t, query, "realestatetype", "apartmentrent")
	assertQueryValue(t, query, "searchType", "region")
	assertQueryValue(t, query, "geocodes", "/de/baden-wuerttemberg/karlsruhe")
	assertQueryValue(t, query, "price", "550.0-850.0")
	assertQueryValue(t, query, "pricetype", "calculatedtotalrent")
	assertQueryValue(t, query, "sorting", "-firstactivation")
}

func TestImmoScoutTranslateSearchURLRejectsShape(t *testing.T) {
	s := New(config.ImmoScoutSettings{})

	_, err := s.translateSearchURL("https://www.immobilienscout24.de/Suche/shape/wohnung-mieten?shape=abc")
	if err == nil || !strings.Contains(err.Error(), "shape") {
		t.Fatalf("expected shape search error, got %v", err)
	}
}

func TestImmoScoutFetchListingsLiveKarlsruhe(t *testing.T) {
	if os.Getenv("KF_LIVE_IMMOSCOUT") != "1" {
		t.Skip("set KF_LIVE_IMMOSCOUT=1 to run live ImmoScout API check")
	}

	s := New(config.ImmoScoutSettings{})
	listings, err := s.FetchListingsPages("https://www.immobilienscout24.de/Suche/de/baden-wuerttemberg/karlsruhe/mietwohnungen-mit-tauschwohnungen?price=550.0-850.0&pricetype=calculatedtotalrent&sorting=2", 2, 1)
	if err != nil {
		t.Fatalf("FetchListingsPages returned error: %v", err)
	}
	if len(listings) == 0 {
		t.Fatalf("expected at least one live Karlsruhe listing")
	}
	if listings[0].ID == "" || listings[0].URL == "" || listings[0].Title == "" {
		t.Fatalf("listing is missing core fields: %+v", listings[0])
	}
	detail, err := s.FetchDetail(listings[0])
	if err != nil {
		t.Fatal(err)
	}
	if !detail.DetailLoaded || detail.Platform != ID {
		t.Fatal("detail did not load")
	}
	t.Logf("live discovery and detail passed: description_chars=%d images=%d", len([]rune(detail.Description)), len(detail.ImageURLs))
}

func TestImmoScoutFetchListingsAndDetails(t *testing.T) {
	var listCalls int
	var detailCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "TestAgent" {
			t.Errorf("unexpected user agent: %q", r.Header.Get("User-Agent"))
		}

		switch r.URL.Path {
		case "/search/list":
			listCalls++
			if r.Method != http.MethodPost {
				t.Errorf("expected POST for search/list, got %s", r.Method)
			}
			if r.URL.Query().Get("pagenumber") != "1" {
				t.Errorf("expected first page request, got %s", r.URL.RawQuery)
			}
			writeJSON(t, w, map[string]any{
				"totalResults": 1,
				"resultListItems": []any{
					map[string]any{
						"type": "EXPOSE_RESULT",
						"item": map[string]any{
							"id":    "123",
							"title": "Helle Wohnung mit Balkon",
							"titlePicture": map[string]any{
								"preview": "https://example.test/image.jpg/ORIG/resize/800x600%3E/format/webp/quality/20",
							},
							"address":        map[string]any{"line": "10115 Berlin, Mitte"},
							"published":      "vor 5 Minuten",
							"realEstateType": "apartmentrent",
							"listOnlyOnIs24": true,
							"attributes": []any{
								map[string]any{"value": "850\u00a0\u20ac"},
								map[string]any{"value": "55\u00a0m\u00b2"},
								map[string]any{"value": "2\u00a0Zi."},
							},
						},
					},
				},
			})
		case "/expose/123":
			detailCalls++
			writeJSON(t, w, map[string]any{
				"sections": []any{
					map[string]any{
						"type": "TOP_ATTRIBUTES",
						"attributes": []any{
							map[string]any{"label": "Warmmiete", "text": "950\u00a0\u20ac"},
							map[string]any{"label": "Etage", "text": "3"},
						},
					},
					map[string]any{
						"type":  "TEXT_AREA",
						"title": "Ausstattung",
						"text":  "Balkon und Einbaukueche vorhanden.",
					},
					map[string]any{
						"type": "MEDIA",
						"media": []any{
							map[string]any{"fullImageUrl": "https://example.test/full.jpg"},
							map[string]any{"previewImageUrl": "https://example.test/second.jpg"},
							map[string]any{"fullImageUrl": "https://example.test/full.jpg"},
							map[string]any{"fullImageUrl": "https://www.static-immobilienscout24.de/statpic/placeholder_house/placeholder.png"},
						},
					},
				},
				"contact": map[string]any{
					"contactData": map[string]any{
						"agent": map[string]any{
							"name":    "Max Mustermann",
							"company": "Example Homes",
						},
					},
				},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	s := New(config.ImmoScoutSettings{
		APIBaseURL: server.URL,
		UserAgent:  "TestAgent",
		PageSize:   10,
	})

	listings, err := s.FetchListingsPages("https://www.immobilienscout24.de/Suche/de/berlin/berlin/wohnung-mieten", 0, 0)
	if err != nil {
		t.Fatalf("FetchListingsPages returned error: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("expected 1 listing, got %d", len(listings))
	}

	listing := listings[0]
	if listing.Platform != ID {
		t.Fatalf("unexpected platform %q", listing.Platform)
	}
	if listing.ID != "is24:123" {
		t.Fatalf("unexpected listing ID: %s", listing.ID)
	}
	if listing.Price == "" {
		t.Fatalf("unexpected listing price: %q", listing.Price)
	}
	if listing.Attributes["Wohnfläche"] == "" {
		t.Fatalf("expected Wohnflaeche attribute")
	}
	if listing.Attributes["Nur auf ImmoScout24"] != "true" {
		t.Fatalf("expected ImmoScout-only flag")
	}
	if listing.PriceAmount == nil || *listing.PriceAmount != 850 {
		t.Fatalf("unexpected price amount: %v", listing.PriceAmount)
	}

	detail, err := s.FetchDetail(listing)
	if err != nil {
		t.Fatalf("FetchDetail returned error: %v", err)
	}
	if len(detail.ImageURLs) != 3 || detail.ImageURLs[0] != "https://example.test/full.jpg" || detail.ImageURLs[1] != "https://example.test/second.jpg" {
		t.Fatalf("unexpected merged gallery %v", detail.ImageURLs)
	}
	if !detail.DetailLoaded {
		t.Fatalf("expected detail to be loaded")
	}
	if detail.Price == "" {
		t.Fatalf("expected detail warm rent to become price, got %q", detail.Price)
	}
	if detail.PriceAmount == nil || *detail.PriceAmount != 950 {
		t.Fatalf("unexpected detail price amount: %v", detail.PriceAmount)
	}
	if !strings.Contains(detail.Description, "Einbaukueche") {
		t.Fatalf("expected detail description, got %q", detail.Description)
	}
	if detail.Seller != "Max Mustermann" {
		t.Fatalf("unexpected seller: %q", detail.Seller)
	}
	if detail.Attributes["Etage"] != "3" {
		t.Fatalf("expected Etage attribute")
	}
	if listCalls != 1 || detailCalls != 1 {
		t.Fatalf("expected one list and one detail call, got list=%d detail=%d", listCalls, detailCalls)
	}
}

func assertQueryValue(t *testing.T, query url.Values, key, expected string) {
	t.Helper()
	if got := query.Get(key); got != expected {
		t.Fatalf("expected %s=%q, got %q", key, expected, got)
	}
}

func assertQueryValues(t *testing.T, query url.Values, key string, expected []string) {
	t.Helper()
	got := query[key]
	if strings.Join(got, ",") != strings.Join(expected, ",") {
		t.Fatalf("expected %s=%v, got %v", key, expected, got)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("could not write JSON: %v", err)
	}
}

func TestImmoScoutPaginationLimits(t *testing.T) {
	for _, test := range []struct {
		name                 string
		maxResults, maxPages int
		want                 []string
		calls                int
	}{
		{"all", 0, 0, []string{"is24:1", "is24:2", "is24:3"}, 3},
		{"result cap", 2, 0, []string{"is24:1", "is24:2"}, 2},
		{"page cap", 0, 1, []string{"is24:1"}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				page, _ := strconv.Atoi(r.URL.Query().Get("pagenumber"))
				var items []immoScoutResultListItem
				switch page {
				case 1:
					items = []immoScoutResultListItem{{Type: "PROMOTION"}, {Type: "EXPOSE_RESULT", Item: immoScoutListItem{ID: "1"}}}
				case 2:
					items = []immoScoutResultListItem{{Type: "EXPOSE_RESULT", Item: immoScoutListItem{ID: "1"}}, {Type: "EXPOSE_RESULT", Item: immoScoutListItem{ID: "2"}}}
				case 3:
					items = []immoScoutResultListItem{{Type: "EXPOSE_RESULT", Item: immoScoutListItem{ID: "3"}}}
				}
				writeJSON(t, w, immoScoutListResponse{ResultListItems: items})
			}))
			defer server.Close()
			provider := New(config.ImmoScoutSettings{APIBaseURL: server.URL, PageSize: 2})
			listings, err := provider.FetchListingsPages("https://www.immobilienscout24.de/Suche/de/wohnung-mieten", test.maxResults, test.maxPages)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, listing := range listings {
				ids = append(ids, listing.ID)
			}
			if !reflect.DeepEqual(ids, test.want) || calls != test.calls {
				t.Fatalf("ids=%v calls=%d", ids, calls)
			}
		})
	}
}

func TestImmoScoutRejectsInvalidSearchURLs(t *testing.T) {
	for _, raw := range []string{"https://example.test/Suche/de/wohnung-mieten", "http://www.immobilienscout24.de/Suche/de/wohnung-mieten", "https://user@www.immobilienscout24.de/Suche/de/wohnung-mieten", "https://www.immobilienscout24.de/expose/123", "https://api.mobile.immobilienscout24.de/expose/123", "https://www.immobilienscout24.de/Suche/shape/wohnung-mieten"} {
		if err := ValidateSearchURL(raw); err == nil {
			t.Errorf("accepted invalid URL %s", raw)
		}
	}
}

func TestImmoScoutAPIURLAndPlaceholder(t *testing.T) {
	provider := New(config.ImmoScoutSettings{APIBaseURL: "https://fixture.example/api"})
	endpoint, err := provider.translateSearchURL("https://api.mobile.immobilienscout24.de/search/list?realestatetype=apartmentrent&geocodes=%2Fde%2Fberlin")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Host != "fixture.example" || endpoint.Path != "/api/search/list" || endpoint.Query().Get("geocodes") != "/de/berlin" {
		t.Fatalf("wrong endpoint %s", endpoint)
	}
	listing := listingFromImmoScoutItem(immoScoutListItem{ID: "123", TitlePicture: immoScoutTitlePicture{Full: "https://www.static-immobilienscout24.de/statpic/placeholder_house/image.png"}})
	if listing.ImageURL != "" || len(listing.ImageURLs) != 0 {
		t.Fatal("placeholder treated as property photo")
	}
	detail := provider.detailListingFromResponse("123", model.Listing{}, immoScoutExposeResponse{})
	if detail.Platform != ID || detail.ID != "is24:123" {
		t.Fatal("missing detail identity")
	}
}
