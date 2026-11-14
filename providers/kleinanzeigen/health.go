package kleinanzeigen

import (
	"github.com/Kamuyin/kleinanzeigenFinder/providers/health"
	"github.com/mxschmitt/playwright-go"
)

// Inspect visible challenge UI only. Merely loading a CAPTCHA script, or mentioning
// CAPTCHA in a normal listing description, does not mean access is blocked.
const accessCheckScript = `() => {
  const visible = el => !!(el.getClientRects().length) &&
    getComputedStyle(el).visibility !== 'hidden' && getComputedStyle(el).display !== 'none';
  const selectors = 'iframe[src*="captcha"], iframe[src*="challenges.cloudflare.com"], #challenge-form, #captcha, .g-recaptcha, .h-captcha';
  if ([...document.querySelectorAll(selectors)].some(visible)) return 'captcha';
  return document.title + '\n' + [...document.querySelectorAll('h1, h2')]
    .filter(visible).map(el => el.innerText).join('\n');
}`

func checkPageAccess(page playwright.Page, status int, operation string) error {
	raw, err := page.Evaluate(accessCheckScript)
	if err != nil {
		return err
	}
	text, _ := raw.(string)
	return health.CheckAccess(ID, operation, status, text)
}
