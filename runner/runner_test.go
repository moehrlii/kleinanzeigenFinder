package runner

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/llm"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/notify"
	"github.com/Kamuyin/kleinanzeigenFinder/providers"
	"github.com/Kamuyin/kleinanzeigenFinder/storage"
)

type testProvider struct{}

func (testProvider) FetchListingsPages(string, int, int) ([]model.Listing, error) {
	return []model.Listing{{ID: "offer", Title: "Apartment", URL: "https://example.com/offer"}}, nil
}
func (testProvider) FetchDetail(listing model.Listing) (model.Listing, error) {
	listing.Description = "Balcony"
	listing.DetailLoaded = true
	return listing, nil
}

func TestAssessmentFailureStaysQueuedAndRetries(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	db, err := storage.Connect(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	seen := storage.NewSeenStore(db)
	repo := storage.NewMonitorRepository(db)
	monitor := config.MonitorConfig{Name: "flat", Platform: "fixture", Enabled: true, FetchDetails: true, Mode: "backfill", Rules: config.RulesConfig{IncludeKeywords: []string{"balcony"}}}
	if err := repo.Upsert(monitor, "test"); err != nil {
		t.Fatal(err)
	}
	cls, err := llm.NewClassifier(config.LLMSettings{Enabled: true, BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	r := New(&config.AppConfig{}, seen, repo, map[string]providers.Provider{"fixture": testProvider{}}, cls, &notify.Router{})
	stats, err := r.RunOnce(t.Context(), false)
	if err != nil || stats.Errors != 1 || repo.OpenQueueCount("flat") != 1 || seen.HasSeen("flat", "offer") || repo.Get("flat").Mode != "backfill" {
		t.Fatalf("failed assessment was consumed: %+v %v", stats, err)
	}
	// Retry the same queue through the normal loop with local-only assessment.
	r.classifier, _ = llm.NewClassifier(config.LLMSettings{})
	stats, err = r.RunOnce(t.Context(), false)
	if err != nil || stats.Rejected != 0 || repo.OpenQueueCount("flat") != 0 || !seen.HasSeen("flat", "offer") || repo.Get("flat").Mode != "watch" {
		t.Fatalf("retry failed: %+v %v", stats, err)
	}
}
