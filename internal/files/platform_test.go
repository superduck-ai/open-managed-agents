package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestPlatformUploadBase64RejectsTrailingJSON(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/organizations/org_test/files/upload_b64",
		strings.NewReader(`{"file_name":"demo.txt","file_b64":""} {}`),
	)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{
		OrganizationUUID: "org_test",
		WorkspaceUUID:    "workspace_test",
	}))
	recorder := httptest.NewRecorder()

	(&Handler{}).uploadBase64(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "Expected JSON body with file_name and file_b64") {
		t.Fatalf("body = %q, want platform upload JSON error", recorder.Body.String())
	}
}

func TestStreamPlatformObject(t *testing.T) {
	t.Run("failure body read error is logged", func(t *testing.T) {
		response := httptest.NewRecorder()
		readErr := errors.New("read failed")
		object := storage.Object{
			Body: &failingObjectBody{err: readErr},
			Size: 4,
		}

		logOutput := capturePlatformLog(t, func(handler *Handler) {
			handler.streamPlatformObject(context.Background(), response, "file-uuid", "object-key", "preview", object, "application/octet-stream")
		})

		if response.Code != http.StatusOK {
			t.Fatalf("response status = %d, want %d", response.Code, http.StatusOK)
		}
		for _, want := range []string{"stream platform file failed", "variant=preview", "file_uuid=file-uuid", "key=object-key", "bytes_copied=0", "expected_size=4", readErr.Error()} {
			if !strings.Contains(logOutput, want) {
				t.Fatalf("log output = %q, want containing %q", logOutput, want)
			}
		}
	})

	t.Run("failure size mismatch is logged", func(t *testing.T) {
		response := httptest.NewRecorder()
		object := storage.Object{
			Body: io.NopCloser(strings.NewReader("body")),
			Size: 5,
		}

		logOutput := capturePlatformLog(t, func(handler *Handler) {
			handler.streamPlatformObject(context.Background(), response, "file-uuid", "object-key", "thumbnail", object, "application/octet-stream")
		})

		if response.Code != http.StatusOK || response.Body.String() != "body" {
			t.Fatalf("response = status %d body %q", response.Code, response.Body.String())
		}
		for _, want := range []string{"stream platform file size mismatch", "variant=thumbnail", "bytes_copied=4", "expected_size=5"} {
			if !strings.Contains(logOutput, want) {
				t.Fatalf("log output = %q, want containing %q", logOutput, want)
			}
		}
	})

	t.Run("unknown size omits content length", func(t *testing.T) {
		response := httptest.NewRecorder()
		object := storage.Object{
			Body:        io.NopCloser(strings.NewReader("body")),
			Size:        -1,
			ContentType: "text/plain",
		}

		(&Handler{logger: slog.Default()}).streamPlatformObject(context.Background(), response, "file-uuid", "object-key", "preview", object, "application/octet-stream")

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

		(&Handler{logger: slog.Default()}).streamPlatformObject(context.Background(), response, "file-uuid", "object-key", "preview", object, "application/octet-stream")

		if got := response.Header().Get("Content-Length"); got != "4" {
			t.Fatalf("Content-Length = %q, want 4", got)
		}
	})
}

type failingObjectBody struct {
	err error
}

func (f *failingObjectBody) Read([]byte) (int, error) {
	return 0, f.err
}

func (*failingObjectBody) Close() error {
	return nil
}

func capturePlatformLog(t *testing.T, fn func(*Handler)) string {
	t.Helper()
	var output bytes.Buffer
	handler := &Handler{logger: slog.New(slog.NewTextHandler(&output, nil))}
	fn(handler)
	return output.String()
}
