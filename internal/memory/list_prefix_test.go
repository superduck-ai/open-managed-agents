package memory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestMemoryListRejectsUnalignedPrefixBeforeDataAccess(t *testing.T) {
	for _, prefix := range []string{"/notes", "/notes/a.md", "/notes-archive", "/中文", " /notes "} {
		t.Run(prefix, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/memory_stores/test/memories?path_prefix="+url.QueryEscape(prefix), nil)
			recorder := httptest.NewRecorder()
			func() {
				defer func() {
					if recover() != nil {
						t.Error("invalid prefix reached data access instead of returning400")
					}
				}()
				(&Handler{}).listMemories(recorder, request, "memstore_test")
			}()
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("got%d, want400", recorder.Code)
			}
			var body struct {
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Error.Type != "invalid_request_error" {
				t.Fatalf("invalid error envelope: %s", recorder.Body.String())
			}
		})
	}
}

func TestMemoryListPrefixAcceptsAlignedDirectoryBoundaries(t *testing.T) {
	for _, prefix := range []string{"", "/", "/notes/", "/notes-archive/", "/notes/sub/", "/中文/", "/a b/%_/"} {
		request := httptest.NewRequest(http.MethodGet, "/?path_prefix="+url.QueryEscape(prefix), nil)
		actual, err := parseMemoryListPrefix(request)
		if err != nil || actual != prefix {
			t.Fatalf("prefix=%q actual=%q error=%v", prefix, actual, err)
		}
	}
}
