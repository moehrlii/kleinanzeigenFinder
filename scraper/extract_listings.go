package scraper

const CardExtractScript = `
() => {
  const clean = (value) => (value || "").replace(/\s+/g, " ").trim();
  const cleanLines = (value) => (value || "")
    .split(/\n+/)
    .map((line) => clean(line))
    .filter(Boolean);
  const abs = (href) => {
    try { return new URL(href, location.origin).href; } catch { return null; }
  };
  const idFromHref = (href) => {
    const match = String(href || "").match(/\/(\d+)-\d+-\d+(?:[/?#]|$)/);
    return match ? match[1] : null;
  };
  const firstText = (root, selectors) => {
    for (const selector of selectors) {
      const node = root.querySelector(selector);
      const text = clean(node?.innerText);
      if (text) return text;
    }
    return null;
  };
  const priceFromText = (text) => {
    const pricePattern = new RegExp(
      "(?:\\d{1,3}(?:[.\\s]\\d{3})*|\\d+)(?:,\\d{1,2})?\\s*(?:€|EUR)"
      + "|VB|Zu verschenken",
      "i"
    );
    const match = text.match(pricePattern);
    return match ? clean(match[0]) : null;
  };

  const SPURIOUS_HEADINGS = [
    "ähnliche anzeigen",
    "das könnte dich auch interessieren",
    "empfohlene anzeigen",
    "sponsored",
    "gesponsert",
    "similar listings",
  ];

  // Find the first element whose text exactly matches one of those headings.
  let cutoffNode = null;
  for (const el of document.querySelectorAll('h2, h3, [class*="headline"], [class*="section-heading"], [class*="similar"]')) {
    const text = clean(el.innerText || el.textContent).toLowerCase();
    if (SPURIOUS_HEADINGS.some(h => text.includes(h))) {
      cutoffNode = el;
      break;
    }
  }

  // Collect only articles that appear BEFORE the cutoff node in DOM order.
  const allCards = Array.from(document.querySelectorAll(
    'article[data-adid], article.aditem, li.ad-listitem'
  ));

  const cards = cutoffNode
    ? allCards.filter(card => {
        // Node.DOCUMENT_POSITION_FOLLOWING means cutoffNode comes after card → card is before cutoff
        return cutoffNode.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING;
      })
    : allCards;

  const seen = new Set();
  const result = [];

  for (const card of cards) {
    // Skip any card that is inside or near a "similar"/"sponsored" section
    const parentSection = card.closest('[class*="similar"], [class*="recommendation"], [class*="sponsored"], [class*="empfohlen"]');
    if (parentSection) continue;

    const links = Array.from(card.querySelectorAll('a[href*="/s-anzeige/"]'));
    const titleLink = links.find((link) => {
      const text = clean(link.innerText || link.getAttribute("aria-label"));
      return text.length > 2 && !/^\d+$/.test(text);
    }) || links[0];
    const href = abs(titleLink?.getAttribute("href") || card.getAttribute("data-href"));
    const id = card.getAttribute("data-adid") || idFromHref(href);
    if (!href || !id || seen.has(id)) continue;
    seen.add(id);

    const lines = cleanLines(card.innerText);
    const rawText = clean(card.innerText);
    const title = clean(titleLink?.innerText || titleLink?.getAttribute("aria-label"))
      || lines.find((line) => line.length > 8 && !/^\d+$/.test(line))
      || href;
    const price = firstText(card, [
      '.aditem-main--middle--price-shipping',
      '[class*="price"]',
    ]) || priceFromText(rawText);
    const location = firstText(card, [
      '.aditem-main--top--left',
      '.aditem-main--top',
    ]) || lines.find((line) => /\b\d{5}\b/.test(line)) || null;
    const postedAt = lines.find((line) =>
      /^(Heute|Gestern)(?:,?\s+\d{1,2}:\d{2})?$|^\d{1,2}\.\d{1,2}\.\d{4}$/i
        .test(line)
    ) || null;
    const image = card.querySelector("img")?.src || null;
    const description = firstText(card, ['.aditem-main--middle--description']);

    result.push({
      id,
      url: href,
      title,
      price,
      location,
      posted_at: postedAt,
      description,
      image_url: image,
      raw_text: rawText,
    });
  }
  const nextNode = document.querySelector('a.pagination-next, [class*="pagination"] a[rel="next"], [class*="pagination"] a[title*="Nächste"], a[title*="Nächste"]');
  const nextHref = nextNode ? abs(nextNode.getAttribute("href")) : null;

  const pageNumberFromUrl = (href) => {
    const match = String(href || "").match(/\/seite:(\d+)(?:\/|$)/);
    return match ? Number(match[1]) : 1;
  };
  const searchPageUrl = (pageNumber) => {
    try {
      const url = new URL(location.href);
      if (/\/seite:\d+(?=\/)/.test(url.pathname)) {
        url.pathname = url.pathname.replace(/\/seite:\d+(?=\/)/, "/seite:" + pageNumber);
      } else {
        url.pathname = url.pathname.replace(/\/(c\d+[^/]*)$/, "/seite:" + pageNumber + "/$1");
      }
      return url.href;
    } catch {
      return null;
    }
  };
  let resolvedNextHref = nextHref;

  const currentPageFromUrl = pageNumberFromUrl(location.pathname);
  if (!resolvedNextHref) {
    const nextButtonSelectors = [
      'a.pagination-next',
      'a[rel="next"]',
      '[class*="pagination"] a[rel="next"]',
      '[class*="pagination"] a[title*="Nächste"]',
      '[class*="pagination"] a[aria-label*="Nächste"]',
      '[class*="pagination"] a[aria-label*="Weiter"]',
      'a[title*="Nächste"]',
      'a[aria-label*="Nächste"]',
      'a[aria-label*="Weiter"]',
    ];
    const nextButton = nextButtonSelectors
      .map((selector) => document.querySelector(selector))
      .find(Boolean);
    resolvedNextHref = nextButton ? abs(nextButton.getAttribute("href")) : null;
  }
  if (!resolvedNextHref) {
    const numberedLinks = Array.from(document.querySelectorAll('[class*="pagination"] a[href*="seite:"], a[href*="seite:"]'))
      .map((link) => ({
        page: pageNumberFromUrl(link.getAttribute("href")),
        href: abs(link.getAttribute("href")),
      }))
      .filter((entry) => entry.href && entry.page > currentPageFromUrl)
      .sort((a, b) => a.page - b.page);
    resolvedNextHref = numberedLinks[0]?.href || null;
  }
  if (!resolvedNextHref) {
    const pageHtml = document.documentElement.innerHTML;
    const metadataNumber = (key) => {
      const directValue = window.BelenConf?.universalAnalyticsOpts?.dimensions?.[key];
      if (directValue) return Number(directValue);
      const match = pageHtml.match(new RegExp('"' + key + '"\\s*:\\s*"?(\\d+)'));
      return match ? Number(match[1]) : 0;
    };
    const dimensions = window.BelenConf?.universalAnalyticsOpts?.dimensions || {};
    const currentPage = metadataNumber("onsite_search_results_page_number") || Number(dimensions.onsite_search_results_page_number || currentPageFromUrl);
    const pageSize = metadataNumber("onsite_search_page_size") || Number(dimensions.onsite_search_page_size || 25);
    const totalResults = metadataNumber("onsite_search_total_results") || Number(dimensions.onsite_search_total_results || 0);
    const totalPages = pageSize > 0 ? Math.ceil(totalResults / pageSize) : 0;
    if (totalPages > 0 && currentPage > 0 && currentPage < totalPages) {
      resolvedNextHref = searchPageUrl(currentPage + 1);
    }
  }

  return {
    listings: result,
    next_page: resolvedNextHref
  };
}
`
