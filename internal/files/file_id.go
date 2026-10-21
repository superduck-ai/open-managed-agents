package files

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func validateFileIDPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "file_id")
		if strings.IndexByte(id, 0) >= 0 || !utf8.ValidString(id) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "file_id must be valid UTF-8 without NUL characters"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
