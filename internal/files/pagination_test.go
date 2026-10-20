package files

import (
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestParseFilePage(t *testing.T) {
	forward := encodeFilePage(filePageCursor{FileID: "file_forward", ScopeID: "scope_a"})
	backward := encodeFilePage(filePageCursor{FileID: "file_backward", ScopeID: "scope_a", Before: true})
	for _, query := range []string{
		"page=invalid!", "page=page_!", "page=page_" + base64.RawURLEncoding.EncodeToString([]byte("null")),
		"page=page_" + base64.RawURLEncoding.EncodeToString([]byte(`{"file_id":false}`)),
		"page=" + forward, "page=" + forward + "&scope_id=scope_b",
		"page=" + forward + "&scope_id=scope_a&after_id=file_other",
		"page=" + forward + "&scope_id=scope_a&before_id=file_other",
	} {
		t.Run("reject "+query, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/?"+query, nil)
			if _, _, err := parseFilePage(r); err == nil {
				t.Fatal("accepted invalid or conflicting page cursor")
			}
		})
	}
	for _, tc := range []struct {
		query  string
		after  string
		before string
	}{
		{},
		{query: "after_id=file_a", after: "file_a"},
		{query: "before_id=file_b", before: "file_b"},
		{query: "after_id=file_a&before_id=file_b", after: "file_a", before: "file_b"},
		{query: "page=" + url.QueryEscape(forward) + "&scope_id=scope_a", after: "file_forward"},
		{query: "page=" + url.QueryEscape(backward) + "&scope_id=scope_a", before: "file_backward"},
	} {
		t.Run("accept "+tc.query, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/?"+tc.query, nil)
			after, before, err := parseFilePage(r)
			if err != nil || after != tc.after || before != tc.before {
				t.Fatalf("got (%q, %q, %v), want (%q, %q, nil)", after, before, err, tc.after, tc.before)
			}
		})
	}
}
