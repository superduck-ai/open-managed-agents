package files

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestStreamPlatformObject(t *testing.T) {
	t.Run("unknown size omits content length", func(t *testing.T) {
		response := httptest.NewRecorder()
		object := storage.Object{
			Body:        io.NopCloser(strings.NewReader("body")),
			Size:        -1,
			ContentType: "text/plain",
		}

		streamPlatformObject(response, "file-uuid", "object-key", "preview", object, "application/octet-stream")

		if got := response.Header().Get("Content-Length"); got != "" {
			t.Fatalf("Content-Length = %q, want omitted", got)
		}
		if response.Code != http.StatusOK || response.Body.String() != "body" {
			t.Fatalf("response = status %d body %q", response.Code, response.Body.String())
		}
	})

	t.Run("known size sets content length", func(t *testing.T) {
		response := httptest.NewRecorder()
		object := storage.Object{
			Body: io.NopCloser(strings.NewReader("body")),
			Size: 4,
		}

		streamPlatformObject(response, "file-uuid", "object-key", "preview", object, "application/octet-stream")

		if got := response.Header().Get("Content-Length"); got != "4" {
			t.Fatalf("Content-Length = %q, want 4", got)
		}
	})
}
