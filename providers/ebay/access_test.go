package ebay

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value string
		want  time.Time
	}{
		{"3600", now.Add(time.Hour)},
		{"0", now},
		{now.Add(time.Hour).Format(http.TimeFormat), now.Add(time.Hour)},
		{"-1", time.Time{}},
		{"999999999999999999999", time.Time{}},
		{"invalid", time.Time{}},
		{"", time.Time{}},
	} {
		t.Run(tc.value, func(t *testing.T) {
			if got := retryAfter(tc.value, now); !got.Equal(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSafeServer(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"AkamaiGHost", "AkamaiGHost"},
		{"nginx/1.2", "nginx/1.2"},
		{"server\r\ninjected", ""},
		{"<script>", ""},
		{strings.Repeat("a", 81), ""},
	} {
		if got := safeServer(tc.value); got != tc.want {
			t.Errorf("safeServer(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}
