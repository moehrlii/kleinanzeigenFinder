package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/sirupsen/logrus"
)

type Classifier struct {
	settings config.LLMSettings
	client   *http.Client
}

func NewClassifier(settings config.LLMSettings) (*Classifier, error) {
	if settings.Enabled && settings.APIKey() == "" {
		return nil, fmt.Errorf("LLM is enabled but API key is missing")
	}

	return &Classifier{
		settings: settings,
		client:   &http.Client{Timeout: time.Duration(settings.TimeoutSeconds) * time.Second},
	}, nil
}

func (c *Classifier) DraftMonitor(ctx context.Context, description, searchURL string) (config.MonitorConfig, error) {
	if !c.settings.Enabled || c.settings.APIKey() == "" {
		return config.MonitorConfig{
			Name:                    slugifySimple(description),
			SearchURL:               searchURL,
			Criteria:                description,
			Mode:                    "backfill",
			IntervalSeconds:         300,
			BackfillIntervalSeconds: 300,
			WatchIntervalSeconds:    1800,
			WatchPages:              2,
			MaxResults:              0,
			FetchDetails:            true,
			MaxDetailPages:          8,
			Enabled:                 true,
		}, nil
	}

	systemPrompt := `Draft a Kleinanzeigen monitor config from a user request. Return strict JSON with keys:
name (string, slug), search_url (string), criteria (string), interval_seconds (int),
backfill_interval_seconds (int), watch_interval_seconds (int), watch_pages (int),
max_results (int, use 0 for unlimited search-result cards), fetch_details (bool), max_detail_pages (int),
include_any (array of strings), include_keywords (array of strings),
exclude_keywords (array of strings), max_price (number or null).
The criteria must be a precise instruction for later LLM listing classification.
If no exact URL is provided, create a best-effort Kleinanzeigen search URL.`

	userMsg := fmt.Sprintf("Description: %s\nProvided search_url: %s\nUse conservative rules and good judgement.", description, searchURL)

	body, _ := json.Marshal(map[string]any{
		"model":           c.settings.ModelName(),
		"temperature":     0.0,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userMsg},
		},
	})

	baseURL := c.settings.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	apiURL := fmt.Sprintf("%s/chat/completions", baseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return config.MonitorConfig{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.settings.APIKey())

	resp, err := c.client.Do(req)
	if err != nil {
		return config.MonitorConfig{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return config.MonitorConfig{}, fmt.Errorf("LLM API error: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return config.MonitorConfig{}, err
	}
	if len(result.Choices) == 0 {
		return config.MonitorConfig{}, fmt.Errorf("LLM returned no choices")
	}

	var data map[string]any
	content := result.Choices[0].Message.Content
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		// Try to extract JSON block
		re := regexp.MustCompile(`(?s)\{.*\}`)
		if m := re.FindString(content); m != "" {
			_ = json.Unmarshal([]byte(m), &data)
		}
	}

	draft := config.MonitorConfig{
		Name:                    slugifySimple(strMapVal(data, "name", slugifySimple(description))),
		SearchURL:               strMapVal(data, "search_url", searchURL),
		Criteria:                strMapVal(data, "criteria", description),
		Mode:                    "backfill",
		IntervalSeconds:         intMapVal(data, "interval_seconds", 300),
		BackfillIntervalSeconds: intMapVal(data, "backfill_interval_seconds", 300),
		WatchIntervalSeconds:    intMapVal(data, "watch_interval_seconds", 1800),
		WatchPages:              intMapVal(data, "watch_pages", 2),
		MaxResults:              intMapVal(data, "max_results", 0),
		FetchDetails:            boolMapVal(data, "fetch_details", true),
		MaxDetailPages:          intMapVal(data, "max_detail_pages", 8),
		Enabled:                 true,
		Rules: config.RulesConfig{
			IncludeAny:      strSliceMapVal(data, "include_any"),
			IncludeKeywords: strSliceMapVal(data, "include_keywords"),
			ExcludeKeywords: strSliceMapVal(data, "exclude_keywords"),
			MaxPrice:        floatPtrMapVal(data, "max_price"),
		},
	}
	return draft, nil
}

func slugifySimple(s string) string {
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := re.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 80 {
		slug = slug[:80]
	}
	if slug == "" {
		return "monitor"
	}
	return slug
}

func strMapVal(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func intMapVal(m map[string]any, key string, fallback int) int {
	if m == nil {
		return fallback
	}
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return fallback
}

func boolMapVal(m map[string]any, key string, fallback bool) bool {
	if m == nil {
		return fallback
	}
	if v, ok := m[key].(bool); ok {
		return v
	}
	return fallback
}

func floatPtrMapVal(m map[string]any, key string) *float64 {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(float64); ok {
		return &v
	}
	return nil
}

func strSliceMapVal(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	raw, ok := m[key].([]interface{})
	if !ok {
		return nil
	}
	var result []string
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	return result
}

func (c *Classifier) Classify(ctx context.Context, listing model.Listing, monitor config.MonitorConfig) model.Classification {
	if !c.settings.Enabled {
		logrus.Debug("LLM disabled; accepting listing without classification")
		return model.AcceptedWithoutLLM("")
	}

	logrus.Debugf("Calling LLM model=%s for monitor=%s listing_id=%s", c.settings.ModelName(), monitor.Name, listing.Identity())

	payload := c.buildPayload(listing, monitor)
	payloadJSON, _ := json.Marshal(payload)

	reqBody, _ := json.Marshal(map[string]any{
		"model":       c.settings.ModelName(),
		"temperature": 0.0,
		"messages": []map[string]string{
			{"role": "system", "content": c.settings.SystemPrompt},
			{"role": "user", "content": "Classify this listing and return only JSON:\n" + string(payloadJSON)},
		},
		"response_format": map[string]string{"type": "json_object"},
	})

	baseURL := c.settings.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	url := fmt.Sprintf("%s/chat/completions", baseURL)

	var lastErr error
	for attempt := 1; attempt <= c.settings.MaxRetries+1; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if err != nil {
			lastErr = err
			break
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.settings.APIKey())

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(c.settings.RetryDelaySeconds) * time.Second)
			continue
		}

		if resp.StatusCode == 400 {
			resp.Body.Close()
			reqBody, _ = json.Marshal(map[string]any{
				"model":       c.settings.ModelName(),
				"temperature": 0.0,
				"messages": []map[string]string{
					{"role": "system", "content": c.settings.SystemPrompt},
					{"role": "user", "content": "Classify this listing and return only JSON:\n" + string(payloadJSON)},
				},
			})
			req, _ = http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+c.settings.APIKey())
			resp, err = c.client.Do(req)
			if err != nil {
				lastErr = err
				time.Sleep(time.Duration(c.settings.RetryDelaySeconds) * time.Second)
				continue
			}
		}

		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			resp.Body.Close()
			time.Sleep(time.Duration(c.settings.RetryDelaySeconds) * time.Second)
			continue
		}

		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}

		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(c.settings.RetryDelaySeconds) * time.Second)
			continue
		}

		if len(result.Choices) > 0 {
			return c.parseResponse(result.Choices[0].Message.Content)
		}

		lastErr = fmt.Errorf("no choices returned")
		time.Sleep(time.Duration(c.settings.RetryDelaySeconds) * time.Second)
	}

	logrus.Warnf("LLM classification failed transiently for listing_id=%s: %v", listing.Identity(), lastErr)
	return model.Classification{
		Suitable: false,
		Score:    0,
		Reason:   fmt.Sprintf("LLM classification failed: %v", lastErr),
		Summary:  "LLM did not return a classification in time.",
		RedFlags: []string{"llm_error", "manual_review"},
		Raw:      map[string]any{"error": lastErr.Error()},
	}
}

