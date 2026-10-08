package tests

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type memoryVerifyObjects struct {
	storage.ObjectStore
	failUpload atomic.Bool
	failDelete atomic.Bool
	mu         sync.Mutex
	keys       []string
}

func (s *memoryVerifyObjects) Upload(ctx context.Context, key string, body io.Reader, opts storage.UploadOptions) (storage.UploadResult, error) {
	if s.failUpload.Load() {
		return storage.UploadResult{}, errors.New("injected upload rejection")
	}
	result, err := s.ObjectStore.Upload(ctx, key, body, opts)
	if err == nil {
		s.mu.Lock()
		s.keys = append(s.keys, key)
		s.mu.Unlock()
	}
	return result, err
}

func (s *memoryVerifyObjects) Delete(ctx context.Context, key string, opts storage.DeleteOptions) error {
	if s.failDelete.Load() {
		return errors.New("injected delete rejection")
	}
	return s.ObjectStore.Delete(ctx, key, opts)
}

type memoryVerifyClient struct{ objects storage.ObjectStore }

func (c memoryVerifyClient) ForBucket(name string) (storage.ObjectStore, error) {
	if name != c.objects.Name() {
		return nil, errors.New("unexpected cleanup bucket")
	}
	return c.objects, nil
}

type memoryVerification struct {
	app     *testApp
	agentID string
	fx      memoryFilestoreFixture
	objects *memoryVerifyObjects
	started time.Time
}

func newMemoryVerification(t *testing.T) *memoryVerification {
	t.Helper()
	if os.Getenv("VERIFY_BE_MEMORY") != "1" {
		t.Skip("run through verify-be memory with disposable dependencies")
	}
	if !strings.HasPrefix(os.Getenv("VERIFY_BE_RUN_ID"), "verify-be-") || os.Getenv("CONFIG_FILE") == "" {
		t.Fatal("isolated verification configuration required")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.S3.Bucket != "verify-be" {
		t.Fatal("refusing non-verification storage")
	}
	client, err := storage.New(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := client.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &memoryVerifyObjects{ObjectStore: objects}
	app := newPayloadIntegrationApp(t, wrapped)
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"memory-verification"}`)
	environment := createEnvironment(t, app, `{"name":"memory-verification"}`)
	fx := newMemoryFilestoreFixture(t, app, agent.ID, environment.ID, "verified-memory", "")
	t.Cleanup(func() {
		wrapped.failDelete.Store(false)
		wrapped.mu.Lock()
		keys := append([]string(nil), wrapped.keys...)
		wrapped.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, key := range keys {
			if err := objects.Delete(ctx, key, storage.DeleteOptions{}); err != nil {
				t.Error(err)
			}
		}
	})
	s3Client := s3.New(s3.Options{Region: cfg.Storage.S3.Region, BaseEndpoint: aws.String(cfg.Storage.S3.Endpoint), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(cfg.Storage.S3.AccessKeyID, cfg.Storage.S3.SecretAccessKey, "")})
	versioning, err := s3Client.GetBucketVersioning(t.Context(), &s3.GetBucketVersioningInput{Bucket: aws.String(objects.Name())})
	if err != nil {
		t.Fatal(err)
	}
	if versioning.Status != "" {
		t.Fatalf("Memory verification requires an unversioned bucket, status=%s", versioning.Status)
	}
	return &memoryVerification{app: app, agentID: agent.ID, fx: fx, objects: wrapped, started: time.Now()}
}

func (f *memoryVerification) attach(t *testing.T, access string) memoryFilestoreFixture {
	t.Helper()
	fx := f.fx
	fx.session = createSession(t, f.app, sessionBodyWithMemoryResource(f.agentID, f.fx.session.EnvironmentID, fx.store.ID, `"access":"`+access+`"`))
	fx.resource = decodeMemoryResource(t, fx.session.Resources[0])
	var err error
	fx.filesystem, err = f.app.db.GetFilestoreFilesystemBySession(t.Context(), fx.workspaceUUID, fx.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := f.app.db.GetFilestoreTokenScopeForSessionIssue(t.Context(), fx.workspaceUUID, fx.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	fx.token, err = f.app.filestoreCredentials.Issue(filestore.TokenIdentity{Subject: scope.AccountExternalID, OrgUUID: scope.OrganizationUUID, AccountUUID: scope.AccountUUID, WorkspaceUUID: scope.WorkspaceUUID, WorkspaceTaggedID: scope.WorkspaceExternalID, ResolvedWorkspaceTaggedID: scope.WorkspaceExternalID, FilesystemID: scope.FilesystemExternalID, OrgTaints: scope.OrgTaints, WorkspaceCMEKEnabled: scope.WorkspaceCMEKEnabled})
	if err != nil {
		t.Fatal(err)
	}
	return fx
}

func memoryVerifyResponse(t *testing.T, response *http.Response, status int) []byte {
	t.Helper()
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("status=%d want=%d body=%s", response.StatusCode, status, data)
	}
	return data
}

func memoryVerifyWrite(t *testing.T, fx memoryFilestoreFixture, path, content string) {
	t.Helper()
	memoryVerifyResponse(t, fx.createFile(t, path, []byte(content)), http.StatusOK)
}

func memoryVerifyRead(t *testing.T, fx memoryFilestoreFixture, path, expected string) {
	t.Helper()
	got := memoryVerifyResponse(t, fx.json(t, "readFile", map[string]any{"filesystemId": fx.filesystem.ExternalID, "path": path}), 200)
	if !bytes.Equal(got, []byte(expected)) {
		t.Fatalf("file %s bytes=%q want=%q", path, got, expected)
	}
}

func (f *memoryVerification) path(relative string) string { return "/memory/" + f.fx.slug() + relative }

func (f *memoryVerification) proof(t *testing.T, stage string) {
	t.Helper()
	t.Logf("BE_PROOF {\"stage\":%q,\"elapsed_ms\":%d}", stage, time.Since(f.started).Milliseconds())
}

func (f *memoryVerification) assertObject(t *testing.T, key, expected string) {
	t.Helper()
	object, err := f.objects.Open(t.Context(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := readAll(t, object.Body)
	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("object bytes=%q want=%q", data, expected)
	}
}

func (f *memoryVerification) assertObjectAbsent(t *testing.T, key string) {
	t.Helper()
	object, err := f.objects.Open(t.Context(), key, nil)
	if err == nil {
		if closeErr := object.Body.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatalf("object cleanup left readable object %s", key)
	}
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("object absence check failed: %v", err)
	}
}

func (f *memoryVerification) rejectCleanupQueue(t *testing.T) func() {
	t.Helper()
	_, err := f.app.pool.Exec(t.Context(), "create function reject_verify_memory_cleanup() returns trigger language plpgsql as $$ begin if NEW.type='object_cleanup' then raise exception 'injected cleanup queue rejection'; end if; return NEW; end $$; create trigger reject_verify_memory_cleanup before insert on jobs for each row execute function reject_verify_memory_cleanup()")
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := f.app.pool.Exec(t.Context(), "drop trigger reject_verify_memory_cleanup on jobs"); err != nil {
			t.Fatal(err)
		}
	}
}
