package immoscout

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Kamuyin/kleinanzeigenFinder/providers/health"
)

// decodeResponse checks required envelopes while allowing new, unknown API fields.
// Bodies are bounded and never included in errors or CI reports.
func decodeResponse(resp *http.Response, operation, arrayKey string, target any) error {
	const maxBytes = 8 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return err
	}
	challengeText := ""
	if resp.StatusCode >= 300 || !json.Valid(body) {
		challengeText = string(body)
	}
	if err := health.CheckAccess(ID, operation, resp.StatusCode, challengeText); err != nil {
		return err
	}
	if len(body) > maxBytes {
		return health.Contract(ID, operation, "response exceeds 8 MiB")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return health.Contract(ID, operation, "expected a JSON object; received HTML, invalid JSON, or a different root type")
	}
	field := bytes.TrimSpace(envelope[arrayKey])
	if len(field) == 0 || field[0] != '[' {
		// Some services deliver a JSON challenge/error with HTTP 200.
		if err := health.CheckAccess(ID, operation, resp.StatusCode, string(body)); err != nil {
			return err
		}
		return health.Contract(ID, operation, "missing or non-array "+arrayKey)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return health.Contract(ID, operation, "API field types no longer match the provider model")
	}
	return nil
}
