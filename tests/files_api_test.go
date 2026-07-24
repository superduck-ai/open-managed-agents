package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const defaultTestKey = config.DefaultAPIKey
const onePixelGIFBase64 = "R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw=="

type testApp struct {
	cfg         config.Config
	db          *db.DB
	store       storage.ObjectStore
	sessions    *platformsession.MemoryStore
	credentials *codesessions.SessionCredentials
	server      *httptest.Server
	baseURL     string
	client      *http.Client
}

type errorResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Error     struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type metadataResponse struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Filename     string `json:"filename"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	CreatedAt    string `json:"created_at"`
	Downloadable bool   `json:"downloadable"`
}

type pageResponse struct {
	Data    []metadataResponse `json:"data"`
	HasMore bool               `json:"has_more"`
	FirstID *string            `json:"first_id"`
	LastID  *string            `json:"last_id"`
}

type platformUploadResponse struct {
	FileKind       string               `json:"file_kind"`
	FileUUID       string               `json:"file_uuid"`
	FileName       string               `json:"file_name"`
	CreatedAt      string               `json:"created_at"`
	UserUUID       *string              `json:"user_uuid"`
	SizeBytes      *int64               `json:"size_bytes"`
	ThumbnailURL   string               `json:"thumbnail_url"`
	PreviewURL     string               `json:"preview_url"`
	ThumbnailAsset *platformUploadAsset `json:"thumbnail_asset"`
	PreviewAsset   *platformUploadAsset `json:"preview_asset"`
	UUID           string               `json:"uuid"`
}

type platformUploadAsset struct {
	URL          string `json:"url"`
	FileVariant  string `json:"file_variant"`
	PrimaryColor string `json:"primary_color"`
	ImageWidth   int    `json:"image_width"`
	ImageHeight  int    `json:"image_height"`
}

func TestV1AuthModes(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	platformSessionKey := "session-auth-modes-" + suffix
	app := newTestAppWithStore(t, &cfg, newFakeStore("auth-modes-bucket"))
	defer app.close()
	app.seedPlatformSession(t, platformSessionKey)

	t.Run("failure platform host requires session cookie", func(t *testing.T) {
		resp := app.doAuthMode(t, "platform.claude.com", "", "", "")
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("success api key works on any host", func(t *testing.T) {
		resp := app.doAuthMode(t, "platform.claude.com", defaultTestKey, "", "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("failure platform host clears invalid session cookie", func(t *testing.T) {
		resp := app.doAuthMode(t, "platform.claude.com", "", "", "session-invalid")
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
		if cookie := responseCookie(resp.Cookies(), "sessionKey"); cookie == nil || cookie.MaxAge >= 0 {
			t.Fatalf("invalid session cookies = %#v, want expired sessionKey", resp.Cookies())
		}
		if cookie := responseCookie(resp.Cookies(), "lastActiveOrg"); cookie == nil || cookie.MaxAge >= 0 {
			t.Fatalf("invalid session cookies = %#v, want expired lastActiveOrg", resp.Cookies())
		}
	})

	t.Run("success session cookie works on any host", func(t *testing.T) {
		resp := app.doAuthMode(t, "api.anthropic.com", "", "", platformSessionKey)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("success api host accepts x api key", func(t *testing.T) {
		resp := app.doAuthMode(t, "api.anthropic.com", defaultTestKey, "", "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("success api host accepts bearer token", func(t *testing.T) {
		resp := app.doAuthMode(t, "api.anthropic.com", "", defaultTestKey, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("success platform host accepts session cookie", func(t *testing.T) {
		resp := app.doAuthMode(t, "platform.claude.com", "", "", platformSessionKey)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("failure platform host rejects session missing from session store", func(t *testing.T) {
		if err := app.sessions.Delete(context.Background(), platformSessionKey); err != nil {
			t.Fatalf("delete platform session: %v", err)
		}
		resp := app.doAuthMode(t, "platform.claude.com", "", "", platformSessionKey)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
		if cookie := responseCookie(resp.Cookies(), "sessionKey"); cookie == nil || cookie.MaxAge >= 0 {
			t.Fatalf("missing store session cookies = %#v, want expired sessionKey", resp.Cookies())
		}
	})

}

func TestPlatformUploadB64FilesAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("platform-upload-b64-bucket"))
	defer app.close()

	defaultIDs := getDefaultDBIDs(t, app.db)
	workspacePath := "/api/" + defaultIDs.WorkspaceUUID
	organizationPath := "/api/" + defaultIDs.OrganizationUUID
	sessionKey := "session-platform-upload-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	cookies := []*http.Cookie{{Name: "sessionKey", Value: sessionKey}}
	app.seedPlatformSession(t, sessionKey)
	pngBytes := generatedPNG(t, 800, 600)
	pngBase64 := base64.StdEncoding.EncodeToString(pngBytes)
	validPayload := `{"file_name":"pixel.png","file_kind":"image","file_b64":"` + pngBase64 + `"}`

	t.Run("failure missing platform session", func(t *testing.T) {
		resp := app.platformAPIRequest(t, "platform.claude.com", http.MethodPost, organizationPath+"/upload_b64", strings.NewReader(validPayload), nil)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure unknown organization", func(t *testing.T) {
		resp := app.platformAPIRequest(t, "platform.claude.com", http.MethodPost, "/api/"+uuid.NewString()+"/upload_b64", strings.NewReader(validPayload), cookies)
		assertError(t, resp, http.StatusForbidden, "permission_error")
	})

	t.Run("failure workspace uuid is not an organization route", func(t *testing.T) {
		resp := app.platformAPIRequest(t, "platform.claude.com", http.MethodPost, workspacePath+"/upload_b64", strings.NewReader(validPayload), cookies)
		assertError(t, resp, http.StatusForbidden, "permission_error")
	})

	t.Run("failure invalid base64", func(t *testing.T) {
		resp := app.platformAPIRequest(t, "platform.claude.com", http.MethodPost, organizationPath+"/upload_b64", strings.NewReader(`{"file_name":"bad.png","file_b64":"not base64"}`), cookies)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("success upload accepts organization uuid and stores png thumbnail", func(t *testing.T) {
		resp := app.platformAPIRequest(t, "platform.claude.com", http.MethodPost, organizationPath+"/upload_b64", strings.NewReader(validPayload), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("upload_b64 status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var uploaded platformUploadResponse
		decodeJSON(t, resp.Body, &uploaded)
		if uploaded.FileKind != "image" || uploaded.FileName != "pixel.png" || uploaded.UUID != uploaded.FileUUID {
			t.Fatalf("upload response = %+v, want image pixel with matching uuid", uploaded)
		}
		if uploaded.UserUUID != nil || uploaded.SizeBytes != nil {
			t.Fatalf("upload response user/size = %v/%v, want nulls", uploaded.UserUUID, uploaded.SizeBytes)
		}
		if _, err := time.Parse(time.RFC3339Nano, uploaded.CreatedAt); err != nil {
			t.Fatalf("created_at = %q, want RFC3339Nano: %v", uploaded.CreatedAt, err)
		}
		if uploaded.ThumbnailURL != organizationPath+"/files/"+uploaded.FileUUID+"/thumbnail" {
			t.Fatalf("thumbnail_url = %q", uploaded.ThumbnailURL)
		}
		if uploaded.PreviewURL != organizationPath+"/files/"+uploaded.FileUUID+"/preview" {
			t.Fatalf("preview_url = %q", uploaded.PreviewURL)
		}
		if uploaded.ThumbnailAsset == nil || uploaded.PreviewAsset == nil {
			t.Fatalf("assets = %#v/%#v, want image assets", uploaded.ThumbnailAsset, uploaded.PreviewAsset)
		}
		if uploaded.ThumbnailAsset.ImageWidth != 400 || uploaded.ThumbnailAsset.ImageHeight != 300 || uploaded.ThumbnailAsset.FileVariant != "thumbnail" {
			t.Fatalf("thumbnail asset = %+v, want 400x300 thumbnail", uploaded.ThumbnailAsset)
		}
		if uploaded.PreviewAsset.ImageWidth != 800 || uploaded.PreviewAsset.ImageHeight != 600 || uploaded.PreviewAsset.FileVariant != "preview" {
			t.Fatalf("preview asset = %+v, want 800x600 preview", uploaded.PreviewAsset)
		}

		record, err := app.db.GetFileByUUID(context.Background(), defaultIDs.WorkspaceID, uploaded.FileUUID)
		if err != nil {
			t.Fatalf("get uploaded file by uuid: %v", err)
		}
		if record.Filename != "pixel.png" || record.MimeType != "image/png" || record.SizeBytes != int64(len(pngBytes)) {
			t.Fatalf("stored record = %+v, want png metadata", record)
		}
		thumbnailKey := strings.TrimSuffix(record.S3Key, "/pixel.png") + "/variants/thumbnail.png"
		if _, ok := app.store.(*fakeStore).objects[thumbnailKey]; !ok {
			t.Fatalf("missing thumbnail object %s", thumbnailKey)
		}

		thumbnailResp := app.platformAPIRequest(t, "oma.duck.ai", http.MethodGet, uploaded.ThumbnailURL, nil, cookies)
		defer thumbnailResp.Body.Close()
		if thumbnailResp.StatusCode != http.StatusOK {
			t.Fatalf("oma thumbnail status = %d, want 200: %s", thumbnailResp.StatusCode, readAll(t, thumbnailResp.Body))
		}
		if got := thumbnailResp.Header.Get("Cache-Control"); got != "private, max-age=604800" {
			t.Fatalf("thumbnail cache-control = %q", got)
		}
		if got := thumbnailResp.Header.Get("Content-Type"); got != "image/png" {
			t.Fatalf("thumbnail content-type = %q", got)
		}
		thumbnailBytes := readAll(t, thumbnailResp.Body)
		if bytes.Equal(thumbnailBytes, pngBytes) {
			t.Fatalf("thumbnail bytes matched original, want resized derived object")
		}
		width, height := imageConfig(t, thumbnailBytes)
		if width != 400 || height != 300 {
			t.Fatalf("thumbnail dimensions = %dx%d, want 400x300", width, height)
		}

		previewResp := app.platformAPIRequest(t, "platform.claude.com", http.MethodGet, uploaded.PreviewURL, nil, cookies)
		defer previewResp.Body.Close()
		if previewResp.StatusCode != http.StatusOK {
			t.Fatalf("platform preview status = %d, want 200: %s", previewResp.StatusCode, readAll(t, previewResp.Body))
		}
		if got := previewResp.Header.Get("Content-Type"); got != "image/png" {
			t.Fatalf("preview content-type = %q", got)
		}
		if got := readAll(t, previewResp.Body); !bytes.Equal(got, pngBytes) {
			t.Fatalf("preview bytes = %d bytes, want %d", len(got), len(pngBytes))
		}

		deleteFile(t, app, record.ExternalID)
		if _, ok := app.store.(*fakeStore).objects[thumbnailKey]; ok {
			t.Fatalf("thumbnail object %s still exists after delete", thumbnailKey)
		}
	})

	t.Run("success gif thumbnail falls back to original", func(t *testing.T) {
		gifBytes, err := base64.StdEncoding.DecodeString(onePixelGIFBase64)
		if err != nil {
			t.Fatalf("decode gif fixture: %v", err)
		}
		payload := `{"file_name":"pixel.gif","file_kind":"image","file_b64":"` + onePixelGIFBase64 + `"}`
		resp := app.platformAPIRequest(t, "platform.claude.com", http.MethodPost, organizationPath+"/upload_b64", strings.NewReader(payload), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("gif upload_b64 status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var uploaded platformUploadResponse
		decodeJSON(t, resp.Body, &uploaded)

		record, err := app.db.GetFileByUUID(context.Background(), defaultIDs.WorkspaceID, uploaded.FileUUID)
		if err != nil {
			t.Fatalf("get gif file by uuid: %v", err)
		}
		thumbnailKey := strings.TrimSuffix(record.S3Key, "/pixel.gif") + "/variants/thumbnail.gif"
		if _, ok := app.store.(*fakeStore).objects[thumbnailKey]; ok {
			t.Fatalf("unexpected gif thumbnail object %s", thumbnailKey)
		}

		thumbnailResp := app.platformAPIRequest(t, "platform.claude.com", http.MethodGet, uploaded.ThumbnailURL, nil, cookies)
		defer thumbnailResp.Body.Close()
		if thumbnailResp.StatusCode != http.StatusOK {
			t.Fatalf("gif thumbnail status = %d, want 200: %s", thumbnailResp.StatusCode, readAll(t, thumbnailResp.Body))
		}
		if got := thumbnailResp.Header.Get("Content-Type"); got != "image/gif" {
			t.Fatalf("gif thumbnail content-type = %q", got)
		}
		if got := readAll(t, thumbnailResp.Body); !bytes.Equal(got, gifBytes) {
			t.Fatalf("gif thumbnail bytes = %d bytes, want original %d", len(got), len(gifBytes))
		}
	})
}

func TestFilesAPI(t *testing.T) {
	app := newTestApp(t, nil)
	defer app.close()

	t.Run("routing group auth applies only to v1", func(t *testing.T) {
		healthResp := app.do(t, http.MethodGet, "/healthz", nil, "", false, "")
		defer healthResp.Body.Close()
		if healthResp.StatusCode != http.StatusOK {
			t.Fatalf("healthz status = %d, want 200: %s", healthResp.StatusCode, readAll(t, healthResp.Body))
		}

		resp := app.do(t, http.MethodGet, "/v1/unknown", nil, "", false, "")
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")

		resp = app.do(t, http.MethodGet, "/v1/unknown", nil, defaultTestKey, false, "")
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("failure missing api key", func(t *testing.T) {
		resp := app.do(t, http.MethodGet, "/v1/files?beta=true", nil, "", true, "")
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure invalid api key", func(t *testing.T) {
		resp := app.do(t, http.MethodGet, "/v1/files?beta=true", nil, "sk-ant-invalid", true, "")
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure missing beta header", func(t *testing.T) {
		resp := app.do(t, http.MethodGet, "/v1/files?beta=true", nil, defaultTestKey, false, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure non multipart upload", func(t *testing.T) {
		resp := app.do(t, http.MethodPost, "/v1/files?beta=true", bytes.NewBufferString(`{"file":"nope"}`), defaultTestKey, true, "application/json")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure missing file field", func(t *testing.T) {
		body, contentType := multipartBody(t, "", "text/plain", nil, true)
		resp := app.do(t, http.MethodPost, "/v1/files?beta=true", body, defaultTestKey, true, contentType)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid filename", func(t *testing.T) {
		body, contentType := multipartBody(t, "bad:name.txt", "text/plain", []byte("bad"), false)
		resp := app.do(t, http.MethodPost, "/v1/files?beta=true", body, defaultTestKey, true, contentType)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure file too large", func(t *testing.T) {
		smallLimit := app.cfg
		smallLimit.Storage.MaxFileBytes = 4
		limited := newTestApp(t, &smallLimit)
		defer limited.close()

		body, contentType := multipartBody(t, "large.txt", "text/plain", []byte("too large"), false)
		resp := limited.do(t, http.MethodPost, "/v1/files?beta=true", body, defaultTestKey, true, contentType)
		assertError(t, resp, http.StatusRequestEntityTooLarge, "invalid_request_error")
	})

	t.Run("failure request body exceeds max bytes reader", func(t *testing.T) {
		smallLimit := app.cfg
		smallLimit.Storage.MaxFileBytes = 1
		limited := newTestApp(t, &smallLimit)
		defer limited.close()

		content := bytes.Repeat([]byte("x"), int(smallLimit.Storage.MaxFileBytes+1024*1024+1))
		body, contentType := multipartBody(t, "huge.txt", "text/plain", content, false)
		resp := limited.do(t, http.MethodPost, "/v1/files?beta=true", body, defaultTestKey, true, contentType)
		assertError(t, resp, http.StatusRequestEntityTooLarge, "invalid_request_error")
	})

	t.Run("failure missing file id", func(t *testing.T) {
		resp := app.do(t, http.MethodGet, "/v1/files/file_missing_test?beta=true", nil, defaultTestKey, true, "")
		assertError(t, resp, http.StatusNotFound, "not_found_error")

		resp = app.do(t, http.MethodDelete, "/v1/files/file_missing_test?beta=true", nil, defaultTestKey, true, "")
		assertError(t, resp, http.StatusNotFound, "not_found_error")

		resp = app.do(t, http.MethodGet, "/v1/files/file_missing_test/content?beta=true", nil, defaultTestKey, true, "")
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("failure cross workspace access", func(t *testing.T) {
		otherKey := "sk-ant-local-other"
		seedWorkspaceKey(t, app.db, "org_other_test", "workspace_other_test", "api_key_other_test", otherKey)

		uploaded := uploadFile(t, app, "cross-workspace.txt", "text/plain", []byte("private"))
		defer deleteFile(t, app, uploaded.ID)

		resp := app.do(t, http.MethodGet, "/v1/files/"+uploaded.ID+"?beta=true", nil, otherKey, true, "")
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("failure uploaded file is not downloadable", func(t *testing.T) {
		uploaded := uploadFile(t, app, "not-downloadable.txt", "text/plain", []byte("cannot download"))
		defer deleteFile(t, app, uploaded.ID)

		resp := app.do(t, http.MethodGet, "/v1/files/"+uploaded.ID+"/content?beta=true", nil, defaultTestKey, true, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure download logs truncated object stream", func(t *testing.T) {
		store := newFakeStore("fake-bucket")
		fakeApp := newTestAppWithStore(t, nil, store)
		defer fakeApp.close()

		fileID, objectKey := createDownloadableFile(t, fakeApp, "truncated.txt", "text/plain", []byte("complete"))
		defer softDeleteFile(t, fakeApp.db, fileID)

		store.getOverride = storage.Object{
			Body:        &errorReadCloser{data: []byte("abc"), err: errors.New("stream reset")},
			Size:        10,
			ContentType: "text/plain",
		}
		var logs bytes.Buffer
		originalLogWriter := log.Writer()
		log.SetOutput(&logs)
		defer log.SetOutput(originalLogWriter)

		resp := fakeApp.do(t, http.MethodGet, "/v1/files/"+fileID+"/content?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			t.Fatalf("download body read error = nil, want truncated body error")
		}
		if string(body) != "abc" {
			t.Fatalf("download body = %q, want abc", body)
		}
		logOutput := logs.String()
		if !strings.Contains(logOutput, "download object stream failed") || !strings.Contains(logOutput, objectKey) {
			t.Fatalf("download stream error was not logged with key %s: %s", objectKey, logOutput)
		}
	})

	t.Run("failure delete object queues cleanup job", func(t *testing.T) {
		store := newFakeStore("fake-bucket")
		store.deleteErr = errors.New("object storage unavailable")
		fakeApp := newTestAppWithStore(t, nil, store)
		defer fakeApp.close()

		uploaded := uploadFile(t, fakeApp, "delete-cleanup.txt", "text/plain", []byte("cleanup later"))
		defer fakeApp.db.Pool.Exec(context.Background(), `delete from jobs where payload->>'file_id' = $1`, uploaded.ID)
		resp := fakeApp.do(t, http.MethodDelete, "/v1/files/"+uploaded.ID+"?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("delete status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}

		var jobCount int
		if err := fakeApp.db.Pool.QueryRow(context.Background(), `
			select count(*)
			from jobs
			where type = 'object_cleanup'
				and status = 'pending'
				and payload->>'file_id' = $1
		`, uploaded.ID).Scan(&jobCount); err != nil {
			t.Fatalf("count cleanup jobs: %v", err)
		}
		if jobCount != 1 {
			t.Fatalf("cleanup job count = %d, want 1", jobCount)
		}
	})

	t.Run("failure upload metadata rejection queues cleanup job when object delete fails", func(t *testing.T) {
		store := newFakeStore("fake-bucket")
		store.deleteErr = errors.New("object storage unavailable")
		limitedConfig := app.cfg
		limitedConfig.Storage.MaxFileBytes = 1024
		limitedConfig.Storage.WorkspaceLimitBytes = 1
		fakeApp := newTestAppWithStore(t, &limitedConfig, store)
		defer fakeApp.close()

		body, contentType := multipartBody(t, "quota-cleanup.txt", "text/plain", []byte("over quota"), false)
		resp := fakeApp.do(t, http.MethodPost, "/v1/files?beta=true", body, defaultTestKey, true, contentType)
		assertError(t, resp, http.StatusForbidden, "permission_error")

		if len(store.objects) != 1 {
			t.Fatalf("stored object count = %d, want 1", len(store.objects))
		}
		var objectKey string
		for key := range store.objects {
			objectKey = key
		}
		defer fakeApp.db.Pool.Exec(context.Background(), `delete from jobs where payload->>'key' = $1`, objectKey)

		var fileRows int
		if err := fakeApp.db.Pool.QueryRow(context.Background(), `
			select count(*)
			from files
			where s3_key = $1
		`, objectKey).Scan(&fileRows); err != nil {
			t.Fatalf("count file rows: %v", err)
		}
		if fileRows != 0 {
			t.Fatalf("file rows for orphan object = %d, want 0", fileRows)
		}

		var jobCount int
		if err := fakeApp.db.Pool.QueryRow(context.Background(), `
			select count(*)
			from jobs
			where type = 'object_cleanup'
				and status = 'pending'
				and payload->>'key' = $1
				and payload->>'file_id' like 'file_%'
		`, objectKey).Scan(&jobCount); err != nil {
			t.Fatalf("count cleanup jobs: %v", err)
		}
		if jobCount != 1 {
			t.Fatalf("cleanup job count = %d, want 1", jobCount)
		}
	})

	t.Run("success docs api files paths without beta query", func(t *testing.T) {
		body, contentType := multipartBody(t, "docs-path.txt", "text/plain", []byte("docs path upload"), false)
		resp := app.do(t, http.MethodPost, "/v1/files", body, defaultTestKey, true, contentType)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("upload docs path status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var uploaded metadataResponse
		decodeJSON(t, resp.Body, &uploaded)
		defer deleteFile(t, app, uploaded.ID)

		resp = app.do(t, http.MethodGet, "/v1/files?limit=20", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list docs path status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var page pageResponse
		decodeJSON(t, resp.Body, &page)
		if !containsFile(page.Data, uploaded.ID) {
			t.Fatalf("list docs path did not include uploaded file %s: %+v", uploaded.ID, page.Data)
		}

		resp = app.do(t, http.MethodGet, "/v1/files/"+uploaded.ID, nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("retrieve docs path status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var retrieved metadataResponse
		decodeJSON(t, resp.Body, &retrieved)
		if retrieved.ID != uploaded.ID {
			t.Fatalf("retrieve docs path id = %s, want %s", retrieved.ID, uploaded.ID)
		}

		content := []byte("docs download")
		downloadableID, objectKey := createDownloadableFile(t, app, "docs-download.txt", "text/plain", content)
		defer func() {
			softDeleteFile(t, app.db, downloadableID)
			_ = app.store.Delete(context.Background(), objectKey, storage.DeleteOptions{})
		}()
		resp = app.do(t, http.MethodGet, "/v1/files/"+downloadableID+"/content", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download docs path status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		if got := readAll(t, resp.Body); string(got) != string(content) {
			t.Fatalf("download docs path content = %q, want %q", got, content)
		}

		resp = app.do(t, http.MethodDelete, "/v1/files/"+uploaded.ID, nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("delete docs path status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var deleted map[string]string
		decodeJSON(t, resp.Body, &deleted)
		if deleted["id"] != uploaded.ID || deleted["type"] != "file_deleted" {
			t.Fatalf("unexpected docs path delete response: %+v", deleted)
		}
	})

	t.Run("success upload list retrieve delete", func(t *testing.T) {
		uploaded := uploadFile(t, app, "document.txt", "text/plain", []byte("hello files"))
		if uploaded.Type != "file" || uploaded.Filename != "document.txt" || uploaded.SizeBytes != int64(len("hello files")) {
			t.Fatalf("unexpected upload metadata: %+v", uploaded)
		}

		resp := app.do(t, http.MethodGet, "/v1/files?beta=true&limit=20", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var page pageResponse
		decodeJSON(t, resp.Body, &page)
		if !containsFile(page.Data, uploaded.ID) {
			t.Fatalf("list did not include uploaded file %s: %+v", uploaded.ID, page.Data)
		}

		resp = app.do(t, http.MethodGet, "/v1/files/"+uploaded.ID+"?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("retrieve status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var retrieved metadataResponse
		decodeJSON(t, resp.Body, &retrieved)
		if retrieved.ID != uploaded.ID {
			t.Fatalf("retrieve id = %s, want %s", retrieved.ID, uploaded.ID)
		}

		resp = app.do(t, http.MethodDelete, "/v1/files/"+uploaded.ID+"?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("delete status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var deleted map[string]string
		decodeJSON(t, resp.Body, &deleted)
		if deleted["id"] != uploaded.ID || deleted["type"] != "file_deleted" {
			t.Fatalf("unexpected delete response: %+v", deleted)
		}
	})

	t.Run("success pagination and scope filter", func(t *testing.T) {
		first := uploadFile(t, app, "page-1.txt", "text/plain", []byte("one"))
		defer deleteFile(t, app, first.ID)
		time.Sleep(5 * time.Millisecond)
		second := uploadFile(t, app, "page-2.txt", "text/plain", []byte("two"))
		defer deleteFile(t, app, second.ID)
		time.Sleep(5 * time.Millisecond)
		third := uploadFile(t, app, "page-3.txt", "text/plain", []byte("three"))
		defer deleteFile(t, app, third.ID)

		page1 := listFiles(t, app, "limit=2")
		if len(page1.Data) != 2 || !page1.HasMore || page1.LastID == nil {
			t.Fatalf("unexpected first page: %+v", page1)
		}
		page2 := listFiles(t, app, "limit=2&after_id="+*page1.LastID)
		if len(page2.Data) == 0 {
			t.Fatalf("expected second page to contain at least one file")
		}
		pageBefore := listFiles(t, app, "limit=2&before_id="+*page1.LastID)
		if len(pageBefore.Data) != 1 {
			t.Fatalf("before_id page length = %d, want 1", len(pageBefore.Data))
		}

		scopeID := "session_scope_test"
		scopedID := createMetadataOnlyFile(t, app, scopeID)
		defer softDeleteFile(t, app.db, scopedID)
		scopedPage := listFiles(t, app, "scope_id="+scopeID)
		if len(scopedPage.Data) != 1 || scopedPage.Data[0].ID != scopedID {
			t.Fatalf("unexpected scoped page: %+v", scopedPage)
		}

		_ = first
		_ = second
		_ = third
	})

	t.Run("success downloadable file returns bytes", func(t *testing.T) {
		content := []byte("generated content")
		fileID, objectKey := createDownloadableFile(t, app, "generated.txt", "text/plain", content)
		defer func() {
			softDeleteFile(t, app.db, fileID)
			_ = app.store.Delete(context.Background(), objectKey, storage.DeleteOptions{})
		}()

		resp := app.do(t, http.MethodGet, "/v1/files/"+fileID+"/content?beta=true", nil, defaultTestKey, true, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		got := readAll(t, resp.Body)
		if string(got) != string(content) {
			t.Fatalf("download content = %q, want %q", got, content)
		}
	})
}

func TestDatabaseMigrationDropsForeignKeys(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	database, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("initial migrate database: %v", err)
	}
	if _, err := database.Pool.Exec(ctx, `
		drop table if exists fk_guard_child_test;
		drop table if exists fk_guard_parent_test;
		create table fk_guard_parent_test (
			id bigint primary key
		);
		create table fk_guard_child_test (
			id bigint primary key,
			parent_id bigint,
			constraint fk_guard_child_parent_id_fkey
				foreign key (parent_id) references fk_guard_parent_test(id)
		);
	`); err != nil {
		t.Fatalf("create foreign key guard tables: %v", err)
	}
	defer database.Pool.Exec(ctx, `
		drop table if exists fk_guard_child_test;
		drop table if exists fk_guard_parent_test;
	`)

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate database with foreign key guard: %v", err)
	}

	var foreignKeyCount int
	if err := database.Pool.QueryRow(ctx, `
		select count(*)
		from pg_constraint con
		join pg_namespace ns on ns.oid = con.connamespace
		where con.contype = 'f'
			and ns.oid = current_schema()::regnamespace
	`).Scan(&foreignKeyCount); err != nil {
		t.Fatalf("count foreign keys: %v", err)
	}
	if foreignKeyCount != 0 {
		t.Fatalf("foreign key count = %d, want 0", foreignKeyCount)
	}
}

func TestDatabaseMigrationRecordsGooseBaseline(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	database, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("first migrate database: %v", err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("second migrate database: %v", err)
	}

	var appliedCount int
	if err := database.Pool.QueryRow(ctx, `
		select count(*)
		from goose_db_version
		where version_id = 1
			and is_applied
	`).Scan(&appliedCount); err != nil {
		t.Fatalf("count goose baseline version: %v", err)
	}
	if appliedCount != 1 {
		t.Fatalf("goose baseline applied count = %d, want 1", appliedCount)
	}
}

func TestObjectCleanupJobAttemptsIncrementOnFailure(t *testing.T) {
	app := newTestApp(t, nil)
	defer app.close()

	ctx := context.Background()
	defaultIDs := getDefaultDBIDs(t, app.db)
	objectKey := "attempts-test/" + uuid.NewString()
	if err := app.db.EnqueueObjectCleanupJob(ctx, defaultIDs.WorkspaceID, app.store.Name(), objectKey, "file_attempts_test"); err != nil {
		t.Fatalf("enqueue cleanup job: %v", err)
	}
	defer app.db.Pool.Exec(ctx, `delete from jobs where payload->>'key' = $1`, objectKey)
	if _, err := app.db.Pool.Exec(ctx, `
		update jobs
		set run_after = '2000-01-01T00:00:00Z', created_at = '2000-01-01T00:00:00Z'
		where payload->>'key' = $1
	`, objectKey); err != nil {
		t.Fatalf("prioritize cleanup job: %v", err)
	}

	jobs, err := app.db.LeaseObjectCleanupJobs(ctx, "attempts-test-worker", 1)
	if err != nil {
		t.Fatalf("lease cleanup jobs: %v", err)
	}
	var job db.ObjectCleanupJob
	for _, candidate := range jobs {
		if candidate.Key == objectKey {
			job = candidate
			break
		}
	}
	if job.ID == 0 {
		t.Fatalf("leased jobs did not include %s: %+v", objectKey, jobs)
	}
	if job.Attempts != 0 {
		t.Fatalf("leased attempts = %d, want 0 before first failure", job.Attempts)
	}

	if err := app.db.FailObjectCleanupJob(ctx, job.ID, job.Attempts, "delete failed", 0, 10); err != nil {
		t.Fatalf("fail cleanup job: %v", err)
	}
	var status string
	var attempts int
	if err := app.db.Pool.QueryRow(ctx, `
		select status, attempts
		from jobs
		where id = $1
	`, job.ID).Scan(&status, &attempts); err != nil {
		t.Fatalf("load cleanup job: %v", err)
	}
	if status != "retry" || attempts != 1 {
		t.Fatalf("cleanup job status=%s attempts=%d, want retry/1", status, attempts)
	}
}

func TestObjectCleanupWorkerContinuesAfterJobFailure(t *testing.T) {
	failedBucket := newFakeStore("failed-bucket")
	failedBucket.deleteErrByKey = map[string]error{"cleanup-worker/fail": errors.New("delete failed")}
	successfulBucket := newFakeStore("successful-bucket")
	successfulBucket.objects["cleanup-worker/succeed"] = fakeObject{data: []byte("cleanup")}
	app := newTestAppWithStore(t, nil, failedBucket)
	defer app.close()

	ctx := context.Background()
	defaultIDs := getDefaultDBIDs(t, app.db)
	jobs := []struct {
		bucket storage.ObjectStore
		key    string
	}{
		{bucket: failedBucket, key: "cleanup-worker/fail"},
		{bucket: successfulBucket, key: "cleanup-worker/succeed"},
	}
	for _, job := range jobs {
		if err := app.db.EnqueueObjectCleanupJob(ctx, defaultIDs.WorkspaceID, job.bucket.Name(), job.key, "file_"+strings.ReplaceAll(job.key, "/", "_")); err != nil {
			t.Fatalf("enqueue cleanup job %s: %v", job.key, err)
		}
		defer app.db.Pool.Exec(ctx, `delete from jobs where payload->>'key' = $1`, job.key)
	}
	if _, err := app.db.Pool.Exec(ctx, `
		update jobs
		set run_after = '2000-01-01T00:00:00Z', created_at = '2000-01-01T00:00:00Z'
		where payload->>'key' in ($1, $2)
	`, jobs[0].key, jobs[1].key); err != nil {
		t.Fatalf("prioritize cleanup jobs: %v", err)
	}

	if err := cleanup.RunObjectCleanupOnce(ctx, app.db, newFakeStorageClient(failedBucket, successfulBucket), "cleanup-worker-test"); err != nil {
		t.Fatalf("run cleanup once: %v", err)
	}

	statusByKey := make(map[string]string)
	rows, err := app.db.Pool.Query(ctx, `
		select payload->>'key', status
		from jobs
		where payload->>'key' in ($1, $2)
	`, jobs[0].key, jobs[1].key)
	if err != nil {
		t.Fatalf("query cleanup jobs: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, status string
		if err := rows.Scan(&key, &status); err != nil {
			t.Fatalf("scan cleanup job: %v", err)
		}
		statusByKey[key] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("cleanup rows: %v", err)
	}
	if statusByKey["cleanup-worker/fail"] != "retry" {
		t.Fatalf("failed job status = %s, want retry", statusByKey["cleanup-worker/fail"])
	}
	if statusByKey["cleanup-worker/succeed"] != "completed" {
		t.Fatalf("succeeded job status = %s, want completed", statusByKey["cleanup-worker/succeed"])
	}
	if _, exists := successfulBucket.objects["cleanup-worker/succeed"]; exists {
		t.Fatal("successful object still exists in its recorded bucket")
	}
}

func newTestApp(t *testing.T, override *config.Config) *testApp {
	t.Helper()
	store, cfg := newS3ObjectStore(t, override)
	return newTestAppWithStore(t, &cfg, store)
}

func newS3ObjectStore(t *testing.T, override *config.Config) (storage.ObjectStore, config.Config) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.OTLPFileLogEnabled = false
	if override != nil {
		cfg = *override
	}
	client, err := storage.New(cfg.Storage)
	if err != nil {
		t.Fatalf("create S3 client: %v", err)
	}
	store, err := client.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		t.Fatalf("bind S3 bucket: %v", err)
	}
	return store, cfg
}

func newTestAppWithStore(t *testing.T, override *config.Config, store storage.ObjectStore) *testApp {
	t.Helper()
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.OTLPFileLogEnabled = false
	if override != nil {
		cfg = *override
	}
	database, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Migrate(ctx); err != nil {
		database.Close()
		t.Fatalf("migrate database: %v", err)
	}
	if err := database.Seed(ctx, cfg.Bootstrap.SeedAPIKeys); err != nil {
		database.Close()
		t.Fatalf("seed database: %v", err)
	}
	platformSessions := platformsession.NewMemoryStore()
	credentials, err := codesessions.NewSessionCredentials(cfg)
	if err != nil {
		database.Close()
		t.Fatalf("create code session credentials: %v", err)
	}
	filestoreCredentials, err := filestore.NewTokenCredentials(cfg)
	if err != nil {
		database.Close()
		t.Fatalf("create filestore credentials: %v", err)
	}
	if err := store.Ensure(ctx); err != nil {
		database.Close()
		t.Fatalf("ensure object store bucket: %v", err)
	}
	server := httptest.NewServer(api.NewServer(api.ServerDeps{
		Config:                 cfg,
		DB:                     database,
		ObjectStore:            store,
		ModelCatalog:           testModelCatalog{},
		PlatformStore:          platformSessions,
		CodeSessionCredentials: credentials,
		FilestoreCredentials:   filestoreCredentials,
	}))
	return &testApp{cfg: cfg, db: database, store: store, sessions: platformSessions, credentials: credentials, server: server, baseURL: server.URL, client: server.Client()}
}

type testModelCatalog struct{}

func (testModelCatalog) Snapshot(context.Context) (modelcatalog.Snapshot, error) {
	now := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	return modelcatalog.Snapshot{
		Models: []modelcatalog.Model{{
			ID:          "test/model",
			DisplayName: "Test model",
		}},
		DefaultModelID:   "test/model",
		DefaultAvailable: true,
		LastSuccessAt:    &now,
	}, nil
}

func (testModelCatalog) ValidateModel(_ context.Context, modelID string) error {
	if strings.TrimSpace(modelID) == "" {
		return modelcatalog.ErrUnknownModel
	}
	return nil
}

func (a *testApp) close() {
	a.server.Close()
	a.db.Close()
}

func (a *testApp) seedPlatformSession(t *testing.T, sessionKey string) {
	t.Helper()
	session, err := a.db.ResolvePlatformSessionIdentity(context.Background(), platformsession.CreateInput{
		SessionKey: sessionKey,
		UserUUID:   a.cfg.Bootstrap.UserExternalID,
		OrgUUID:    a.cfg.Bootstrap.OrganizationExternalID,
	})
	if err != nil {
		t.Fatalf("resolve platform session identity: %v", err)
	}
	if err := a.sessions.Save(context.Background(), sessionKey, session); err != nil {
		t.Fatalf("seed platform session store: %v", err)
	}
}

func (a *testApp) do(t *testing.T, method, path string, body io.Reader, key string, beta bool, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, a.baseURL+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	if beta {
		req.Header.Set("anthropic-beta", "files-api-2025-04-14")
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func (a *testApp) doAuthMode(t *testing.T, host, key, bearerToken, sessionKey string) *http.Response {
	t.Helper()
	return a.doAuthModePath(t, host, "/v1/files?beta=true", key, bearerToken, sessionKey)
}

func (a *testApp) doAuthModePath(t *testing.T, host, path, key, bearerToken, sessionKey string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, a.baseURL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = host
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	if sessionKey != "" {
		req.AddCookie(&http.Cookie{Name: "sessionKey", Value: sessionKey})
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if strings.HasPrefix(path, "/v1/files") {
		req.Header.Set("anthropic-beta", "files-api-2025-04-14")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func (a *testApp) platformAPIRequest(t *testing.T, host, method, path string, body io.Reader, cookies []*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, a.baseURL+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = host
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func multipartBody(t *testing.T, filename, contentType string, content []byte, omitFile bool) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if omitFile {
		if err := writer.WriteField("name", "value"); err != nil {
			t.Fatalf("write field: %v", err)
		}
	} else {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
		header.Set("Content-Type", contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("create file part: %v", err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body, writer.FormDataContentType()
}

func assertError(t *testing.T, resp *http.Response, status int, typ string) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, status, readAll(t, resp.Body))
	}
	var body errorResponse
	decodeJSON(t, resp.Body, &body)
	if body.Type != "error" || body.Error.Type != typ {
		t.Fatalf("error = %+v, want type %s", body, typ)
	}
}

func uploadFile(t *testing.T, app *testApp, filename, contentType string, content []byte) metadataResponse {
	t.Helper()
	body, requestContentType := multipartBody(t, filename, contentType, content, false)
	resp := app.do(t, http.MethodPost, "/v1/files?beta=true", body, defaultTestKey, true, requestContentType)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var metadata metadataResponse
	decodeJSON(t, resp.Body, &metadata)
	return metadata
}

func deleteFile(t *testing.T, app *testApp, fileID string) {
	t.Helper()
	resp := app.do(t, http.MethodDelete, "/v1/files/"+fileID+"?beta=true", nil, defaultTestKey, true, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete cleanup status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func listFiles(t *testing.T, app *testApp, query string) pageResponse {
	t.Helper()
	resp := app.do(t, http.MethodGet, "/v1/files?beta=true&"+query, nil, defaultTestKey, true, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page pageResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func containsFile(files []metadataResponse, id string) bool {
	for _, file := range files {
		if file.ID == id {
			return true
		}
	}
	return false
}

func seedWorkspaceKey(t *testing.T, database *db.DB, orgID, workspaceID, keyID, apiKey string) {
	t.Helper()
	ctx := context.Background()
	var organizationRowID int64
	if err := database.Pool.QueryRow(ctx, `
		insert into organizations (external_id, name)
		values ($1, $1)
		on conflict (external_id) do update set name = excluded.name
		returning id
	`, orgID).Scan(&organizationRowID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	var workspaceRowID int64
	if err := database.Pool.QueryRow(ctx, `
		insert into workspaces (external_id, organization_id, name)
		values ($1, $2, $1)
		on conflict (external_id) do update set
			organization_id = excluded.organization_id,
			name = excluded.name
		returning id
	`, workspaceID, organizationRowID).Scan(&workspaceRowID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := database.Pool.Exec(ctx, `
		insert into api_keys (external_id, workspace_id, key_hash, status)
		values ($1, $2, $3, 'active')
		on conflict (external_id) do update set
			workspace_id = excluded.workspace_id,
			key_hash = excluded.key_hash,
			status = 'active'
	`, keyID, workspaceRowID, auth.HashAPIKey(apiKey)); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
}

func createMetadataOnlyFile(t *testing.T, app *testApp, scopeID string) string {
	t.Helper()
	fileExternalID, err := ids.New("file_")
	if err != nil {
		t.Fatalf("new file id: %v", err)
	}
	defaultIDs := getDefaultDBIDs(t, app.db)
	scopeType := "session"
	if err := app.db.CreateFile(context.Background(), db.FileRecord{
		UUID:              uuid.NewString(),
		ExternalID:        fileExternalID,
		WorkspaceID:       defaultIDs.WorkspaceID,
		Filename:          "scoped.txt",
		MimeType:          "text/plain",
		SizeBytes:         1,
		SHA256:            "00",
		S3Bucket:          app.store.Name(),
		S3Key:             "metadata-only/" + fileExternalID,
		Downloadable:      false,
		ScopeType:         &scopeType,
		ScopeID:           &scopeID,
		CreatedByAPIKeyID: defaultIDs.APIKeyID,
		CreatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create metadata-only file: %v", err)
	}
	return fileExternalID
}

func createDownloadableFile(t *testing.T, app *testApp, filename, contentType string, content []byte) (string, string) {
	t.Helper()
	fileExternalID, err := ids.New("file_")
	if err != nil {
		t.Fatalf("new file id: %v", err)
	}
	fileUUID := uuid.NewString()
	defaultIDs := getDefaultDBIDs(t, app.db)
	objectKey := "workspaces/" + defaultIDs.WorkspaceUUID + "/files/" + fileUUID + "/" + filename
	if _, err := app.store.Upload(context.Background(), objectKey, bytes.NewReader(content), storage.UploadOptions{Size: int64(len(content)), ContentType: contentType}); err != nil {
		t.Fatalf("put downloadable object: %v", err)
	}
	sum := sha256.Sum256(content)
	if err := app.db.CreateFile(context.Background(), db.FileRecord{
		UUID:              fileUUID,
		ExternalID:        fileExternalID,
		WorkspaceID:       defaultIDs.WorkspaceID,
		Filename:          filename,
		MimeType:          contentType,
		SizeBytes:         int64(len(content)),
		SHA256:            fmt.Sprintf("%x", sum),
		S3Bucket:          app.store.Name(),
		S3Key:             objectKey,
		Downloadable:      true,
		CreatedByAPIKeyID: defaultIDs.APIKeyID,
		CreatedAt:         time.Now().UTC(),
	}); err != nil {
		_ = app.store.Delete(context.Background(), objectKey, storage.DeleteOptions{})
		t.Fatalf("create downloadable metadata: %v", err)
	}
	return fileExternalID, objectKey
}

func softDeleteFile(t *testing.T, database *db.DB, fileID string) {
	t.Helper()
	var workspaceID int64
	if err := database.Pool.QueryRow(context.Background(), `
		select workspace_id from files where external_id = $1 and deleted_at is null
	`, fileID).Scan(&workspaceID); errors.Is(err, pgx.ErrNoRows) {
		return
	} else if err != nil {
		t.Fatalf("load file %s before soft delete: %v", fileID, err)
	}
	if err := database.SoftDeleteFile(context.Background(), workspaceID, fileID); err != nil {
		t.Fatalf("soft delete file %s: %v", fileID, err)
	}
}

type defaultDBIDs struct {
	OrganizationUUID string
	WorkspaceID      int64
	WorkspaceUUID    string
	APIKeyID         int64
}

func getDefaultDBIDs(t *testing.T, database *db.DB) defaultDBIDs {
	t.Helper()
	var ids defaultDBIDs
	if err := database.Pool.QueryRow(context.Background(), `
		select o.uuid::text, w.id, w.uuid::text, ak.id
		from workspaces w
		join organizations o on o.id = w.organization_id
		join api_keys ak on ak.workspace_id = w.id
		where w.external_id = 'workspace_default'
			and ak.external_id = 'api_key_default'
	`).Scan(&ids.OrganizationUUID, &ids.WorkspaceID, &ids.WorkspaceUUID, &ids.APIKeyID); err != nil {
		t.Fatalf("load default db ids: %v", err)
	}
	return ids
}

func decodeJSON(t *testing.T, r io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(target); err != nil {
		t.Fatalf("decode json: %v", err)
	}
}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return data
}

func generatedPNG(t *testing.T, width int, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: uint8((x + y) % 256), A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}
	return out.Bytes()
}

func imageConfig(t *testing.T, content []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("decode image config: %v", err)
	}
	return cfg.Width, cfg.Height
}

type fakeStore struct {
	bucket         string
	objects        map[string]fakeObject
	getOverride    storage.Object
	deleteErr      error
	deleteErrByKey map[string]error
}

type fakeObject struct {
	data        []byte
	contentType string
}

func newFakeStore(bucket string) *fakeStore {
	return &fakeStore{bucket: bucket, objects: make(map[string]fakeObject)}
}

func (s *fakeStore) Ensure(context.Context) error {
	return nil
}

func (s *fakeStore) Upload(_ context.Context, key string, body io.Reader, options storage.UploadOptions) (storage.UploadResult, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return storage.UploadResult{}, err
	}
	s.objects[key] = fakeObject{data: data, contentType: options.ContentType}
	return storage.UploadResult{Size: int64(len(data))}, nil
}

func (s *fakeStore) Open(_ context.Context, key string, byteRange *storage.ByteRange) (storage.Object, error) {
	if s.getOverride.Body != nil {
		return s.getOverride, nil
	}
	object, ok := s.objects[key]
	if !ok {
		return storage.Object{}, storage.ErrNotFound
	}
	data := object.data
	if byteRange != nil {
		start := byteRange.Offset
		if start < 0 || start > int64(len(data)) {
			return storage.Object{}, storage.ErrInvalidRange
		}
		end := int64(len(data))
		if byteRange.Length >= 0 && start+byteRange.Length < end {
			end = start + byteRange.Length
		}
		data = data[start:end]
	}
	return storage.Object{
		Body:        io.NopCloser(bytes.NewReader(data)),
		Size:        int64(len(data)),
		ContentType: object.contentType,
	}, nil
}

func (s *fakeStore) Copy(_ context.Context, sourceKey, destinationKey string) (storage.CopyResult, error) {
	object, ok := s.objects[sourceKey]
	if !ok {
		return storage.CopyResult{}, storage.ErrNotFound
	}
	s.objects[destinationKey] = fakeObject{data: append([]byte(nil), object.data...), contentType: object.contentType}
	return storage.CopyResult{}, nil
}

func (s *fakeStore) Delete(_ context.Context, key string, _ storage.DeleteOptions) error {
	if err := s.deleteErrByKey[key]; err != nil {
		return err
	}
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, key)
	return nil
}

func (s *fakeStore) Name() string {
	return s.bucket
}

type fakeStorageClient struct {
	buckets map[string]storage.ObjectStore
}

func newFakeStorageClient(buckets ...storage.ObjectStore) *fakeStorageClient {
	client := &fakeStorageClient{buckets: make(map[string]storage.ObjectStore, len(buckets))}
	for _, bucket := range buckets {
		client.buckets[bucket.Name()] = bucket
	}
	return client
}

func (c *fakeStorageClient) ForBucket(name string) (storage.ObjectStore, error) {
	bucket, ok := c.buckets[name]
	if !ok {
		return nil, fmt.Errorf("bucket %q is not configured", name)
	}
	return bucket, nil
}

type errorReadCloser struct {
	data []byte
	err  error
	done bool
}

func (r *errorReadCloser) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	return copy(p, r.data), nil
}

func (r *errorReadCloser) Close() error {
	return nil
}
