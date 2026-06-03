# KleinanzeigenFinder

KleinanzeigenFinder überwacht Kleinanzeigen-Such-URLs, sammelt neue Anzeigen ein, lädt bei Bedarf die Detailseiten, bewertet die Anzeigen per LLM und verschickt passende Treffer an Discord, Telegram oder WhatsApp.

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
    DEEPSEEK_API_KEY=...
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
api_key_env = "DEEPSEEK_API_KEY"
base_url = "https://api.deepseek.com"
model = "deepseek-chat"
min_score = 70
timeout_seconds = 90
max_retries = 2
retry_delay_seconds = 3
classification_concurrency = 2

```

- `min_score`: Mindestscore für passende Anzeigen.
- `classification_concurrency`: Parallele LLM-Bewertungen.
- `DEEPSEEK_MODEL`: Optionaler ENV-Override für das Modell.

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
