package health

import (
	"errors"
	"testing"
)

func TestAccessFailures(t *testing.T) {
	for _, test := range []struct {
		name, text, kind string
		status           int
	}{
		{"success", "", "", 200},
		{"captcha with success status", "Please complete the CAPTCHA", "challenge", 200},
		{"cloudflare", "Just a moment...", "challenge", 403},
		{"german challenge", "Sicherheitsüberprüfung", "challenge", 200},
		{"soft denial", "Zugriff verweigert", "access_denied", 200},
		{"unauthorized", "", "access_denied", 401},
		{"forbidden", "", "access_denied", 403},
		{"rate limit", "", "rate_limited", 429},
		{"gone", "", "not_found", 410},
		{"outage", "", "http_error", 503},
		{"redirect", "", "http_error", 302},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := CheckAccess("test", "search", test.status, test.text)
			if test.kind == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			failure, ok := errors.AsType[*Failure](err)
			if !ok || failure.Kind != test.kind || failure.Status != test.status {
				t.Fatalf("expected %s with HTTP %d, got %v", test.kind, test.status, err)
			}
		})
	}
}
