package llmproviders

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

var (
	ErrRequestBodyNotObject   = errors.New("request body must be a JSON object")
	ErrRequestModelRequired   = errors.New("model is required")
	ErrDuplicateRequestModel  = errors.New("model must appear exactly once")
	ErrRequestModelWhitespace = errors.New("model must not contain surrounding whitespace")
)

const MaxMessagesRequestBodyBytes int64 = 32 << 20

// ApplyAPIKey injects both authentication forms used by supported Anthropic-compatible gateways.
func ApplyAPIKey(headers http.Header, apiKey string) {
	headers.Set("X-Api-Key", apiKey)
	headers.Set("Authorization", "Bearer "+apiKey)
}

// MessageRequestModel returns the single top-level model without rewriting the request body.
func MessageRequestModel(body []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil {
		return "", ErrRequestBodyNotObject
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return "", ErrRequestBodyNotObject
	}

	modelID := ""
	foundModel := false
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return "", ErrRequestBodyNotObject
		}
		key, ok := keyToken.(string)
		if !ok {
			return "", ErrRequestBodyNotObject
		}
		if key != "model" {
			var skipped json.RawMessage
			if err := decoder.Decode(&skipped); err != nil {
				return "", ErrRequestBodyNotObject
			}
			continue
		}
		if foundModel {
			return "", ErrDuplicateRequestModel
		}
		foundModel = true
		if err := decoder.Decode(&modelID); err != nil {
			return "", ErrRequestModelRequired
		}
		if modelID == "" {
			return "", ErrRequestModelRequired
		}
		if strings.TrimSpace(modelID) != modelID {
			return "", ErrRequestModelWhitespace
		}
	}
	if _, err := decoder.Token(); err != nil {
		return "", ErrRequestBodyNotObject
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return "", ErrRequestBodyNotObject
	}
	if !foundModel {
		return "", ErrRequestModelRequired
	}
	return modelID, nil
}
