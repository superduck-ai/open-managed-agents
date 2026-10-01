package livefiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type filesEnv struct {
	ctx      context.Context
	database *db.DB
	objects  storage.ObjectStore
	s3       *s3.Client
	cfg      config.Config
	key      db.APIKey
	token    string
	url      string
	started  time.Time
}

type metadata struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Filename     string `json:"filename"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	Downloadable bool   `json:"downloadable"`
	CreatedAt    string `json:"created_at"`
}

type filePage struct {
	Data    []metadata `json:"data"`
	HasMore bool       `json:"has_more"`
	LastID  string     `json:"last_id"`
}

func requireOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newFilesEnv(t *testing.T) *filesEnv {
	t.Helper()
	if os.Getenv("VERIFY_BE_RUN_ID") == "" {
		t.Skip("run through verify-be files with isolated dependencies")
	}
	if os.Getenv("CONFIG_FILE") == "" || os.Getenv("VERIFY_BE_API_URL") != "http://127.0.0.1:18080" {
		t.Fatal("isolated verification configuration required")
	}
	timeout, err := time.ParseDuration(os.Getenv("VERIFY_BE_TIMEOUT"))
	requireOK(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	t.Cleanup(cancel)
	cfg, err := config.Load()
	requireOK(t, err)
	if cfg.Storage.S3.Bucket != "verify-be" || cfg.Storage.MaxFileBytes != 65536 || cfg.Storage.WorkspaceLimitBytes != 98304 {
		t.Fatal("refusing non-verification storage configuration")
	}
	database, err := db.Open(ctx, cfg, nil)
	requireOK(t, err)
	t.Cleanup(database.Close)
	token := config.DefaultAPIKey
	key, err := database.GetAPIKey(ctx, auth.HashAPIKey(token))
	requireOK(t, err)
	client, err := storage.New(cfg.Storage)
	requireOK(t, err)
	objects, err := client.ForBucket(cfg.Storage.S3.Bucket)
	requireOK(t, err)
	e := &filesEnv{ctx: ctx, cfg: cfg, database: database, objects: objects, key: key, token: token, url: os.Getenv("VERIFY_BE_API_URL"), started: time.Now()}
	e.s3 = s3.New(s3.Options{Region: cfg.Storage.S3.Region, BaseEndpoint: aws.String(cfg.Storage.S3.Endpoint), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(cfg.Storage.S3.AccessKeyID, cfg.Storage.S3.SecretAccessKey, "")})
	return e
}

func (e *filesEnv) proof(t *testing.T, stage string) {
	t.Helper()
	value, err := json.Marshal(struct {
		Stage     string `json:"stage"`
		ElapsedMS int64  `json:"elapsed_ms"`
	}{stage, time.Since(e.started).Milliseconds()})
	requireOK(t, err)
	t.Logf("BE_PROOF %s", value)
}

func (e *filesEnv) request(t *testing.T, method, path, token, contentType string, body io.Reader, beta bool, status int) ([]byte, http.Header) {
	t.Helper()
	ctx, cancel := context.WithTimeout(e.ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, e.url+path, body)
	requireOK(t, err)
	if token != "" {
		req.Header.Set("x-api-key", token)
	}
	if beta {
		req.Header.Set("anthropic-beta", "files-api-2025-04-14")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response, err := http.DefaultClient.Do(req)
	requireOK(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	requireOK(t, err)
	if response.StatusCode != status {
		t.Fatalf("%s %s status=%d want=%d", method, path, response.StatusCode, status)
	}
	if status >= 400 {
		var failure struct {
			Type  string `json:"type"`
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		requireOK(t, json.Unmarshal(data, &failure))
		wantType := map[int]string{400: "invalid_request_error", 401: "authentication_error", 403: "permission_error", 404: "not_found_error", 409: "conflict_error", 413: "invalid_request_error", 500: "api_error"}[status]
		if failure.Type != "error" || failure.Error.Type != wantType {
			t.Fatal("missing API error envelope")
		}
	}
	return data, response.Header
}

func multipartFile(t *testing.T, name string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if name != "" {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, name))
		header.Set("Content-Type", "application/octet-stream")
		part, err := writer.CreatePart(header)
		requireOK(t, err)
		_, err = part.Write(content)
		requireOK(t, err)
	} else {
		requireOK(t, writer.WriteField("description", "missing file"))
	}
	requireOK(t, writer.Close())
	return &body, writer.FormDataContentType()
}

func (e *filesEnv) upload(t *testing.T, name string, content []byte, status int) metadata {
	t.Helper()
	body, kind := multipartFile(t, name, content)
	data, _ := e.request(t, "POST", "/v1/files", e.token, kind, body, true, status)
	var result metadata
	if status == 200 {
		requireOK(t, json.Unmarshal(data, &result))
	}
	return result
}

func (e *filesEnv) record(t *testing.T, id string) db.FileRecord {
	t.Helper()
	record, err := e.database.GetFile(e.ctx, e.key.WorkspaceUUID.String(), id)
	requireOK(t, err)
	return record
}

func (e *filesEnv) assertObject(t *testing.T, key string, content []byte) {
	t.Helper()
	object, err := e.objects.Open(e.ctx, key, nil)
	requireOK(t, err)
	defer object.Body.Close()
	got, err := io.ReadAll(object.Body)
	requireOK(t, err)
	if !bytes.Equal(got, content) || object.Size != int64(len(content)) {
		t.Fatal("object bytes or size differ")
	}
}

func (e *filesEnv) objectAbsent(t *testing.T, key string) bool {
	t.Helper()
	object, err := e.objects.Open(e.ctx, key, nil)
	if err == nil {
		requireOK(t, object.Body.Close())
		return false
	}
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("object absence check failed: %v", err)
	}
	return true
}

func (e *filesEnv) objectKeys(t *testing.T) []string {
	t.Helper()
	prefix := "workspaces/" + e.key.WorkspaceUUID.String() + "/files/"
	pages := s3.NewListObjectsV2Paginator(e.s3, &s3.ListObjectsV2Input{Bucket: aws.String(e.objects.Name()), Prefix: aws.String(prefix)})
	var keys []string
	for pages.HasMorePages() {
		page, err := pages.NextPage(e.ctx)
		requireOK(t, err)
		for _, object := range page.Contents {
			keys = append(keys, aws.ToString(object.Key))
		}
	}
	return keys
}

func (e *filesEnv) downloadable(t *testing.T, content []byte) db.FileRecord {
	t.Helper()
	id := uuid.NewString()
	sum := sha256.Sum256(content)
	record := db.FileRecord{UUID: id, ExternalID: "file_" + strings.ReplaceAll(id, "-", ""), WorkspaceUUID: e.key.WorkspaceUUID.String(), Filename: "output.bin", MimeType: "application/octet-stream", SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(sum[:]), S3Bucket: e.objects.Name(), S3Key: "workspaces/" + e.key.WorkspaceUUID.String() + "/files/" + id + "/output.bin", Downloadable: true, CreatedByAPIKeyUUID: e.key.UUID.String(), CreatedAt: time.Now().UTC()}
	_, err := e.objects.Upload(e.ctx, record.S3Key, bytes.NewReader(content), storage.UploadOptions{Size: record.SizeBytes, ContentType: record.MimeType})
	requireOK(t, err)
	requireOK(t, e.database.CreateFile(e.ctx, record))
	return record
}

func (e *filesEnv) delete(t *testing.T, record db.FileRecord) {
	t.Helper()
	data, _ := e.request(t, "DELETE", "/v1/files/"+record.ExternalID, e.token, "", nil, true, 200)
	var deleted struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	requireOK(t, json.Unmarshal(data, &deleted))
	if deleted.ID != record.ExternalID || deleted.Type != "file_deleted" {
		t.Fatal("invalid deletion response")
	}
	for _, suffix := range []string{"", "/content"} {
		e.request(t, "GET", "/v1/files/"+record.ExternalID+suffix, e.token, "", nil, true, 404)
	}
	e.request(t, "DELETE", "/v1/files/"+record.ExternalID, e.token, "", nil, true, 404)
	if !e.objectAbsent(t, record.S3Key) {
		t.Fatal("deleted object remains")
	}
}

func (e *filesEnv) tenantKey(t *testing.T, otherOrganization bool) string {
	t.Helper()
	org := e.key.OrganizationUUID.String()
	if otherOrganization {
		requireOK(t, e.database.WithPlatformAuthTx(e.ctx, func(tx db.PlatformAuthTxStore) error {
			created, err := tx.InsertOrganization(e.ctx, db.PlatformAuthOrganizationInput{Name: "verify-files"})
			org = created.UUID
			return err
		}))
	}
	workspace, err := e.database.CreateConsoleWorkspace(e.ctx, platform.CreateConsoleWorkspaceInput{OrgUUID: org, Name: "verify-files-" + uuid.NewString()})
	requireOK(t, err)
	key, err := e.database.CreateConsoleAPIKey(e.ctx, platform.CreateConsoleAPIKeyInput{OrgUUID: org, WorkspaceUUID: workspace.UUID, Name: "verify-files"})
	requireOK(t, err)
	return key.RawKey
}
