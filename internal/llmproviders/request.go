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
	model, err := locateRequestModel(body)
	return model.id, err
}

func ReplaceRequestModel(body []byte, modelID string) ([]byte, error) {
	model, err := locateRequestModel(body)
	if err != nil {
		return nil, err
	}
	if model.id == modelID {
		return body, nil
	}
	encoded, err := json.Marshal(modelID)
	if err != nil {
		return nil, err
	}
	replaced := make([]byte, 0, len(body)-(model.end-model.start)+len(encoded))
	replaced = append(replaced, body[:model.start]...)
	replaced = append(replaced, encoded...)
	return append(replaced, body[model.end:]...), nil
}

type requestModel struct {
	id    string
	start int
	end   int
}

func locateRequestModel(body []byte) (requestModel, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil {
		return requestModel{}, ErrRequestBodyNotObject
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return requestModel{}, ErrRequestBodyNotObject
	}

	var model requestModel
	foundModel := false
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return requestModel{}, ErrRequestBodyNotObject
		}
		key, ok := keyToken.(string)
		if !ok {
			return requestModel{}, ErrRequestBodyNotObject
		}
		if key != "model" {
			var skipped json.RawMessage
			if err := decoder.Decode(&skipped); err != nil {
				return requestModel{}, ErrRequestBodyNotObject
			}
			continue
		}
		if foundModel {
			return requestModel{}, ErrDuplicateRequestModel
		}
		foundModel = true
		model.start = valueStart(body, int(decoder.InputOffset()))
		if err := decoder.Decode(&model.id); err != nil {
			return requestModel{}, ErrRequestModelRequired
		}
		model.end = int(decoder.InputOffset())
		if model.id == "" {
			return requestModel{}, ErrRequestModelRequired
		}
		if strings.TrimSpace(model.id) != model.id {
			return requestModel{}, ErrRequestModelWhitespace
		}
	}
	if _, err := decoder.Token(); err != nil {
		return requestModel{}, ErrRequestBodyNotObject
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return requestModel{}, ErrRequestBodyNotObject
	}
	if !foundModel {
		return requestModel{}, ErrRequestModelRequired
	}
	return model, nil
}

func valueStart(body []byte, offset int) int {
	for offset < len(body) {
		switch body[offset] {
		case ' ', '\t', '\r', '\n', ':':
			offset++
		default:
			return offset
		}
	}
	return offset
}
