# KleinanzeigenFinder

KleinanzeigenFinder überwacht Kleinanzeigen- und ImmoScout24-Such-URLs, sammelt neue Anzeigen ein, lädt bei Bedarf die Detailseiten, bewertet die Anzeigen per LLM und verschickt passende Treffer an Discord, Telegram oder WhatsApp.

## Funktionen

- Mehrere Monitore mit eigenen Such-URLs und Kriterien
- LLM-Klassifizierung mit Score, Begründung und Zusammenfassung
- Benachrichtigungen über Discord, Telegram und optional WhatsApp
- Discord-Bot zur vollständigen Steuerung der Monitore

## Start Mit Docker

Voraussetzungen:

- Docker
- Docker Compose

1. Beispielkonfiguration kopieren:

    ```powershell
    Copy-Item config.example.toml config.toml
    Copy-Item .env.example .env
    ```

2. `.env` ausfüllen:

    ```env
    OPENAI_API_KEY=...
    DISCORD_BOT_TOKEN=...
    DISCORD_GUILD_ID=...
    TELEGRAM_BOT_TOKEN=...
    TELEGRAM_CHAT_ID=...
    ```

3. `config.toml` anpassen.

    Für Discord zum Beispiel:

    ```toml
    [discord]
    enabled = true
    token_env = "DISCORD_BOT_TOKEN"
    guild_id_env = "DISCORD_GUILD_ID"
    admin_role_ids = []
    category_name = "kleinanzeigen"
    review_channel_name = "kf-review"
    channel_prefix = "kf-"
    sync_commands = true
    ```

4. Container builden und starten:

    ```powershell
    docker compose up -d --build
    ```

5. Stoppen:

    ```powershell
    docker compose down
    ```

## Lokaler Start Ohne Docker

1. Abhängigkeiten laden:

    ```powershell
    go mod download
    ```

2. Playwright/Chromium installieren:

    ```powershell
    go run https://github.com/playwright-community/playwright-go/cmd/playwright@v0.5700.1 install --with-deps chromium
    ```

3. Konfiguration anlegen:

    ```powershell
    Copy-Item config.example.toml config.toml
    Copy-Item .env.example .env
    ```

4. Starten:

    ```powershell
    go run . run
    ```

    Einmalig:

    ```powershell
    go run . run --once --no-notify
    ```

## Konfiguration

### App

```toml
[app]
database = "data/seen.sqlite3"

```

Die SQLite-Datenbank enthält bekannte Anzeigen, Queue-Einträge, Monitor-Konfigurationen und Discord-Metadaten.

### Logging

```toml
[logging]
level = "INFO"
file = "logs/kleinanzeigen-finder.log"
rotation = "10 MB"
retention = "14 days"

```

### Scraper

```toml
[scraper]
headless = true
timeout_ms = 30000
detail_delay_seconds = 0.8
detail_concurrency = 1

```

- `headless`: Chromium ohne sichtbares Fenster ausführen.
- `timeout_ms`: Timeout für Seitenaufrufe.
- `detail_delay_seconds`: Mindestabstand zwischen Detailseiten-Abrufen.
- `detail_concurrency`: Parallele Detailseiten-Abrufe. Vorsichtig erhöhen.

### LLM

```toml
[llm]
enabled = true
api_key_env = "OPENAI_API_KEY"
base_url = "https://api.openai.com/v1"
model = "gpt-6-astra"
min_score = 70
timeout_seconds = 90
max_retries = 2
retry_delay_seconds = 3
classification_concurrency = 2

```

- `min_score`: Mindestscore für passende Anzeigen.
- `classification_concurrency`: Parallele LLM-Bewertungen.
- `OPENAI_MODEL`: Optionaler ENV-Override für das Modell.

### Telegram

```toml
[telegram]
enabled = true
bot_token_env = "TELEGRAM_BOT_TOKEN"
chat_id_env = "TELEGRAM_CHAT_ID"
disable_web_page_preview = false

```

### Discord

```toml
[discord]
enabled = true
token_env = "DISCORD_BOT_TOKEN"
guild_id_env = "DISCORD_GUILD_ID"
admin_role_ids = []
category_name = "kleinanzeigen"
review_channel_name = "kf-review"
channel_prefix = "kf-"
sync_commands = true

```

