package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fmt"
	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/providers"
)

func response(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{map[string]any{"type": "reasoning"}, map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": content}}}}})
}

func TestTextAndImages(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("incorrect API request")
		}
		var body struct {
			Store bool `json:"store"`
			Text  struct {
				Format struct {
					Type   string
					Strict bool
				}
			} `json:"text"`
		}
		// Inspect the multimodal content using its wire field names.
		var raw map[string]any
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &body)
		if body.Store || body.Text.Format.Type != "json_schema" || !body.Text.Format.Strict {
			t.Error("missing strict structured output")
		}
		input := raw["input"].([]any)[0].(map[string]any)["content"].([]any)
		if len(input) != 3 {
			t.Errorf("expected text and two unique images, got %d", len(input))
		}
		if input[1].(map[string]any)["image_url"] != "https://images.example/a.jpg" {
			t.Error("wrong gallery order")
		}
		response(w, `{"suitable":true,"score":85,"reason":"Fits","summary":"Apartment","red_flags":[],"visual_assessment":"Balcony visible","uncertainties":["Confirm warm rent"]}`)
	}))
	defer server.Close()
	c, err := NewClassifier(config.LLMSettings{Enabled: true, BaseURL: server.URL, MaxImages: 2, MinScore: 90})
	if err != nil {
		t.Fatal(err)
	}
	got := c.Classify(t.Context(), model.Listing{ImageURLs: []string{"https://images.example/a.jpg", "https://images.example/a.jpg", "https://images.example/b.jpg", "https://images.example/c.jpg"}}, config.MonitorConfig{Criteria: "Balcony"})
	if got.Error != "" || got.Suitable || got.Score != 85 || got.ImagesAnalyzed != 2 || got.VisualAssessment != "Balcony visible" {
		t.Fatalf("unexpected assessment %+v", got)
	}
	c.settings.MaxImages = 1
	if got := c.listingImages(model.Listing{ImageURL: "https://images.example/legacy.jpg"}); len(got) != 1 {
		t.Fatal("legacy image lost")
	}
	c.settings.MaxImages = 0
	if len(c.listingImages(model.Listing{ImageURL: "https://images.example/legacy.jpg"})) != 0 {
		t.Fatal("text-only mode failed")
	}
}

func TestAPIFailuresRemainErrors(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	for _, status := range []int{401, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(status) }))
			defer server.Close()
			c, _ := NewClassifier(config.LLMSettings{Enabled: true, BaseURL: server.URL, MaxRetries: 1})
			got := c.Classify(t.Context(), model.Listing{}, config.MonitorConfig{})
			want := int32(2)
			if status == 401 {
				want = 1
			}
			if got.Error == "" || got.Suitable || calls.Load() != want {
				t.Fatalf("error=%q calls=%d", got.Error, calls.Load())
			}
		})
	}
}

func TestRetryCancellation(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429); cancel() }))
	defer server.Close()
	c, _ := NewClassifier(config.LLMSettings{Enabled: true, BaseURL: server.URL, MaxRetries: 2, RetryDelaySeconds: 30})
	start := time.Now()
	if result := c.Classify(ctx, model.Listing{}, config.MonitorConfig{}); result.Error == "" {
		t.Fatal("expected cancellation")
	}
	if time.Since(start) > time.Second {
		t.Fatal("retry ignored cancellation")
	}
}

func TestDraftRetainsRequirementsAndRules(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response(w, `{"name":"flat","search_url":"https://www.kleinanzeigen.de/s-wohnung/k0","criteria":"Balcony required","min_price":500,"max_price":1000,"include_any":["Balkon","Terrasse"],"include_keywords":[],"exclude_keywords":["Tausch"]}`)
	}))
	defer server.Close()
	c, _ := NewClassifier(config.LLMSettings{Enabled: true, BaseURL: server.URL})
	provided := "https://www.kleinanzeigen.de/s-wohnung-mieten/berlin/c203l3331"
	draft, err := c.DraftMonitor(t.Context(), "Warm rent under 1000, bright kitchen", provided)
	if err != nil {
		t.Fatal(err)
	}
	if draft.SearchURL != provided || draft.Platform != "kleinanzeigen" || draft.Rules.MaxPrice == nil || *draft.Rules.MaxPrice != 1000 || !strings.Contains(draft.Criteria, "bright kitchen") {
		t.Fatalf("lost draft data %+v", draft)
	}
}

func TestUnusableResponsesStayPending(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	cases := map[string]string{
		"refusal":    `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal"}]}]}`,
		"incomplete": `{"status":"incomplete","output":[]}`,
		"malformed":  `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"invalid"}]}]}`,
		"empty":      `{"status":"completed","output":[]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			c, _ := NewClassifier(config.LLMSettings{Enabled: true, BaseURL: server.URL})
			got := c.Classify(t.Context(), model.Listing{}, config.MonitorConfig{})
			if got.Error == "" || got.Suitable {
				t.Fatalf("unusable response accepted: %+v", got)
			}
		})
	}
}

// fixtureMarketplace exercises registration without adding production marketplace branches.
type fixtureMarketplace struct{}

func (fixtureMarketplace) FetchListingsPages(string, int, int) ([]model.Listing, error) {
	return nil, nil
}
func (fixtureMarketplace) FetchDetail(listing model.Listing) (model.Listing, error) {
	return listing, nil
}
func (fixtureMarketplace) Close() error { return nil }

func TestWizardUsesRegisteredProvider(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	registry, err := providers.NewRegistry("fixture", providers.Definition{
		ID: "fixture", Name: "Fixture marketplace", Hosts: []string{"fixture.example"},
		DraftInstructions: "Use fixture search rules.",
		ValidateSearchURL: func(raw string) error {
			if raw != "https://fixture.example/search" {
				return fmt.Errorf("invalid fixture search")
			}
			return nil
		},
		Open: func(providers.Settings) (providers.ManagedProvider, error) { return fixtureMarketplace{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Instructions string `json:"instructions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !strings.Contains(body.Instructions, "Use fixture search rules.") || strings.Contains(body.Instructions, "kleinanzeigen.de") {
			t.Error("wrong marketplace instructions")
		}
		response(w, `{"name":"fixture-search","search_url":"https://fixture.example/search","criteria":"A bright flat"}`)
	}))
	defer server.Close()
	classifier, err := NewClassifierWithProviders(config.LLMSettings{Enabled: true, BaseURL: server.URL}, registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"", "https://fixture.example/search"} {
		draft, err := classifier.DraftMonitor(t.Context(), "A bright flat", url)
		if err != nil {
			t.Fatal(err)
		}
		if draft.Platform != "fixture" {
			t.Fatalf("wrong platform %q", draft.Platform)
		}
		if err := classifier.ValidateDraft(draft); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := classifier.DraftMonitorForPlatform(t.Context(), "unknown", "A flat", ""); err == nil {
		t.Fatal("unknown provider accepted")
	}
}