func (c *Classifier) buildPayload(listing model.Listing, monitor config.MonitorConfig) map[string]any {
	desc := listing.Description
	if len(desc) > c.settings.MaxDescriptionChars {
		desc = desc[:c.settings.MaxDescriptionChars] + "..."
	}

	return map[string]any{
		"monitor":  monitor.Name,
		"criteria": monitor.Criteria,
		"local_rules": map[string]any{
			"include_keywords": monitor.Rules.IncludeKeywords,
			"include_any":      monitor.Rules.IncludeAny,
			"exclude_keywords": monitor.Rules.ExcludeKeywords,
			"min_price":        monitor.Rules.MinPrice,
			"max_price":        monitor.Rules.MaxPrice,
		},
		"listing": map[string]any{
			"title":        listing.Title,
			"price":        listing.Price,
			"price_amount": listing.PriceAmount,
			"location":     listing.Location,
			"posted_at":    listing.PostedAt,
			"attributes":   listing.Attributes,
			"description":  desc,
			"raw_text":     listing.RawText,
			"url":          listing.URL,
		},
	}
}

func (c *Classifier) parseResponse(content string) model.Classification {
	var data map[string]any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		re := regexp.MustCompile(`(?s)\{.*\}`)
		match := re.FindString(content)
		if match == "" {
			return model.Classification{
				Suitable: false,
				Score:    0,
				Reason:   "LLM returned invalid JSON format.",
				Raw:      map[string]any{"raw_response": content},
			}
		}
		if err := json.Unmarshal([]byte(match), &data); err != nil {
			return model.Classification{
				Suitable: false,
				Score:    0,
				Reason:   "LLM returned invalid JSON format.",
				Raw:      map[string]any{"raw_response": content},
			}
		}
	}

	scoreFloat, _ := data["score"].(float64)
	score := int(scoreFloat)
	suitableBool, _ := data["suitable"].(bool)

	suitable := suitableBool && score >= c.settings.MinScore

	reason, _ := data["reason"].(string)
	if reason == "" {
		reason = "No reason returned."
	}

	summary, _ := data["summary"].(string)

	var redFlags []string
	if rf, ok := data["red_flags"].([]interface{}); ok {
		for i, flag := range rf {
			if i >= 8 {
				break
			}
			redFlags = append(redFlags, fmt.Sprintf("%v", flag))
		}
	}

	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}

	return model.Classification{
		Suitable: suitable,
		Score:    score,
		Reason:   reason,
		Summary:  summary,
		RedFlags: redFlags,
		Raw:      data,
	}
}
