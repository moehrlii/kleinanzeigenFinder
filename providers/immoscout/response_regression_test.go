package immoscout

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/providers/health"
)

func TestUpstreamResponseRegression(t *testing.T) {
	for _, operation := range []string{"search", "detail"} {
		t.Run(operation, func(t *testing.T) {
			key := "resultListItems"
			if operation == "detail" {
				key = "sections"
			}
			for _, test := range []struct {
				name, body, kind string
				status           int
			}{
				{"empty array remains valid", `{"` + key + `":[],"newField":true}`, "", 200},
				{"listing text is not a challenge", `{"` + key + `":[],"description":"CAPTCHA security check"}`, "", 200},
				{"renamed envelope", `{"newResults":[]}`, "contract_changed", 200},
				{"null envelope", `{"` + key + `":null}`, "contract_changed", 200},
				{"wrong array type", `{"` + key + `":{}}`, "contract_changed", 200},
				{"wrong item type", `{"` + key + `":[42]}`, "contract_changed", 200},
				{"invalid JSON", `{"broken":`, "contract_changed", 200},
				{"HTML login", `<html><h1>Sign in</h1></html>`, "contract_changed", 200},
				{"HTML captcha", `<html><title>CAPTCHA</title></html>`, "challenge", 200},
				{"JSON challenge", `{"error":"captcha_required"}`, "challenge", 200},
				{"forbidden challenge", `<h1>Verify you are human</h1>`, "challenge", 403},
				{"unauthorized", `{"error":"authentication required"}`, "access_denied", 401},
				{"forbidden", `{"error":"denied"}`, "access_denied", 403},
				{"rate limit", `{"error":"slow down"}`, "rate_limited", 429},
				{"removed listing", `{"error":"gone"}`, "not_found", 410},
				{"server error", `{"error":"unavailable"}`, "http_error", 503},
			} {
				t.Run(test.name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(test.status)
						_, _ = w.Write([]byte(test.body))
					}))
					t.Cleanup(server.Close)
					provider := New(config.ImmoScoutSettings{APIBaseURL: server.URL})
					t.Cleanup(func() { _ = provider.Close() })
					var err error
					if operation == "search" {
						_, err = provider.FetchListingsPages("https://www.immobilienscout24.de/Suche/de/wohnung-mieten", 5, 1)
					} else {
						_, err = provider.FetchDetail(model.Listing{ID: "is24:123"})
					}
					if test.kind == "" {
						if err != nil {
							t.Fatal(err)
						}
						return
					}
					failure, ok := errors.AsType[*health.Failure](err)
					if !ok || failure.Kind != test.kind || failure.Operation != operation {
						t.Fatalf("expected %s %s, got %v", operation, test.kind, err)
					}
					if strings.Contains(err.Error(), test.body) {
						t.Fatal("error leaked response body")
					}
				})
			}
		})
	}
}
