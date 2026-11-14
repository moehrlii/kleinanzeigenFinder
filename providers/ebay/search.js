() => {
  const text = e => (e?.textContent || '').replace(/\s+/g, ' ').trim();
  const root = document.querySelector('.srp-results');
  const count = text(document.querySelector('.srp-controls__count-heading, #srp-results-heading'));
  const empty = /^0\s+(?:results|Ergebnisse|Treffer)\b/i.test(count) ||
    !!document.querySelector('.srp-save-null-search, .srp-main--isEmpty');
  const result = {recognized: !!root || empty, empty, listings: [], next_page: ''};
  if (!root || empty) return result;
  // Related/fuzzy results can share the result river. Stop at its first separator.
  for (const node of root.children) {
    if (node.matches('.srp-river-answer, .srp-river-answer--REWRITE_START, .srp-river-answer--MORE_MATCHES') ||
        /(?:results matching fewer words|fewer words|Ergebnisse für weniger Suchbegriffe|Ergebnisse für ähnliche Suchbegriffe)/i.test(text(node.querySelector('h2, .srp-river-answer__title')))) break;
    if (!node.matches('li.s-card, li.s-item')) continue;
    const link = node.querySelector('a.s-card__link[href*="/itm/"], a.s-item__link[href*="/itm/"]');
    if (!link || !/\/itm\/(?:[^/]+\/)?\d{9,15}(?:[/?#]|$)/.test(link.href)) continue;
    const heading = node.querySelector('.s-card__title, .s-item__title');
    const title = text(heading).replace(/^(?:Neues Angebot|New Listing)\s*/i, '')
      .replace(/\s*(?:Opens in a new window or tab|Wird in neuem Fenster oder Tab geöffnet)\s*$/i, '').trim();
    if (!title || /^Shop on eBay$/i.test(title)) continue;
    const rows = [...node.querySelectorAll('.s-card__attribute-row, .s-item__detail')].map(text).filter(Boolean);
    const find = pattern => rows.find(v => pattern.test(v)) || '';
    const bids = find(/\b(?:Gebot|Gebote|bid|bids)\b/i);
    const format = bids || find(/Auktion|Auction/i) ? 'Auction' : find(/Sofort.Kaufen|Buy It Now|oder Preisvorschlag|or Best Offer/i) ? 'Buy It Now' : '';
    const attributes = {};
    const condition = text(node.querySelector('.s-card__subtitle, .s-item__subtitle, .SECONDARY_INFO'));
    if (condition) attributes.Condition = condition;
    if (format) attributes['Buying format'] = format;
    if (bids) attributes.Bids = bids;
    const shipping = find(/Versand|Lieferung|delivery|shipping|Abholung|pickup/i);
    if (shipping) attributes.Shipping = shipping;
    const location = find(/^(?:aus |von |Located in |From )/i).replace(/^(?:aus |von |Located in |From )/i, '');
    const seller = text(node.querySelector('.s-item__seller-info-text')) || find(/%\s*(?:positiv|positive)/i);
    const posted = text(node.querySelector('.s-item__listingDate')) || find(/(?:\d{1,2}\.\s*\w+.*\d{1,2}:\d{2}|[A-Z][a-z]{2}-\d{1,2}\s+\d{1,2}:\d{2})/);
    const images = [...node.querySelectorAll('.s-card__image, .s-item__image-wrapper img')]
      .map(e => e.getAttribute('data-src') || e.getAttribute('src')).filter(Boolean);
    result.listings.push({url: link.href, title, price: text(node.querySelector('.s-card__price, .s-item__price')),
      location, seller, posted_at: posted, images, attributes});
  }
  const next = document.querySelector('a.pagination__next[href]:not([aria-disabled="true"])');
  if (next) result.next_page = next.href;
  return result;
}
