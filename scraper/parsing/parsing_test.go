package parsing

import (
	"testing"
)

func TestExtractIDFromURL_Standard(t *testing.T) {
	url := "https://www.kleinanzeigen.de/s-anzeige/foo/3424034687-203-9188"
	if got := ExtractIDFromURL(url); got != "3424034687" {
		t.Errorf("expected 3424034687, got %v", got)
	}
}

func TestExtractIDFromURL_NoMatch(t *testing.T) {
	url := "https://example.com/page"
	if got := ExtractIDFromURL(url); got != "" {
		t.Errorf("expected empty string, got %v", got)
	}
}

func TestParsePriceAmount_Comma(t *testing.T) {
	got := ParsePriceAmount("1.000 €")
	if got == nil || *got != 1000.0 {
		t.Errorf("expected 1000.0, got %v", got)
	}
}

func TestParsePriceAmount_Plain(t *testing.T) {
	got := ParsePriceAmount("750,50 EUR")
	if got == nil || *got != 750.5 {
		t.Errorf("expected 750.5, got %v", got)
	}
}

func TestParsePriceAmount_Free(t *testing.T) {
	got := ParsePriceAmount("Zu verschenken")
	if got == nil || *got != 0.0 {
		t.Errorf("expected 0.0, got %v", got)
	}
}

func TestParsePriceAmount_None(t *testing.T) {
	got := ParsePriceAmount("VB")
	if got != nil {
		t.Errorf("expected nil, got %v", *got)
	}
}

func TestParseRooms_Decimal(t *testing.T) {
	text := "27,50 m² · 1,5 Zi."
	got := ParseRooms(text)
	if got == nil || *got != 1.5 {
		t.Errorf("expected 1.5, got %v", got)
	}
}

func TestParseRooms_Integer(t *testing.T) {
	got := ParseRooms("2 Zimmer")

	if got == nil || *got != 2.0 {
		t.Errorf("expected 2.0, got %v", got)
	}
}

func TestParseSizeM2_Decimal(t *testing.T) {
	text := "27,50 m² · 1,5 Zi."
	got := ParseSizeM2(text)
	if got == nil || *got != 27.5 {
		t.Errorf("expected 27.5, got %v", got)
	}
}

func TestParseSizeM2_Integer(t *testing.T) {
	got := ParseSizeM2("75 m²")
	if got == nil || *got != 75.0 {
		t.Errorf("expected 75.0, got %v", got)
	}
}
