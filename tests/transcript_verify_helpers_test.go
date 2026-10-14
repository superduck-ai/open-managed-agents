package tests

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptretention"
	"go.yaml.in/yaml/v3"
)

type transcriptVerifyStore struct {
	storage.ObjectStore
	beforeUpload func() error
	afterUpload  func() error
	uploads      atomic.Int32
}

func (s *transcriptVerifyStore) Upload(ctx context.Context, key string, body io.Reader, opts storage.UploadOptions) (storage.UploadResult, error) {
	archive := strings.HasPrefix(key, "transcript-archive/")
	if archive {
		s.uploads.Add(1)
		if s.beforeUpload != nil {
			if err := s.beforeUpload(); err != nil {
				return storage.UploadResult{}, err
			}
		}
	}
	result, err := s.ObjectStore.Upload(ctx, key, body, opts)
	if err == nil && archive && s.afterUpload != nil {
		err = s.afterUpload()
	}
	return result, err
}

type transcriptVerification struct {
	app     *testApp
	session db.CodeSession
	scope   db.TranscriptScope
	objects *transcriptVerifyStore
	client  storage.Client
	service *transcriptretention.Service
	before  []byte
	started time.Time
	count   int
}

func requireTranscriptVerification(t *testing.T) {
	t.Helper()
	if os.Getenv("VERIFY_BE_TRANSCRIPT") != "1" {
		t.Skip("run through verify-be transcript with disposable PostgreSQL and MinIO")
	}
	if !strings.HasPrefix(os.Getenv("VERIFY_BE_RUN_ID"), "verify-be-") || os.Getenv("CONFIG_FILE") == "" {
		t.Fatal("isolated verification configuration required")
	}
}

func newTranscriptVerification(t *testing.T, count int, large bool) *transcriptVerification {
	t.Helper()
	requireTranscriptVerification(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.S3.Bucket != "verify-be" || cfg.TranscriptArchive.Enabled {
		t.Fatal("refusing non-verification storage or active archive scheduling")
	}
	client, err := storage.New(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := client.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &transcriptVerifyStore{ObjectStore: objects}
	app := newPayloadIntegrationApp(t, wrapped)
	session, _ := newPayloadIntegrationSession(t, app)
	inputs := make([]db.AppendCodeSessionInternalEventInput, count)
	if large {
		inputs[0].Payload = []byte(sizedPrivatePayload("large", 65536))
	}
	seedArchiveEvents(t, app, session, inputs)
	policy := transcriptPolicy()
	policy.HardDeleteEnabled = true
	f := &transcriptVerification{app: app, session: session, scope: transcriptScope(session), objects: wrapped, client: client, started: time.Now(), count: count}
	f.service = newTranscriptRetentionService(t, app, wrapped, policy)
	f.before = f.export(t)
	makeArchiveTerminal(t, app, session)
	return f
}

func (f *transcriptVerification) export(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := f.service.Export(t.Context(), f.scope, &out); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func (f *transcriptVerification) assertExport(t *testing.T) {
	t.Helper()
	if !bytes.Equal(f.before, f.export(t)) {
		t.Fatal("exported history changed identities, sequence, timestamps, payload or metadata")
	}
}

func (f *transcriptVerification) archive(t *testing.T) {
	t.Helper()
	if err := f.service.Archive(t.Context(), f.scope, true); err != nil {
		t.Fatal(err)
	}
}

func (f *transcriptVerification) rows(t *testing.T, total, live int) {
	t.Helper()
	assertPayloadSQLCount(t, f.app, "select count(*) from code_session_internal_events where code_session_uuid=$1", total, f.scope.CodeSessionUUID)
	assertPayloadSQLCount(t, f.app, "select count(*) from code_session_internal_events where code_session_uuid=$1 and deleted_at is null", live, f.scope.CodeSessionUUID)
}

func (f *transcriptVerification) segment(t *testing.T, state string) db.TranscriptArchive {
	t.Helper()
	archives, err := f.app.db.ListTranscriptArchives(t.Context(), f.scope, 0, 100, false)
	if err != nil || len(archives) != 1 || archives[0].State != state || archives[0].EventCount != f.count {
		t.Fatalf("expected one %s manifest covering %d rows: %+v %v", state, f.count, archives, err)
	}
	return archives[0]
}

func (f *transcriptVerification) ageDeleted(t *testing.T) {
	t.Helper()
	if _, err := f.app.pool.Exec(t.Context(), "update code_session_internal_events set deleted_at=now()-interval '15 days' where code_session_uuid=$1 and deleted_at is not null", f.scope.CodeSessionUUID); err != nil {
		t.Fatal(err)
	}
}

func (f *transcriptVerification) hardDelete(t *testing.T) {
	t.Helper()
	if err := f.service.HardDelete(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}
}

func transcriptVerifyProof(t *testing.T, started time.Time, stage string) {
	t.Helper()
	t.Logf("BE_PROOF {\"stage\":%q,\"elapsed_ms\":%d}", stage, time.Since(started).Milliseconds())
}

func transcriptObjectBytes(t *testing.T, objects storage.ObjectStore, key string) []byte {
	t.Helper()
	object, err := objects.Open(t.Context(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	data, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (f *transcriptVerification) maintenanceCLI(t *testing.T) func(string, db.TranscriptScope) ([]byte, error) {
	t.Helper()
	directory := t.TempDir()
	binary := filepath.Join(directory, "transcript-archive")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/transcript-archive")
	cmd.Dir = ".."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build maintenance CLI: %v %s", err, output)
	}
	configPath := filepath.Join(directory, "config.yaml")
	data, err := yaml.Marshal(f.app.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return func(mode string, scope db.TranscriptScope) ([]byte, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "-mode", mode, "-organization", scope.OrganizationUUID, "-workspace", scope.WorkspaceUUID, "-code-session", scope.CodeSessionUUID)
		cmd.Env = append(os.Environ(), "CONFIG_FILE="+configPath)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		if err := cmd.Run(); err != nil {
			return nil, errors.New("maintenance CLI failed: " + err.Error())
		}
		return out.Bytes(), nil
	}
}
