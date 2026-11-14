() => {
  const text = e => {
    if (!e) return '';
    const copy = e.cloneNode(true);
    copy.querySelectorAll('.clipped, .ux-textspans--CLIPPED, [hidden], script, style').forEach(e => e.remove());
    return (copy.textContent || '').replace(/\s+/g, ' ').trim();
  };
  const plain = value => typeof value === 'string' ? text(new DOMParser().parseFromString(value, 'text/html').body) : '';
  const objects = [];
  const visit = value => {
    if (Array.isArray(value)) value.forEach(visit);
    else if (value && typeof value === 'object') {
      objects.push(value);
      if (value['@graph']) visit(value['@graph']);
    }
  };
  for (const script of document.querySelectorAll('script[type="application/ld+json"]')) {
    try { visit(JSON.parse(script.textContent)); } catch { /* Broken unrelated metadata is not product data. */ }
  }
  const itemID = location.pathname.match(/\/itm\/(?:[^/]+\/)?(\d+)/)?.[1];
  const products = objects.filter(o => [o['@type']].flat().includes('Product'));
  const product = products.find(o => {
    const offers = [o.offers].flat().filter(Boolean);
    return offers.some(v => String(v.url || '').match(/\/itm\/(?:[^/]+\/)?(\d+)/)?.[1] === itemID);
  }) || (products.length === 1 && ![products[0].offers].flat().some(o => o?.url) ? products[0] : {});
  const offer = [product.offers].flat().find(Boolean) || {};
  const attributes = {};
  for (const row of document.querySelectorAll('.ux-labels-values, .ux-layout-section-evo__col')) {
    const name = text(row.querySelector('.ux-labels-values__labels'));
    const value = text(row.querySelector('.ux-labels-values__values'));
    if (name && value) attributes[name.replace(/:$/, '')] = value;
  }
  const condition = text(document.querySelector('.x-item-condition-text .ux-textspans')) ||
    text(document.querySelector('.x-item-condition-text')) || String(offer.itemCondition || '').split('/').pop();
  if (condition) attributes.Condition = condition;
  const bidText = text(document.querySelector('.x-bid-count, .x-bid-count__count'));
  if (bidText) { attributes['Buying format'] = 'Auction'; attributes.Bids = bidText; }
  else if (document.querySelector('#binBtn_btn, .x-bin-action')) attributes['Buying format'] = 'Buy It Now';
  const shipping = text(document.querySelector('.ux-labels-values--shipping .ux-labels-values__values'));
  if (shipping) attributes.Shipping = shipping;
  const locationText = text(document.querySelector('.ux-labels-values--itemLocation .ux-labels-values__values')) ||
    shipping.match(/(?:Artikelstandort|Standort|Located in):\s*(.*)$/i)?.[1] || '';
  const images = [...document.querySelectorAll('.ux-image-carousel .ux-image-carousel-item img, #icImg')]
    .map(e => e.getAttribute('data-zoom-src') ||
      (e.getAttribute('srcset') || '').split(',').map(v => v.trim().split(/\s+/))
        .sort((a,b) => parseInt(b[1] || '0') - parseInt(a[1] || '0'))[0]?.[0] ||
      e.getAttribute('data-src') || e.getAttribute('src')).filter(Boolean);
  for (const entry of [product.image].flat().filter(Boolean)) {
    const url = typeof entry === 'string' ? entry : entry.url || entry.contentUrl;
    if (url) images.push(url);
  }
  const title = text(document.querySelector('.x-item-title__mainTitle, #itemTitle, [data-testid="x-item-title"]')) || plain(product.name);
  const price = text(document.querySelector('.x-price-primary, #prcIsum, #mm-saleDscPrc, #prcIsum_bidPrice')) ||
    (offer.price != null ? String(offer.price) + ' ' + (offer.priceCurrency || '') : '');
  const ended = /\/(?:OutOfStock|SoldOut|Discontinued)$/.test(String(offer.availability || '')) ||
    !!document.querySelector('.x-ended-listing, #vi-cvipEndedMessage');
  return {url: location.href, title, price, currency: offer.priceCurrency || '', location: locationText,
    seller: text(document.querySelector('.x-sellercard-atf__info__about-seller .ux-textspans--BOLD')) ||
      text(document.querySelector('.x-sellercard-atf__info__about-seller a, .mbg-nw')),
    description: text(document.querySelector('#desc_div')) || plain(product.description),
    description_frame: document.querySelector('iframe#desc_ifr')?.src || '', images, attributes, ended};
}
