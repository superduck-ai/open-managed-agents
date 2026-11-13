package codesessions

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestToolCallPathIdentitySurvivesURLEncoding(t *testing.T) {
	for _, tc := range []struct{ name, threadID, callID string }{
		{"colon", "thread:child", "toolu_functions.Bash:0"},
		{"slash", "thread/child", "call/tool"},
		{"literal_escape", "root", "call%2Fvalue"},
		{"special_characters", "thread +?", "call:?&=% #"},
		{"unicode", "thread-\u4e2d\u6587", "call-\u4f60\u597d"},
		{"unreserved", "root", "call_simple-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := chi.NewRouter()
			router.Get("/threads/{thread_id}/tool-calls/{call_id}", func(w http.ResponseWriter, r *http.Request) {
				threadID, callID, err := toolCallPathParams(r)
				if err != nil {
					t.Fatal(err)
				}
				if threadID != tc.threadID || callID != tc.callID {
					t.Errorf("identity changed: thread=%q call=%q", threadID, callID)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			escape := func(value string) string { return strings.ReplaceAll(url.QueryEscape(value), "+", "%20") }
			request := httptest.NewRequest(http.MethodGet, "/threads/"+escape(tc.threadID)+"/tool-calls/"+escape(tc.callID), nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("request status=%d", response.Code)
			}
		})
	}
}
