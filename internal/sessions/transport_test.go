package sessions

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func TestServeHTTPAcceptsSessionsWithoutBetaQuery(t *testing.T) {
	router := chi.NewRouter()
	router.Post("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	h := &Handler{router: router, errorAdapter: httpapi.NewErrorAdapter(slog.Default())}

	for _, test := range []struct {
		name string
		path string
	}{
		{name: "without beta", path: "/"},
		{name: "beta false", path: "/?beta=false"},
		{name: "beta true", path: "/?beta=true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, test.path, nil))
			if response.Code != http.StatusCreated {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
			}
		})
	}
}
