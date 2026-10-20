package files

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestInvalidFileIDRejectedBeforePersistence(t *testing.T) {
	for _, path := range []string{"/nul%00id", "/invalid%FFid"} {
		for _, operation := range []struct{ method, suffix string }{{http.MethodGet, ""}, {http.MethodDelete, ""}, {http.MethodGet, "/content"}} {
			t.Run(operation.method+path+operation.suffix, func(t *testing.T) {
				defer func() {
					if value := recover(); value != nil {
						t.Fatalf("invalid file ID reached persistence: %v", value)
					}
				}()
				handler := NewHandler(config.Config{}, nil, nil, nil)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(operation.method, path+operation.suffix, nil))
				if response.Code != http.StatusBadRequest {
					t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
				}
			})
		}
	}
}

func TestValidFileIDPreservesRouting(t *testing.T) {
	for _, id := range []string{"file_abc123", "opaque-existing-id"} {
		router := chi.NewRouter()
		router.With(validateFileIDPath).Get("/{file_id}", func(w http.ResponseWriter, r *http.Request) {
			if got := chi.URLParam(r, "file_id"); got != id {
				t.Fatalf("ID changed: %q", got)
			}
			w.WriteHeader(http.StatusNoContent)
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+id, nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("valid ID rejected: %d", response.Code)
		}
	}
}
