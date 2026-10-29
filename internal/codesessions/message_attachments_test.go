package codesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
)

type messageImageTransport func(*http.Request) (*http.Response, error)

func (f messageImageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type messageAttachmentFiles struct {
	records map[string]db.FileRecord
	data    []byte
	writes  int
}

func (f *messageAttachmentFiles) GetFile(context.Context, string, string) (db.FileRecord, error) {
	return db.FileRecord{}, db.ErrNotFound
}

func (f *messageAttachmentFiles) GetSessionAttachmentFileByPath(_ context.Context, workspace, session, path string) (db.FileRecord, error) {
	if workspace != "workspace" || session != "session-uuid" {
		return db.FileRecord{}, db.ErrNotFound
	}
	file, found := f.records[path]
	if !found {
		return file, db.ErrNotFound
	}
	return file, nil
}

func (f *messageAttachmentFiles) PutSessionAttachment(_ context.Context, target filestore.SessionAttachmentTarget, mediaType string, reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if target.WorkspaceUUID != "workspace" || target.SessionExternalID != "sesn_test" {
		return errors.New("incorrect attachment scope")
	}
	f.data = data
	f.writes++
	f.records[target.Path] = db.FileRecord{ExternalID: "file_url_image", MimeType: mediaType}
	return nil
}

func (f *messageAttachmentFiles) CopyFileToSessionAttachment(context.Context, filestore.SessionAttachmentTarget, db.FileRecord) error {
	return errors.New("unexpected Files copy")
}

func messageAttachmentFixture(transport http.RoundTripper, limit int64) (*Service, *messageAttachmentFiles, db.SessionEvent) {
	files := &messageAttachmentFiles{records: make(map[string]db.FileRecord)}
	service := &Service{attachments: files, attachmentFiles: files, attachmentHTTP: &http.Client{Transport: transport}, attachmentMaxBytes: limit}
	event := db.SessionEvent{CreatedAt: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), EventType: "user.message", ExternalID: "sevt_image", WorkspaceUUID: "workspace", SessionUUID: "session-uuid", SessionExternalID: "sesn_test",
		Payload: json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"Describe"},{"type":"image","source":{"type":"url","url":"https://images.example/a.png?signature=private"}}]}`)}
	return service, files, event
}

func TestBrainURLImagesRejectPermanentFailuresAndRetryTemporaryFailures(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte("a"), 2048)...)
	for _, scenario := range []struct {
		name, mediaType string
		status          int
		data            []byte
		size            int64
		retry           bool
	}{
		{name: "missing", status: 404},
		{name: "unauthorized origin", status: 401},
		{name: "temporary origin", status: 503, retry: true},
		{name: "rate limited origin", status: 429, retry: true},
		{name: "not an image", status: 200, data: []byte("<html>not an image</html>"), mediaType: "text/html"},
		{name: "mismatched MIME", status: 200, data: png[:20], mediaType: "image/jpeg"},
		{name: "declared oversized", status: 200, data: png[:20], mediaType: "image/png", size: 1025},
		{name: "chunked oversized", status: 200, data: png, mediaType: "image/png", size: -1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			service, files, event := messageAttachmentFixture(messageImageTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: scenario.status, Header: http.Header{"Content-Type": {scenario.mediaType}}, ContentLength: scenario.size, Body: io.NopCloser(bytes.NewReader(scenario.data))}, nil
			}), 1024)
			result, err := service.materializeMessageAttachments(context.Background(), event, true)
			if scenario.retry {
				if err == nil {
					t.Fatal("temporary download failure did not propagate")
				}
			} else if err != nil || !bytes.Contains(result.Payload, []byte("unavailable")) || bytes.Contains(result.Payload, []byte("file_id")) {
				t.Fatalf("permanent rejection = %s, %v", result.Payload, err)
			}
			if files.writes != 0 {
				t.Fatal("rejected bytes were saved")
			}
		})
	}
}

func TestMessageImageDownloadsDoNotLeakSourceURLsInErrors(t *testing.T) {
	service, _, event := messageAttachmentFixture(messageImageTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection failed")
	}), 1024)
	_, err := service.materializeMessageAttachments(context.Background(), event, true)
	if err == nil || strings.Contains(err.Error(), "signature") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("download error included source URL: %v", err)
	}
}

func TestMessageImageDownloadsRejectPrivateTargetsAndRedirects(t *testing.T) {
	var contacted atomic.Int32
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		contacted.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer private.Close()
	initial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL, http.StatusFound)
	}))
	defer initial.Close()
	client := newMessageImageHTTPClient()
	transport := client.Transport.(*http.Transport)
	defer transport.CloseIdleConnections()
	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "public.example:80" {
			return (&net.Dialer{}).DialContext(ctx, network, initial.Listener.Addr().String())
		}
		return dial(ctx, network, address)
	}
	service := &Service{attachmentHTTP: client, attachmentMaxBytes: 1024}
	for _, target := range []string{private.URL, "http://public.example/image.png", "file:///etc/passwd", "https://user:password@public.example/a.png"} {
		if _, _, err := service.downloadMessageImage(context.Background(), target); !errors.Is(err, errMessageImageRejected) {
			t.Fatalf("target %q error = %v", target, err)
		}
	}
	if contacted.Load() != 0 {
		t.Fatal("private image target was contacted")
	}
}

func TestBrainURLImageMaterializationPreservesPublicPayloadAndReusesPrivateFile(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n")
	requests := 0
	service, files, event := messageAttachmentFixture(messageImageTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" || request.Method != http.MethodGet {
			t.Fatal("image download forwarded credentials or used an incorrect method")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/octet-stream"}}, Body: io.NopCloser(bytes.NewReader(png))}, nil
	}), 1024)
	original := bytes.Clone(event.Payload)
	for range 2 {
		result, err := service.materializeMessageAttachments(context.Background(), event, true)
		if err != nil || !bytes.Contains(result.Payload, []byte(`"file_id":"file_url_image"`)) || bytes.Contains(result.Payload, []byte("https://")) {
			t.Fatalf("materialized worker image = %s, %v", result.Payload, err)
		}
	}
	if requests != 1 || files.writes != 1 || !bytes.Equal(files.data, png) || !bytes.Equal(event.Payload, original) {
		t.Fatalf("requests=%d writes=%d bytes=%x public=%s", requests, files.writes, files.data, event.Payload)
	}
	if _, found := files.records["/uploads/message_attachments/sevt_image/1.png"]; !found {
		t.Fatalf("private attachment paths = %v", files.records)
	}
}

func TestNonBrainImagesAndURLDocumentsRemainUnchanged(t *testing.T) {
	service, _, event := messageAttachmentFixture(messageImageTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("non-Brain image or URL document triggered a download")
		return nil, errors.New("unexpected request")
	}), 1024)
	result, err := service.materializeMessageAttachments(context.Background(), event, false)
	if err != nil || !bytes.Equal(result.Payload, event.Payload) {
		t.Fatalf("non-Brain URL changed: %s, %v", result.Payload, err)
	}
	event.Payload = bytes.ReplaceAll(event.Payload, []byte(`"type":"image"`), []byte(`"type":"document"`))
	result, err = service.materializeMessageAttachments(context.Background(), event, true)
	if err != nil || !bytes.Equal(result.Payload, event.Payload) {
		t.Fatalf("URL document changed: %s, %v", result.Payload, err)
	}
}

func TestWorkerTransportControlsURLImageMaterialization(t *testing.T) {
	for _, transport := range []string{"claim", "nats"} {
		t.Run(transport, func(t *testing.T) {
			requests := 0
			service, _, event := messageAttachmentFixture(messageImageTransport(func(*http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\n"))}, nil
			}), 1024)
			codeSession := db.CodeSession{ExternalID: "cse_test", Metadata: json.RawMessage(`{"config":{"worker_transport":"` + transport + `"}}`)}
			payload, err := service.publicWorkerPayload(context.Background(), codeSession, event)
			if err != nil {
				t.Fatal(err)
			}
			if transport == "claim" {
				if requests != 1 || !bytes.Contains(payload, []byte(`"file_id":"file_url_image"`)) || bytes.Contains(payload, []byte("https://")) {
					t.Fatalf("Brain worker received %s, downloads=%d", payload, requests)
				}
			} else if requests != 0 || !bytes.Contains(payload, []byte("https://images.example/")) {
				t.Fatalf("non-Brain URL changed: %s, downloads=%d", payload, requests)
			}
		})
	}
}

func TestURLImageDownloadHonorsUnlimitedStorageConfiguration(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n")
	service, _, event := messageAttachmentFixture(messageImageTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(png))}, nil
	}), 0)
	result, err := service.materializeMessageAttachments(context.Background(), event, true)
	if err != nil || !bytes.Contains(result.Payload, []byte(`"file_id":"file_url_image"`)) {
		t.Fatalf("unlimited storage configuration rejected image: %s, %v", result.Payload, err)
	}
}
