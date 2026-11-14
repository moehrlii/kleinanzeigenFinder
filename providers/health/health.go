// Package health describes provider access failures without retaining response bodies.
package health

import (
	"fmt"
	"strings"
	"time"
)

// Failure distinguishes access restrictions from changes to an upstream contract.
type Failure struct {
	Access    *AccessDiagnostics `json:"access,omitempty"`
	Provider  string             `json:"provider"`
	Operation string             `json:"operation"`
	Kind      string             `json:"kind"`
	Status    int                `json:"http_status,omitzero"`
	Detail    string             `json:"detail"`
}

// AccessDiagnostics contains only non-secret transport metadata. Never include
// cookies, authorization headers, full response bodies, or browser profile data.
type AccessDiagnostics struct {
	Server         string    `json:"server,omitempty"`
	Reference      string    `json:"reference,omitempty"`
	BrowserVersion string    `json:"browser_version,omitempty"`
	Runtime        string    `json:"runtime,omitempty"`
	RetryAt        time.Time `json:"retry_at,omitzero"`
}

func (f *Failure) Error() string {
	if f.Status == 0 {
		return fmt.Sprintf("%s %s: %s: %s", f.Provider, f.Operation, f.Kind, f.Detail)
	}
	return fmt.Sprintf("%s %s: %s (HTTP %d): %s", f.Provider, f.Operation, f.Kind, f.Status, f.Detail)
}

// CheckAccess receives challenge UI text, or an API error body, never listing text.
// A detected challenge is reported; callers must not attempt to solve or retry it.
func CheckAccess(provider, operation string, status int, challengeText string) error {
	kind, detail := "", ""
	text := strings.ToLower(challengeText)
	for _, marker := range []string{"captcha", "verify you are human", "verify that you are human", "are you a robot", "bist du ein mensch", "sicherheitsüberprüfung", "security check", "just a moment", "checking your browser", "challenge-platform"} {
		if strings.Contains(text, marker) {
			kind, detail = "challenge", "CAPTCHA or browser verification detected"
			break
		}
	}
	if kind == "" {
		switch {
		case status == 429:
			kind, detail = "rate_limited", "upstream rate limit; do not immediately retry"
		case status == 401 || status == 403 || strings.Contains(text, "access denied") || strings.Contains(text, "zugriff verweigert"):
			kind, detail = "access_denied", "authentication or access policy blocked the request"
		case status == 404 || status == 410:
			kind, detail = "not_found", "endpoint or listing no longer available"
		case status < 200 || status >= 300:
			kind, detail = "http_error", "unexpected upstream HTTP status"
		}
	}
	if kind == "" {
		return nil
	}
	return &Failure{Provider: provider, Operation: operation, Kind: kind, Status: status, Detail: detail}
}

func Contract(provider, operation, detail string) error {
	return &Failure{Provider: provider, Operation: operation, Kind: "contract_changed", Detail: detail}
}
