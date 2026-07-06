package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type MetadataValidator func(map[string]string) error

func DecodeObjectBody(w http.ResponseWriter, r *http.Request, maxBodySize int64) (map[string]json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&fields); err != nil {
		return nil, errors.New("Invalid JSON body")
	}
	if fields == nil {
		return nil, errors.New("JSON body must be an object")
	}
	return fields, nil
}

func MarshalRaw(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func IsJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func NormalizeMetadata(raw json.RawMessage, validate MetadataValidator) (json.RawMessage, error) {
	if IsJSONNull(raw) {
		return json.RawMessage(`{}`), nil
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, errors.New("metadata must be an object with string values")
	}
	if validate != nil {
		if err := validate(metadata); err != nil {
			return nil, err
		}
	}
	return MarshalRaw(metadata)
}

func ValidateMetadataEntryLimit(metadata map[string]string, max int, message string) error {
	if len(metadata) > max {
		return errors.New(message)
	}
	return nil
}

func PatchMetadata(current json.RawMessage, raw json.RawMessage, validate MetadataValidator) (json.RawMessage, error) {
	if IsJSONNull(raw) {
		return json.RawMessage(`{}`), nil
	}
	var metadata map[string]string
	if len(current) > 0 && !IsJSONNull(current) {
		if err := json.Unmarshal(current, &metadata); err != nil {
			return nil, errors.New("stored metadata is invalid")
		}
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	var patch map[string]*string
	if err := json.Unmarshal(raw, &patch); err != nil {
		return nil, errors.New("metadata must be an object with string or null values")
	}
	for key, value := range patch {
		if value == nil || *value == "" {
			delete(metadata, key)
			continue
		}
		metadata[key] = *value
	}
	if validate != nil {
		if err := validate(metadata); err != nil {
			return nil, err
		}
	}
	return MarshalRaw(metadata)
}

func RawOr(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

func FormatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func OptionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	value := FormatTime(*t)
	return &value
}

func ParseLimit(r *http.Request, max int) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 20, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 || limit > max {
		return 0, fmt.Errorf("limit must be between 1 and %d", max)
	}
	if limit == 0 {
		return 20, nil
	}
	return limit, nil
}

func ParseOptionalTime(r *http.Request, name string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be RFC3339", name)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}
