package brainattachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type attachmentDatabase struct {
	records map[string]db.BrainImageAttachment
	source  db.FileRecord
}

func (d *attachmentDatabase) GetBrainImageAttachment(_ context.Context, workspace, session, digest string) (db.BrainImageAttachment, error) {
	image, found := d.records[workspace+"/"+session+"/"+digest]
	if !found {
		return image, db.ErrNotFound
	}
	return image, nil
}

func (d *attachmentDatabase) PutBrainImageAttachment(_ context.Context, image db.BrainImageAttachment, upload func() error) (bool, error) {
	key := image.WorkspaceUUID + "/" + image.SessionUUID + "/" + image.SHA256
	if _, found := d.records[key]; found {
		return false, nil
	}
	if err := upload(); err != nil {
		return false, err
	}
	d.records[key] = image
	return true, nil
}

func (d *attachmentDatabase) GetSessionAttachmentFile(_ context.Context, workspace, session, id string) (db.FileRecord, error) {
	if workspace != "workspace" || session != "session" || id != "file_private" {
		return db.FileRecord{}, db.ErrNotFound
	}
	return d.source, nil
}

type attachmentObjects struct {
	storage.ObjectStore
	data        map[string][]byte
	uploads     int
	readFailure error
}

func (s *attachmentObjects) Name() string { return "images" }
func (s *attachmentObjects) Upload(_ context.Context, key string, body io.Reader, _ storage.UploadOptions) (storage.UploadResult, error) {
	data, err := io.ReadAll(body)
	if err == nil {
		s.data[key] = data
		s.uploads++
	}
	return storage.UploadResult{Size: int64(len(data))}, err
}
func (s *attachmentObjects) Open(_ context.Context, key string, _ *storage.ByteRange) (storage.Object, error) {
	if s.readFailure != nil {
		return storage.Object{Body: io.NopCloser(iotest.ErrReader(s.readFailure))}, nil
	}
	data, found := s.data[key]
	if !found {
		return storage.Object{}, db.ErrNotFound
	}
	return storage.Object{Body: io.NopCloser(bytes.NewReader(data)), Size: int64(len(data))}, nil
}

func TestImageAttachmentHTTPContract(t *testing.T) {
	scope := Scope{OrganizationUUID: "organization", WorkspaceUUID: "workspace", SessionUUID: "session"}
	database := &attachmentDatabase{records: map[string]db.BrainImageAttachment{}}
	objects := &attachmentObjects{data: map[string][]byte{}}
	handler := NewHandler(database, objects, func(w http.ResponseWriter, r *http.Request) (Scope, bool) {
		if r.Header.Get("X-Worker-Epoch") != "1" {
			w.WriteHeader(http.StatusConflict)
			return Scope{}, false
		}
		return scope, true
	}, 0, nil)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)
	image := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 20))
	digest := sha256.Sum256(image)
	path := "/image-attachments/" + hex.EncodeToString(digest[:])
	request := func(method, target, mime string, body []byte, epoch string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", mime)
		req.Header.Set("X-Worker-Epoch", epoch)
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		return result
	}
	t.Run("reject invalid bytes before storage", func(t *testing.T) {
		for _, input := range []struct {
			path, mime string
			data       []byte
			status     int
		}{
			{path, "image/png", []byte("bad"), 400},
			{path, "image/jpeg", image, 400},
			{"/image-attachments/INVALID", "image/png", image, 400},
			{path, "image/png", make([]byte, MaxObjectBytes+1), 413},
		} {
			if got := request("PUT", input.path, input.mime, input.data, "1").Code; got != input.status {
				t.Fatalf("status = %d, want %d", got, input.status)
			}
		}
		if objects.uploads != 0 || len(database.records) != 0 {
			t.Fatal("invalid input reached storage")
		}
		if got := request("PUT", path, "image/png", image, "0").Code; got != 409 {
			t.Fatalf("stale epoch = %d", got)
		}
	})
	t.Run("private source does not resolve workspace files", func(t *testing.T) {
		for _, id := range []string{"file_workspace", "file_other_session"} {
			if got := request("GET", "/message-attachments/"+id+"/content", "", nil, "1").Code; got != 404 {
				t.Fatalf("source %s = %d", id, got)
			}
		}
	})
	t.Run("foreign source bucket is a permanent missing attachment", func(t *testing.T) {
		database.source = db.FileRecord{S3Bucket: "other-bucket"}
		if got := request("GET", "/message-attachments/file_private/content", "", nil, "1").Code; got != 404 {
			t.Fatalf("foreign bucket status = %d", got)
		}
	})

	t.Run("upload is immutable and scoped", func(t *testing.T) {
		if got := request("PUT", path, "image/png", image, "1").Code; got != 201 {
			t.Fatalf("upload = %d", got)
		}
		if got := request("PUT", path, "image/png", image, "1").Code; got != 200 {
			t.Fatalf("repeat = %d", got)
		}
		if objects.uploads != 1 {
			t.Fatalf("uploads = %d", objects.uploads)
		}
		if got := request("PUT", path, "image/png", []byte("bad"), "1").Code; got != 400 {
			t.Fatalf("bad overwrite = %d", got)
		}
		result := request("GET", path, "", nil, "1")
		if result.Code != 200 || !bytes.Equal(result.Body.Bytes(), image) {
			t.Fatalf("read = %d, %q", result.Code, result.Body.Bytes())
		}
		head := request("HEAD", path, "", nil, "1")
		if head.Code != 200 || head.Header().Get("Content-Length") == "" || head.Body.Len() != 0 {
			t.Fatal("HEAD contract failed")
		}
		scope.SessionUUID = "other"
		if got := request("GET", path, "", nil, "1").Code; got != 404 {
			t.Fatalf("other session = %d", got)
		}
		scope.SessionUUID = "session"
		scope.WorkspaceUUID = "other"
		if got := request("GET", path, "", nil, "1").Code; got != 404 {
			t.Fatalf("other workspace = %d", got)
		}
		scope.WorkspaceUUID = "workspace"
	})
	t.Run("temporary object read failure remains retryable", func(t *testing.T) {
		objects.readFailure = errors.New("temporary stream failure")
		defer func() { objects.readFailure = nil }()
		if got := request("GET", path, "", nil, "1").Code; got != 503 {
			t.Fatalf("read failure status = %d", got)
		}
	})

	t.Run("corruption is rejected before response body", func(t *testing.T) {
		for key := range objects.data {
			objects.data[key] = []byte("bad")
		}
		result := request("GET", path, "", nil, "1")
		if result.Code != 400 {
			t.Fatalf("corrupt object = %d", result.Code)
		}
	})
}
