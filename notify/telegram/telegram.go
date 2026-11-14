package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/sirupsen/logrus"
)

type TelegramNotifier struct {
	settings config.TelegramSettings
}

func NewTelegramNotifier(settings config.TelegramSettings) (*TelegramNotifier, error) {
	if settings.Enabled && (settings.BotToken() == "" || settings.ChatID() == "") {
		return nil, fmt.Errorf("Telegram is enabled, but bot token or chat ID is missing")
	}
	return &TelegramNotifier{settings: settings}, nil
}

func (t *TelegramNotifier) NotifyAccepted(ctx context.Context, monitor config.MonitorConfig, listing model.Listing, classification model.Classification) (bool, error) {
	if !t.settings.Enabled {
		logrus.Debugf("Telegram disabled; skipping send for listing_id=%s", listing.Identity())
		return false, nil
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.settings.BotToken())

	msgText := formatMessage(monitor.Name, listing, classification)

	payload := map[string]any{
		"chat_id":                  t.settings.ChatID(),
		"text":                     msgText,
		"disable_web_page_preview": t.settings.DisableWebPagePreview,
	}

	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")

	logrus.Debugf("Sending Telegram message for listing_id=%s", listing.Identity())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("telegram API returned status: %d", resp.StatusCode)
	}

	logrus.Debugf("Telegram accepted message for listing_id=%s", listing.Identity())
	return true, nil
}

func (t *TelegramNotifier) NotifyRejected(ctx context.Context, monitor config.MonitorConfig, listing model.Listing, classification model.Classification) (bool, error) {
	// Telegram only notifies on accepted
	return false, nil
}

func formatMessage(monitorName string, listing model.Listing, classification model.Classification) string {
	var lines []string

	monName := monitorName
	if monName == "" {
		monName = "Kleinanzeigen"
	}
	lines = append(lines, fmt.Sprintf("Neue Anzeige: %s\n", monName))
	lines = append(lines, listing.Title)

	if listing.Price != "" {
		lines = append(lines, fmt.Sprintf("Preis: %s", listing.Price))
	}
	if listing.Location != "" {
		lines = append(lines, fmt.Sprintf("Ort: %s", listing.Location))
	}
	if listing.PostedAt != "" {
		lines = append(lines, fmt.Sprintf("Online: %s", listing.PostedAt))
	}

	var attrs []string
	keys := []string{"Wohnfläche", "Zimmer", "Warmmiete", "Kaltmiete"}
	for _, key := range keys {
		if val, ok := listing.Attributes[key]; ok && val != "" {
			attrs = append(attrs, fmt.Sprintf("%s: %s", key, val))
		}
	}
	if len(attrs) > 0 {
		lines = append(lines, "")
		lines = append(lines, attrs...)
	}

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Score: %d/100", classification.Score))
	lines = append(lines, fmt.Sprintf("Einschaetzung: %s", classification.AssessmentText()))

	if len(classification.RedFlags) > 0 {
		flags := classification.RedFlags
		if len(flags) > 4 {
			flags = flags[:4]
		}
		lines = append(lines, fmt.Sprintf("Warnhinweise: %s", strings.Join(flags, ", ")))
	}

	if listing.Description != "" {
		snippet := strings.TrimSpace(listing.Description)
		if len(snippet) > 500 {
			snippet = snippet[:500] + "..."
		}
		lines = append(lines, "")
		lines = append(lines, snippet)
	}

	lines = append(lines, "")
	lines = append(lines, listing.URL)

	result := strings.Join(lines, "\n")
	if len(result) > 3900 {
		result = result[:3900]
	}
	return result
}
