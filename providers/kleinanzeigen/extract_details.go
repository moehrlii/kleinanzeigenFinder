package kleinanzeigen

const DetailExtractScript = `
() => {
  const clean = (value) => (value || "").replace(/\s+/g, " ").trim();
  const lines = (document.querySelector("main article")?.innerText || document.body.innerText || "")
    .split(/\n+/)
    .map((line) => clean(line))
    .filter(Boolean);
  const article = document.querySelector("main article") || document.querySelector("article");
  const abs = (href) => {
    try { return new URL(href, location.href).href; } catch { return location.href; }
  };
  const idFromUrl = (href) => {
    const match = String(href || "").match(/\/(\d+)-\d+-\d+(?:[/?#]|$)/);
    return match ? match[1] : null;
  };

  const title = clean(article?.querySelector("h1")?.innerText)
    || clean(document.querySelector("h1")?.innerText);
  const price = lines.find((line) =>
    /^(?:\d{1,3}(?:[.\s]\d{3})*|\d+)(?:,\d{1,2})?\s*(?:€|EUR)\b/i.test(line)
  ) || null;
  const locationPattern = new RegExp(
    "\\b\\d{5}\\b.*(Baden|Berlin|Hamburg|Hessen|Bayern|Karlsruhe|Deutschland)",
    "i"
  );
  const locationLine = lines.find((line) => locationPattern.test(line))
    || lines.find((line) => /\b\d{5}\b/.test(line))
    || null;
  const postedAt = lines.find((line) =>
    /^(Heute|Gestern)(?:,?\s+\d{1,2}:\d{2})?$|^\d{1,2}\.\d{1,2}\.\d{4}$/i
      .test(line)
  ) || null;
  const labels = new Set([
    "Wohnfläche", "Zimmer", "Schlafzimmer", "Badezimmer", "Etage", "Wohnungstyp",
    "Verfügbar ab", "Nebenkosten", "Heizkosten", "Warmmiete", "Kaltmiete",
    "Kaution", "Provision", "Marke", "Modell", "Farbe", "Zustand"
  ]);
  const attributes = {};
  for (let index = 0; index < lines.length - 1; index += 1) {
    if (labels.has(lines[index])) {
      attributes[lines[index]] = lines[index + 1];
    }
  }

  const descriptionStart = lines.findIndex((line) => line === "Beschreibung");
  let description = null;
  if (descriptionStart >= 0) {
    const stopLabels = new Set([
      "Nachricht schreiben",
      "Andere Anzeigen des Anbieters",
      "Das könnte dich auch interessieren",
      "Anzeige teilen",
    ]);
    const end = lines.findIndex((line, index) => index > descriptionStart && stopLabels.has(line));
    description = lines.slice(descriptionStart + 1, end > 0 ? end : undefined).join("\n").trim();
  }

  const seller = lines.find((line) => /^(Privater Nutzer|Gewerblicher Nutzer|PRO\b)/i.test(line))
    || null;
  // Restrict extraction to the offer gallery, excluding recommendations and seller avatars.
  const imageNodes = Array.from(document.querySelectorAll(
    '#viewad-image img, #viewad-image-gallery img, .galleryimage-large img, img.galleryimage-large, .galleryimage-element[data-imgsrc], [data-testid="vip-image-gallery"] img'
  ));
  const imageURLs = [...new Set(imageNodes.map(img =>
    img.getAttribute('data-imgsrc') || img.getAttribute('data-src') || img.currentSrc || img.src
  ).filter(Boolean).map(abs).filter(url => url.startsWith('https://')))];
  const socialImage = document.querySelector('meta[property="og:image"]')?.content;
  if (!imageURLs.length && socialImage) imageURLs.push(abs(socialImage));
  const image = imageURLs[0] || null;

  return {
    id: idFromUrl(location.href),
    url: abs(location.href),
    title,
    price,
    location: locationLine,
    posted_at: postedAt,
    description,
    image_url: image,
    image_urls: imageURLs,
    seller,
    raw_text: lines.join("\n"),
    attributes,
  };
}
`
