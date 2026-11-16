package skills

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsolePackagePreviewLimit(t *testing.T) {
	pkg, err := skillPackageFromFiles([]normalizedSkillFile{{Name: "demo/SKILL.md", Data: []byte("---\nname: demo\ndescription: Test\n---\n" + strings.Repeat("a", 256*1024))}}, MaxSkillPackageBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, console := range []bool{true, false} {
		t.Run(map[bool]string{true: "console rejects unpreviewable content", false: "SDK keeps existing archive limit"}[console], func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("files[]", "demo.zip")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = part.Write(pkg.Zip); err != nil {
				t.Fatal(err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/", &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			if console {
				request = request.WithContext(context.WithValue(request.Context(), consoleSkillContextKey{}, true))
			}
			_, err = readSkillPackage(httptest.NewRecorder(), request, MaxSkillPackageBytes)
			if !console {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			rejected, ok := errors.AsType[packageError](err)
			if !ok || rejected.Status != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected 413 for console upload, got %v", err)
			}
		})
	}
}