### WhatsApp

```toml
[whatsapp]
enabled = false
session_db = "data/whatsapp_session.sqlite3"

```

Beim ersten Start wird ein QR-Code in den Logs ausgegeben. Den Code mit WhatsApp unter `Verknüpfte Geräte` scannen.

## Discord-Bedienung

Wichtige Commands:

- `/kf status`: Zeigt an, ob der Bot läuft und wie viele Monitore existieren.
- `/monitor list`: Listet alle Monitore.
- `/monitor create`: Erstellt einen Monitor manuell oder per AI-Wizard.
- `/monitor edit name:<name>`: Bearbeitet einen Monitor.
- `/monitor delete name:<name>`: Löscht einen Monitor.
- `/monitor pause name:<name>`: Pausiert einen Monitor.
- `/monitor resume name:<name>`: Aktiviert einen Monitor wieder.
- `/monitor sync-channels`: Erstellt oder verknüpft Discord-Kanäle.
- `/monitor test name:<name>`: Zeigt Details und prüft den Kanal.

## Monitore

Monitore werden aktuell primär über Discord in der SQLite-Datenbank erstellt.

Eine typische Such-URL sollte bereits auf Kleinanzeigen so gefiltert sein, wie du sie überwachen willst, zum Beispiel:

```text
https://www.kleinanzeigen.de/s-wohnung-mieten/berlin/sortierung:neuste/preis::1000/c203l3331r5
```

- Sortierung auf `neuste` setzen.
- Preis, Ort, Radius und Kategorie direkt in der Such-URL setzen.
- Kriterien im Monitor als natürliche Sprache beschreiben, zum Beispiel: `2 Zimmer, Balkon nice-to-have, keine Suchanzeigen, Warmmiete unter 1000 Euro, etc...`.


## OpenAI, Bilder und AI-Wizard

Die Bewertung verwendet die OpenAI Responses API. In `.env` wird `OPENAI_API_KEY`
gesetzt. Bestehende `config.toml`-Dateien m?ssen im Abschnitt `[llm]` auf
`api_key_env = "OPENAI_API_KEY"`, `base_url = "https://api.openai.com/v1"` und
`model = "gpt-6-astra"` umgestellt werden. `OPENAI_MODEL` ?berschreibt das Modell.
Das gew?hlte Modell muss Bild-Eingaben und Structured Outputs ?ber Responses unterst?tzen.

`max_images = 8` begrenzt die Bilder pro Bewertung (0 deaktiviert Bilder, Maximum 32).
`image_detail = "auto"` kann auch `low` oder `high` sein. Bilder werden ?ber ihre
?ffentlichen HTTPS-URLs an OpenAI ?bergeben und verursachen zus?tzliche API-Nutzung.
Detailseiten liefern die Galerie, ?ltere gespeicherte Anzeigen k?nnen ihr Vorschaubild nutzen.
Wenn OpenAI ein Bild nicht abrufen kann, bleibt die Bewertung zur Wiederholung in der Queue.
Ohne verf?gbare Bilder erfolgt eine Textbewertung mit entsprechendem Hinweis.

Die Benachrichtigung enth?lt die gemeinsame Bewertung, sichtbare Merkmale aus Fotos
und offene Fragen. Aus Bildern werden keine gesicherten Aussagen zu unsichtbaren M?ngeln,
exakten Ma?en oder nicht gezeigten Ausstattungsmerkmalen abgeleitet.

?ber `/monitor create` ? AI Wizard lassen sich W?nsche, Muss-Kriterien, Budget und
visuelle Vorlieben in nat?rlicher Sprache eingeben. Der Wizard erstellt Such-URL,
Bewertungskriterien, lokale Regeln und Scan-Einstellungen. Eine angegebene Such-URL
bleibt erhalten. Ohne URL erstellt die AI eine breite Stichwortsuche. Orts- und
Budgetanforderungen bleiben dann in den Bewertungskriterien und sollten anhand der
vorgeschlagenen Suche gepr?ft werden. Die Vorschau kann gek?rzt sein. Der vollst?ndige
Entwurf wird f?r 24 Stunden in SQLite gespeichert und beim Speichern oder manuellen
Bearbeiten verwendet, einschlie?lich Preis- und Keyword-Regeln.

