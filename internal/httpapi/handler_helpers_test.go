package httpapi

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDecodeObjectBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"demo"}`))
	rec := httptest.NewRecorder()
	fields, err := DecodeObjectBody(rec, req, 1024)
	if err != nil {
		t.Fatalf("DecodeObjectBody error = %v", err)
	}
	if string(fields["name"]) != `"demo"` {
		t.Fatalf("name = %s", fields["name"])
	}
}

func TestDecodeObjectBodyRejectsNonObject(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`null`))
	rec := httptest.NewRecorder()
	_, err := DecodeObjectBody(rec, req, 1024)
	if err == nil || err.Error() != "JSON body must be an object" {
		t.Fatalf("error = %v", err)
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
