package memory

import (
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/memorypath"
)

func parseMemoryListPrefix(r *http.Request) (string, error) {
	prefix := strings.TrimSpace(r.URL.Query().Get("path_prefix"))
	if err := memorypath.ValidatePrefix(prefix); err != nil {
		return "", err
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		return "", errUnalignedMemoryPrefix
	}
	return prefix, nil
}
