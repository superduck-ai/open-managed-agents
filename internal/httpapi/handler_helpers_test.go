package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDecodeObjectBodyAsRejectsInvalidBodies(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		maxBodySize int64
		wantError   string
	}{
		{name: "malformed", body: `{"name":`, maxBodySize: 1024, wantError: "Invalid JSON body"},
		{name: "null", body: `null`, maxBodySize: 1024, wantError: "JSON body must be an object"},
		{name: "non object", body: `[]`, maxBodySize: 1024, wantError: "Invalid JSON body"},
		{name: "body too large", body: `{"name":"demo"}`, maxBodySize: int64(len(`{"name":"demo"}`) - 1), wantError: "Invalid JSON body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			_, err := DecodeObjectBodyAs[struct {
				Name string `json:"name"`
			}](rec, req, tt.maxBodySize)
			if err == nil || err.Error() != tt.wantError {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestDecodeObjectBodyAsPreservesMaxBytesError(t *testing.T) {
	payload := `{"name":"demo"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	_, err := DecodeObjectBodyAs[struct {
		Name string `json:"name"`
	}](rec, req, int64(len(payload)-1))

	var maxBytesErr *http.MaxBytesError
	if !errors.As(err, &maxBytesErr) {
		t.Fatalf("error = %v, want wrapped *http.MaxBytesError", err)
	}
	if err.Error() != "Invalid JSON body" {
		t.Fatalf("error = %q, want Invalid JSON body", err)
	}
}

func TestDecodeObjectBodyAsDecodesNamedObject(t *testing.T) {
	type requestBody struct {
		Name string `json:"name"`
	}
	payload := " \n{\"name\":\"demo\"}\t"
	req := httptest.NewRequest("POST", "/", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	body, err := DecodeObjectBodyAs[requestBody](rec, req, int64(len(payload)))
	if err != nil {
		t.Fatalf("DecodeObjectBodyAs error = %v", err)
	}
	if body.Name != "demo" {
		t.Fatalf("name = %q", body.Name)
	}
}

func TestNormalizeMetadata(t *testing.T) {
	raw, err := NormalizeMetadata(json.RawMessage(`{"team":"api"}`), func(metadata map[string]string) error {
		if metadata["team"] != "api" {
			t.Fatalf("metadata = %#v", metadata)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("NormalizeMetadata error = %v", err)
	}
	if string(raw) != `{"team":"api"}` {
		t.Fatalf("raw = %s", raw)
	}
}

func TestPatchMetadata(t *testing.T) {
	raw, err := PatchMetadata(json.RawMessage(`{"keep":"yes","drop":"old"}`), json.RawMessage(`{"drop":null,"add":"new"}`), nil)
	if err != nil {
		t.Fatalf("PatchMetadata error = %v", err)
	}
	if string(raw) != `{"add":"new","keep":"yes"}` {
		t.Fatalf("raw = %s", raw)
	}
}

func TestPatchMetadataUsesValidator(t *testing.T) {
	_, err := PatchMetadata(json.RawMessage(`{}`), json.RawMessage(`{"a":"b"}`), func(map[string]string) error {
		return errors.New("metadata failed validation")
	})
	if err == nil || err.Error() != "metadata failed validation" {
		t.Fatalf("error = %v", err)
	}
}

func TestParseLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/?limit=0", nil)
	limit, err := ParseLimit(req, 100)
	if err != nil {
		t.Fatalf("ParseLimit error = %v", err)
	}
	if limit != 20 {
		t.Fatalf("limit = %d", limit)
	}

	req = httptest.NewRequest("GET", "/?limit=101", nil)
	_, err = ParseLimit(req, 100)
	if err == nil || err.Error() != "limit must be between 1 and 100" {
		t.Fatalf("error = %v", err)
	}
}

func TestFormatTimeUTC(t *testing.T) {
	loc := time.FixedZone("plus-two", 2*60*60)
	got := FormatTime(time.Date(2026, 7, 6, 12, 30, 0, 0, loc))
	if got != "2026-07-06T10:30:00Z" {
		t.Fatalf("FormatTime = %s", got)
	}
}
