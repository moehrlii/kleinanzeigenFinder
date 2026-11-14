package providers

import (
	"strings"
	"testing"
)

func TestBuiltinResolution(t *testing.T) {
	registry := DefaultRegistry()
	for _, test := range []struct{ name, id, url, want, errorText string }{
		{name: "default", want: "kleinanzeigen"},
		{name: "search URL", url: "https://www.kleinanzeigen.de/s-wohnung/k0", want: "kleinanzeigen"},
		{name: "legacy", id: NormalizePlatform(""), url: "https://www.kleinanzeigen.de/s-wohnung/k0", want: "kleinanzeigen"},
		{name: "eBay Germany", url: "https://www.ebay.de/sch/i.html", want: "ebay"},
		{name: "eBay US", url: "https://www.ebay.com/sch/293/i.html?_nkw=lego", want: "ebay"},
		{name: "ImmoScout", id: "immoscout", want: "immoscout"},
		{name: "ImmoScout URL", url: "https://www.immobilienscout24.de/Suche/de/berlin/berlin/wohnung-mieten", want: "immoscout"},
		{name: "ImmoScout API URL", url: "https://api.mobile.immobilienscout24.de/search/list?realestatetype=apartmentrent", want: "immoscout"},
		{name: "planned WG", url: "https://www.wg-gesucht.de/", errorText: "not implemented"},
		{name: "unknown", id: "missing", errorText: "unknown provider"},
		{name: "wrong host", url: "https://www.kleinanzeigen.de.example.com/s-wohnung/k0", errorText: "no provider"},
		{name: "mismatch", id: "kleinanzeigen", url: "https://www.ebay.de/sch/i.html", errorText: "expected"},
		{name: "credentials", url: "https://user@www.kleinanzeigen.de/s-wohnung/k0", errorText: "invalid"},
		{name: "insecure", url: "http://www.kleinanzeigen.de/s-wohnung/k0", errorText: "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := registry.Resolve(test.id, test.url)
			if test.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), test.errorText) {
					t.Fatalf("expected %q, got %v", test.errorText, err)
				}
				return
			}
			if err != nil || got.ID != test.want {
				t.Fatalf("provider=%q error=%v", got.ID, err)
			}
		})
	}
	if got := registry.Available(); len(got) != 3 || got[0].ID != "kleinanzeigen" || got[1].ID != "immoscout" || got[2].ID != "ebay" {
		t.Fatalf("unexpected active providers %+v", got)
	}
}

func TestRegistryRejectsAmbiguousDefinitions(t *testing.T) {
	base := DefaultRegistry().Available()[0]
	duplicateHost := base
	duplicateHost.ID = "other"
	for _, definitions := range [][]Definition{{base, base}, {base, duplicateHost}, {{ID: "empty", Name: "Empty"}}} {
		if _, err := NewRegistry(base.ID, definitions...); err == nil {
			t.Fatal("invalid registration accepted")
		}
	}
}

func TestRegistryOwnsHostSlices(t *testing.T) {
	definition := DefaultRegistry().Available()[0]
	registry, err := NewRegistry(definition.ID, definition)
	if err != nil {
		t.Fatal(err)
	}
	definition.Hosts[0] = "changed.example"
	available := registry.Available()
	available[0].Hosts[0] = "changed-again.example"
	resolved, err := registry.Resolve("", "https://www.kleinanzeigen.de/s-wohnung/k0")
	if err != nil {
		t.Fatal(err)
	}
	resolved.Hosts[0] = "changed-third.example"
	if _, err := registry.Resolve("", "https://www.kleinanzeigen.de/s-wohnung/k0"); err != nil {
		t.Fatal(err)
	}
}