Fehlgeschlagene AI-Bewertungen bleiben in der Queue. Sie werden bei folgenden L?ufen
erneut versucht. Fehlversuche werden hinter Eintr?gen mit weniger Versuchen verarbeitet.
`--no-notify` verarbeitet und speichert weiterhin Anzeigen, ohne Benachrichtigungen zu senden.

## ImmoScout24

The restored provider uses the ImmoScout24 mobile API for search and listing details.
Optional settings in `config.toml` have these defaults.

```toml
[immoscout]
api_base_url = "https://api.mobile.immobilienscout24.de"
user_agent = "ImmoScout_27.12_26.2_._"
page_size = 50
```

Paste an ImmoScout search URL into the manual monitor form or AI wizard. The registry
selects ImmoScout and the wizard uses its search instructions. Regional and radius
searches support apartment and house rentals and purchases, including price, rooms,
area and equipment filters. Radius coordinates use semicolons, which are preserved
when translating the URL. Drawn shape searches return an unsupported-search error.

Example search URL

```text
https://www.immobilienscout24.de/Suche/de/berlin/berlin/wohnung-mieten?price=-1200&pricetype=calculatedtotalrent&numberofrooms=2.0-&sorting=2
```

The provider retains `is24:` listing IDs from the older implementation. Details include
warm rent, attributes, description and gallery URLs for OpenAI assessment. Placeholder
images are excluded from the gallery. The integration depends on the mobile API's
response format and availability.

## Modular providers

Marketplace implementations live under `providers/`.

| Package | Responsibility | Status |
| --- | --- | --- |
| `providers/kleinanzeigen` | Browser lifecycle, pagination, listing and gallery extraction, URL validation, search instructions | Implemented |
| `providers/kleinanzeigen/parsing` | Kleinanzeigen listing IDs and compatibility access to value parsing | Implemented |
| `providers/parsing` | Shared German price, room-count and area parsing | Implemented |
| `providers/immoscout` | Mobile API discovery, URL translation, listing details and gallery extraction | Implemented |
| `providers/ebay` | eBay integration | Reserved for implementation |
| `providers/wggesucht` | WG-Gesucht integration | Reserved for implementation |
| `providers` | Shared interfaces, registry, built-in registrations and legacy platform identity | Implemented |

`providers/builtin.go` registers the built-in marketplaces. Startup opens the available
providers and closes their resources on exit. The runner receives their instances.
The AI wizard and manual monitor form use the same registry to identify and validate
search URLs. Marketplace-specific search instructions live in the provider package.
Planned integrations return an explicit unavailable error before scraping or AI generation.
A wizard request without a URL uses the registry default, currently Kleinanzeigen.
`DraftMonitorForPlatform` also supports explicit platform selection.

To implement another marketplace, add its scraper and extraction logic to its package,
return normalized `model.Listing` values with stable IDs and offer gallery URLs, and
implement `FetchListingsPages`, `FetchDetail` and `Close`. Add a URL validator and AI
search instructions, then complete its `Definition` in `providers/builtin.go` with an
`Open` factory. Providers must support concurrent monitor calls. The runner, queue,
classification client and notification transports use the shared listing model.

Existing monitors with an empty platform recover their marketplace from the search URL,
including ImmoScout monitors from older versions. Unrecognized or absent URLs retain the
legacy Kleinanzeigen fallback. Explicit platform identities are preserved. Monitor names
remain globally unique. Changing an existing monitor's platform requires a new
name to preserve the meaning of stored listing IDs and feedback. The former `scraper`
and `scraper/parsing` packages forward their public entry points for compatibility.

## Tests

Run `go test ./...` for registry routing, provider-independent wizard generation,
OpenAI request formatting, queue recovery, persisted drafts and parsing tests.
For the local gallery browser fixture with Playwright/Chromium installed, set
`$env:KF_BROWSER_TESTS = "1"` in PowerShell and run `go test ./providers/kleinanzeigen`.
The fixture blocks external image requests.

For the opt-in live ImmoScout check, set `$env:KF_LIVE_IMMOSCOUT = "1"` and run
`go test ./providers/immoscout -run TestImmoScoutFetchListingsLiveKarlsruhe -v`.
This fetches a bounded search result and one detail response without sending notifications.
