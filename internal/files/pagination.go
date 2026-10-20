package files

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type filePageCursor struct {
	FileID  string `json:"file_id"`
	ScopeID string `json:"scope_id"`
	Before  bool   `json:"before"`
}

func parseFilePage(r *http.Request) (string, string, *httpapi.Error) {
	query := r.URL.Query()
	afterID, beforeID := query.Get("after_id"), query.Get("before_id")
	page := query.Get("page")
	if page == "" {
		return afterID, beforeID, nil
	}
	if afterID != "" || beforeID != "" {
		return "", "", conflictingFilePageError()
	}
	encoded, ok := strings.CutPrefix(page, "page_")
	if !ok {
		return "", "", invalidFilePageError()
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", invalidFilePageError()
	}
	var cursor filePageCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.FileID == "" || cursor.ScopeID != query.Get("scope_id") {
		return "", "", invalidFilePageError()
	}
	if cursor.Before {
		return "", cursor.FileID, nil
	}
	return cursor.FileID, "", nil
}

func encodeFilePage(cursor filePageCursor) string {
	data, _ := json.Marshal(cursor)
	return "page_" + base64.RawURLEncoding.EncodeToString(data)
}
