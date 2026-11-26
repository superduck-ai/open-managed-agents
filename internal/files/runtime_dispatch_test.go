package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type stubFileProfileStore struct {
	profile db.RuntimeProfile
}

func (s *stubFileProfileStore) GetRuntimeProfileByUUID(context.Context, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubFileProfileStore) GetWorkspaceRuntimeProfile(context.Context, string, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubFileProfileStore) UpdateRuntimeProfileHealth(context.Context, string, string, string, time.Time) error {
	return nil
}

type mockFileStream struct {
	*bytes.Reader
	contentType string
	filename    string
}

func (s *mockFileStream) Close() error         { return nil }
func (s *mockFileStream) ContentType() string  { return s.contentType }
func (s *mockFileStream) ContentLength() int64 { return int64(s.Len()) }
func (s *mockFileStream) Filename() string     { return s.filename }

type mockFileRuntime struct {
	file      agentruntime.File
	content   []byte
	deletedID string
	uploaded  agentruntime.UploadFileInput
}

func (m *mockFileRuntime) ProviderKind() agentruntime.ProviderKind {
	return agentruntime.ProviderQoderCloud
}

func (m *mockFileRuntime) Capabilities() agentruntime.CapabilitySet {
	return agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
}

func (m *mockFileRuntime) Close() error { return nil }

func (m *mockFileRuntime) UploadFile(_ context.Context, _ agentruntime.Scope, input agentruntime.UploadFileInput) (agentruntime.File, error) {
	m.uploaded = input
	return m.file, nil
}

func (m *mockFileRuntime) GetFile(context.Context, agentruntime.Scope, string) (agentruntime.File, error) {
	return m.file, nil
}

func (m *mockFileRuntime) ListFiles(context.Context, agentruntime.Scope, agentruntime.ListFilesQuery) (agentruntime.Page[agentruntime.File], error) {
	return agentruntime.Page[agentruntime.File]{Items: []agentruntime.File{m.file}}, nil
}

func (m *mockFileRuntime) DownloadFile(context.Context, agentruntime.Scope, string) (agentruntime.FileContentStream, error) {
	return &mockFileStream{
		Reader:      bytes.NewReader(m.content),
		contentType: m.file.MimeType,
		filename:    m.file.Filename,
	}, nil
}

func (m *mockFileRuntime) DeleteFile(_ context.Context, _ agentruntime.Scope, fileID string) error {
	m.deletedID = fileID
	return nil
}

func TestFilesHandlerExternalRuntimeV1ConsoleAndPlatformRoutes(t *testing.T) {
	const (
		orgUUID = "00000000-0000-4000-8000-000000000010"
		wsUUID  = "00000000-0000-4000-8000-000000000011"
	)
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	mockRT := &mockFileRuntime{
		file: agentruntime.File{
			ID:           "file_qdr_1",
			Filename:     "pixel.png",
			MimeType:     "image/png",
			SizeBytes:    int64(len(pngBytes)),
			Downloadable: true,
			CreatedAt:    now,
		},
		content: pngBytes,
	}
	registry := agentruntime.NewRegistry()
	if err := registry.Register(agentruntime.ProviderQoderCloud, func(context.Context, agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return mockRT, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	profile := db.RuntimeProfile{
		UUID:         "00000000-0000-4000-8000-000000000090",
		ProviderKind: agentruntime.ProviderQoderCloud.String(),
		Status:       "active",
	}
	resolver := agentruntime.NewResolver(&stubFileProfileStore{profile: profile}, registry, nil, nil)
	handler := NewHandler(config.Config{Storage: config.StorageConfig{MaxFileBytes: 10 << 20}}, nil, nil, nil).WithRuntimeResolver(resolver)

	principal := auth.Principal{
		OrganizationUUID:    orgUUID,
		WorkspaceUUID:       wsUUID,
		WorkspaceExternalID: "wrkspc_qoder",
		RuntimeProvider:     agentruntime.ProviderQoderCloud.String(),
		RuntimeProfileUUID:  profile.UUID,
		CredentialType:      auth.CredentialTypePlatformSession,
	}

	t.Run("v1 upload list get content delete", func(t *testing.T) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		part, err := writer.CreateFormFile("file", "pixel.png")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		_, _ = io.Copy(part, bytes.NewReader(pngBytes))
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), principal)))
		if rec.Code != http.StatusOK {
			t.Fatalf("upload status = %d: %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/?limit=10", nil)
		handler.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), principal)))
		if rec.Code != http.StatusOK {
			t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/file_qdr_1", nil)
		handler.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), principal)))
		if rec.Code != http.StatusOK {
			t.Fatalf("get status = %d: %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/file_qdr_1/content", nil)
		handler.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), principal)))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pngBytes) {
			t.Fatalf("download status = %d len = %d", rec.Code, rec.Body.Len())
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodDelete, "/file_qdr_1", nil)
		handler.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), principal)))
		if rec.Code != http.StatusOK || mockRT.deletedID != "file_qdr_1" {
			t.Fatalf("delete status = %d deletedID = %q: %s", rec.Code, mockRT.deletedID, rec.Body.String())
		}
	})

	t.Run("console routes proxy external metadata and image content", func(t *testing.T) {
		router := chi.NewRouter()
		router.Route("/api/console/organizations/{orgUuid}", func(r chi.Router) {
			handler.RegisterConsoleRoutes(r)
		})

		req := httptest.NewRequest(http.MethodGet, "/api/console/organizations/"+orgUUID+"/workspaces/wrkspc_qoder/files/file_qdr_1", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), principal)))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "pixel.png") {
			t.Fatalf("console metadata status = %d body = %s", rec.Code, rec.Body.String())
		}

		req = httptest.NewRequest(http.MethodGet, "/api/console/organizations/"+orgUUID+"/workspaces/wrkspc_qoder/files/file_qdr_1/content", nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), principal)))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pngBytes) {
			t.Fatalf("console content status = %d len = %d", rec.Code, rec.Body.Len())
		}
	})

	t.Run("platform routes upload_b64 and preview stream external files", func(t *testing.T) {
		router := chi.NewRouter()
		router.Route("/api/{orgUuid}", func(r chi.Router) {
			handler.RegisterPlatformRoutes(r)
		})

		encoded := base64.StdEncoding.EncodeToString(pngBytes)
		req := httptest.NewRequest(http.MethodPost, "/api/"+orgUUID+"/upload_b64", strings.NewReader(`{"file_name":"pixel.png","file_b64":"`+encoded+`"}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), principal)))
		if rec.Code != http.StatusOK {
			t.Fatalf("upload_b64 status = %d body = %s", rec.Code, rec.Body.String())
		}
		var uploadResp platformUploadB64Response
		if err := json.Unmarshal(rec.Body.Bytes(), &uploadResp); err != nil {
			t.Fatalf("unmarshal upload_b64: %v", err)
		}
		if uploadResp.FileUUID != "file_qdr_1" {
			t.Fatalf("file_uuid = %q, want file_qdr_1", uploadResp.FileUUID)
		}

		req = httptest.NewRequest(http.MethodGet, "/api/"+orgUUID+"/files/file_qdr_1/preview", nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), principal)))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pngBytes) {
			t.Fatalf("preview status = %d len = %d", rec.Code, rec.Body.Len())
		}
	})
}
