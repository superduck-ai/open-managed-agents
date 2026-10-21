package memory

import (
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestParseOptionalDepth(t *testing.T) {
	for _, tc := range []struct {
		name, raw        string
		invalid, subtree bool
	}{
		{name: "negative rejected", raw: "-1", invalid: true},
		{name: "two rejected", raw: "2", invalid: true},
		{name: "larger rejected", raw: "10", invalid: true},
		{name: "fraction rejected", raw: "1.5", invalid: true},
		{name: "text rejected", raw: "invalid", invalid: true},
		{name: "omitted lists subtree", subtree: true},
		{name: "zero lists subtree", raw: "0", subtree: true},
		{name: "one lists children", raw: "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/memory_stores/test/memories?depth="+url.QueryEscape(tc.raw), nil)
			depth, err := parseOptionalDepth(request)
			if (err != nil) != tc.invalid {
				t.Fatalf("depth=%v error=%v", depth, err)
			}
			if tc.invalid {
				return
			}
			if tc.subtree {
				if depth != nil {
					t.Fatalf("subtree should use unbounded listing, depth=%d", *depth)
				}
			} else if depth == nil || *depth != 1 {
				t.Fatalf("immediate-child depth=%v", depth)
			}
		})
	}
}
