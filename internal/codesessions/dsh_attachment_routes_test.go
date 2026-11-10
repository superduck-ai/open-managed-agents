package codesessions

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestDSHAttachmentRouteContract(t *testing.T) {
	data, err := os.ReadFile("testdata/dsr_attachment_routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Method string `json:"method"`
		Path   string `json:"path"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(config.Config{}, newTestService(t, nil), nil, nil).WithImageAttachments(nil)
	router := chi.NewRouter()
	router.Route("/v1/dsr", handler.registerDSHCapabilities)
	for _, scenario := range cases {
		t.Run(scenario.Method+" "+scenario.Path, func(t *testing.T) {
			request := httptest.NewRequest(scenario.Method, scenario.Path, nil)
			request.Header.Set("DSR-Protocol", "1")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != scenario.Status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, scenario.Status, response.Body.String())
			}
		})
	}
}
