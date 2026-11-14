package kleinanzeigen

import (
	"os"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

func TestGalleryExtraction(t *testing.T) {
	if os.Getenv("KF_BROWSER_TESTS") != "1" {
		t.Skip("set KF_BROWSER_TESTS=1 with Playwright Chromium installed")
	}
	pw, err := playwright.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer pw.Stop()
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: new(true)})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	page, err := browser.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	err = page.Route("**/*", func(route playwright.Route) { _ = route.Abort() })
	if err != nil {
		t.Fatal(err)
	}
	err = page.SetContent(`<main><article><h1>Apartment</h1><div id="viewad-image">
 <img src="https://images.example/a.jpg"><img data-src="https://images.example/b.jpg">
 <img src="https://images.example/a.jpg"></div><div class="galleryimage-element" data-imgsrc="https://images.example/c.jpg"></div>
 <img class="seller-avatar" src="https://images.example/avatar.jpg"></article></main>
 <aside class="recommendations"><img src="https://images.example/other.jpg"></aside>`)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := page.Evaluate(DetailExtractScript)
	if err != nil {
		t.Fatal(err)
	}
	s := &KleinanzeigenScraper{}
	listing := s.listingFromRaw(raw.(map[string]any))
	if len(listing.ImageURLs) != 3 || listing.ImageURLs[1] != "https://images.example/b.jpg" || listing.Platform != "kleinanzeigen" {
		t.Fatalf("unexpected gallery %+v", listing.ImageURLs)
	}
}
