package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/model"
)

func (c *Classifier) listingImages(listing model.Listing) []string {
	var result []string
	candidates := append(slices.Clone(listing.ImageURLs), listing.ImageURL)
	for _, candidate := range candidates {
		if len(result) >= c.settings.MaxImages {
			break
		}
		u, err := url.Parse(candidate)
		if err == nil && u.Scheme == "https" && u.Host != "" && !slices.Contains(result, candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

func (c *Classifier) complete(ctx context.Context, instructions, input string, images []string, name string, schema map[string]any) (string, error) {
	content := []map[string]any{{"type": "input_text", "text": input}}
	for _, image := range images {
		content = append(content, map[string]any{"type": "input_image", "image_url": image, "detail": c.settings.ImageDetail})
	}
	body, err := json.Marshal(map[string]any{
		"model": c.settings.ModelName(), "store": false,
		"instructions": instructions,
		"input":        []map[string]any{{"role": "user", "content": content}},
		"text":         map[string]any{"format": map[string]any{"type": "json_schema", "name": name, "strict": true, "schema": schema}},
	})
	if err != nil {
		return "", err
	}
	var lastErr error
	for attempt := 0; attempt <= c.settings.MaxRetries; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(c.settings.RetryDelaySeconds * float64(time.Second)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			case <-timer.C:
			}
		}
		output, retry, err := c.request(ctx, body)
		if err == nil {
			return output, nil
		}
		lastErr = err
		if !retry || ctx.Err() != nil {
			break
		}
	}
	return "", lastErr
}

func (c *Classifier) request(ctx context.Context, body []byte) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.settings.BaseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.settings.APIKey())
	resp, err := c.client.Do(req)
	if err != nil {
		return "", true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode == 429 || resp.StatusCode >= 500, fmt.Errorf("OpenAI HTTP %d", resp.StatusCode)
	}
	var result struct {
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil {
		return "", true, fmt.Errorf("decode OpenAI response: %w", err)
	}
	if result.Status != "completed" {
		return "", false, fmt.Errorf("OpenAI response status %q", result.Status)
	}
	for _, item := range result.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "refusal" {
				return "", false, fmt.Errorf("OpenAI declined assessment")
			}
			if part.Type == "output_text" && part.Text != "" {
				return part.Text, false, nil
			}
		}
	}
	return "", false, fmt.Errorf("OpenAI returned no assessment")
}

func objectSchema(properties map[string]any) map[string]any {
	required := slices.Sorted(maps.Keys(properties))
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func classificationSchema() map[string]any {
	text := map[string]any{"type": "string"}
	list := map[string]any{"type": "array", "items": text}
	return objectSchema(map[string]any{
		"suitable": map[string]any{"type": "boolean"},
		"score":    map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
		"reason":   text, "summary": text, "red_flags": list,
		"visual_assessment": text, "uncertainties": list,
	})
}

func draftSchema() map[string]any {
	text := map[string]any{"type": "string"}
	list := map[string]any{"type": "array", "items": text}
	interval := map[string]any{"type": "integer", "minimum": 60}
	positive := map[string]any{"type": "integer", "minimum": 1}
	price := map[string]any{"type": []string{"number", "null"}, "minimum": 0}
	return objectSchema(map[string]any{
		"name": text, "search_url": text, "criteria": map[string]any{"type": "string", "maxLength": 1500},
		"interval_seconds": interval, "backfill_interval_seconds": interval, "watch_interval_seconds": interval,
		"watch_pages": positive, "max_detail_pages": positive,
		"max_results":   map[string]any{"type": "integer", "minimum": 0},
		"fetch_details": map[string]any{"type": "boolean"},
		"include_any":   list, "include_keywords": list, "exclude_keywords": list,
		"min_price": price, "max_price": price,
	})
}
