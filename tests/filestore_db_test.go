package tests

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/google/uuid"
)

func TestSessionNamespaceUsesResourcesAndFiles(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-entry-schema"))
	t.Cleanup(app.close)

	var oldTableExists bool
	if err := app.db.Pool.QueryRow(context.Background(), `
		select to_regclass(current_schema() || '.filestore_entries') is not null
	`).Scan(&oldTableExists); err != nil {
		t.Fatalf("query old Filestore table: %v", err)
	}
	if oldTableExists {
		t.Fatal("filestore_entries still exists")
	}

	var resourceColumns, fileColumns, forbiddenColumns int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select
			count(*) filter (where table_name = 'session_resources'
				and column_name = any($1::text[])),
			count(*) filter (where table_name = 'files'
				and column_name = any($2::text[])),
			count(*) filter (where table_name in ('session_resources', 'files')
				and column_name = any($3::text[]))
		from information_schema.columns
		where table_schema = current_schema()
	`, []string{"path", "parent_path", "file_uuid", "file_ownership", "expires_at"},
		[]string{"detected_mime_type", "metadata", "authorization_metadata", "tags", "md5", "s3_etag", "s3_version_id"},
		[]string{"attached", "cataloged", "namespace_role", "filesystem_uuid", "source_file_uuid", "session_file_external_id", "skill_version_uuid", "skill_source", "skill_id", "skill_size_bytes", "skill_sha256", "skill_s3_bucket", "skill_s3_key"},
	).Scan(&resourceColumns, &fileColumns, &forbiddenColumns); err != nil {
		t.Fatalf("query unified namespace columns: %v", err)
	}
	if resourceColumns != 5 || fileColumns != 7 || forbiddenColumns != 0 {
		t.Fatalf("unified columns = resources %d files %d forbidden %d", resourceColumns, fileColumns, forbiddenColumns)
	}
}

func TestSessionResourceFileOwnershipConstraints(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-ownership-constraints"))
	t.Cleanup(app.close)
	_, _, organizationUUID, workspaceUUID, _, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	input := filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID)
	session, _, _, _, err := app.db.CreateSession(context.Background(), input)
	if err != nil {
		t.Fatalf("create ownership constraint Session: %v", err)
	}
	cleanupFilestoreSession(t, app, workspaceUUID, session.ExternalID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatalf("load ownership constraint filesystem: %v", err)
	}

	t.Run("failure directory cannot carry File ownership", func(t *testing.T) {
		if _, err := app.db.Pool.Exec(context.Background(), `
			update session_resources
			set file_ownership = 'owned'
			where workspace_uuid = $1 and session_uuid = $2 and path = '/outputs'
		`, workspaceUUID, session.UUID); err == nil {
			t.Fatal("directory accepted owned File ownership")
		}
	})

	result, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceUUID:  workspaceUUID,
		FilesystemUUID: filesystem.UUID,
		Path:           "/outputs/owned.txt",
		Blob:           workspaceStorageBlob(5, nil),
	})
	if err != nil {
		t.Fatalf("create owned File for constraint tests: %v", err)
	}

	t.Run("failure namespace File requires ownership", func(t *testing.T) {
		if _, err := app.db.Pool.Exec(context.Background(), `
			update session_resources set file_ownership = null where uuid = $1
		`, result.Node.UUID); err == nil {
			t.Fatal("namespace File accepted NULL ownership")
		}
	})

	t.Run("failure ownership value is closed", func(t *testing.T) {
		if _, err := app.db.Pool.Exec(context.Background(), `
			update session_resources set file_ownership = 'borrowed' where uuid = $1
		`, result.Node.UUID); err == nil {
			t.Fatal("namespace File accepted unknown ownership")
		}
	})

	t.Run("success owned File remains explicit", func(t *testing.T) {
		entry, err := app.db.GetSessionResourceFile(
			context.Background(),
			workspaceUUID,
			filesystem.UUID,
			"/outputs/owned.txt",
		)
		if err != nil || !entry.OwnsFile() || entry.ReferencesSourceFile() {
			t.Fatalf("owned File after rejected constraint writes = (%+v, %v)", entry, err)
		}
	})
}

func TestCreateSessionRejectsFileResourcesAboveDBLimit(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-session-file-limit"))
	t.Cleanup(app.close)
	_, _, organizationUUID, workspaceUUID, _, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	input := filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID)

	for index := range db.MaxSessionFileResources + 1 {
		resourceID := fmt.Sprintf("sesrsc_file_limit_%03d", index)
		input.Resources = append(input.Resources, db.CreateSessionResourceInput{
			Resource: db.SessionResource{
				UUID:             uuid.NewString(),
				ExternalID:       resourceID,
				OrganizationUUID: organizationUUID,
				WorkspaceUUID:    workspaceUUID,
				ResourceType:     db.SessionResourceTypeFile,
				Payload:          json.RawMessage(`{}`),
				SecretPayload:    json.RawMessage(`{}`),
				CreatedAt:        input.Session.CreatedAt,
				UpdatedAt:        input.Session.CreatedAt,
			},
			FileMount: &db.SessionFileMount{
				ResourceExternalID: resourceID,
				FileExternalID:     fmt.Sprintf("file_limit_%03d", index),
				Path:               fmt.Sprintf("/uploads/db-limit/file-%03d.txt", index),
			},
		})
	}

	_, _, _, _, err := app.db.CreateSession(context.Background(), input)
	var limitError *db.SessionFileResourceLimitError
	if !errors.As(err, &limitError) || limitError.Limit != db.MaxSessionFileResources {
		t.Fatalf("CreateSession() error = %v, want file resource limit %d", err, db.MaxSessionFileResources)
	}

	if _, err := app.db.GetSession(
		context.Background(),
		workspaceUUID,
		input.Session.ExternalID,
	); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("GetSession() after rollback error = %v, want ErrNotFound", err)
	}
}

func TestCreateSessionRollsBackWhenFilesystemScopeIsInvalid(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-session-rollback"))
	t.Cleanup(app.close)
	_, _, organizationUUID, workspaceUUID, _, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	input := filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID)
	input.Session.CreatedByAPIKeyUUID = uuid.NewString()

	if _, _, _, _, err := app.db.CreateSession(context.Background(), input); !errors.Is(err, db.ErrPreconditionFailed) {
		t.Fatalf("CreateSession() error = %v, want ErrPreconditionFailed", err)
	}
	var sessionCount, threadCount, workCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select
			(select count(*) from sessions where external_id = $1),
			(select count(*) from session_threads where external_id = $2),
			(select count(*) from environment_work where external_id = $3)
	`, input.Session.ExternalID, input.Thread.ExternalID, input.Work.ExternalID).Scan(
		&sessionCount,
		&threadCount,
		&workCount,
	); err != nil {
		t.Fatalf("count rolled-back session graph: %v", err)
	}
	if sessionCount != 0 || threadCount != 0 || workCount != 0 {
		t.Fatalf("rolled-back rows = session %d, thread %d, work %d", sessionCount, threadCount, workCount)
	}
}

func TestCreateSessionRollsBackAfterFilesystemIDCollisions(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-session-collision-rollback"))
	t.Cleanup(app.close)
	_, _, organizationUUID, workspaceUUID, _, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	for _, value := range []byte{0, 1, 2} {
		insertFilestoreCollisionOwner(
			t,
			app,
			organizationUUID,
			workspaceUUID,
			apiKeyUUID,
			filestoreExternalIDForRandomByte(value),
		)
	}
	input := filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID)
	previousRandomReader := cryptorand.Reader
	cryptorand.Reader = filestoreRandomReader(0, 1, 2)
	t.Cleanup(func() {
		cryptorand.Reader = previousRandomReader
	})

	if _, _, _, _, err := app.db.CreateSession(context.Background(), input); !errors.Is(err, db.ErrDuplicate) {
		t.Fatalf("CreateSession() error = %v, want ErrDuplicate", err)
	}
	var sessionCount, threadCount, filesystemCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select
			(select count(*) from sessions where uuid = $1),
			(select count(*) from session_threads where uuid = $2),
			(select count(*) from filestore_filesystems where session_uuid = $1)
	`, input.Session.UUID, input.Thread.UUID).Scan(&sessionCount, &threadCount, &filesystemCount); err != nil {
		t.Fatalf("count collision rollback rows: %v", err)
	}
	if sessionCount != 0 || threadCount != 0 || filesystemCount != 0 {
		t.Fatalf("collision rollback rows = session %d, thread %d, filesystem %d", sessionCount, threadCount, filesystemCount)
	}
}

func TestCreateSessionRetriesFilesystemIDCollision(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-session-collision-retry"))
	t.Cleanup(app.close)
	_, _, organizationUUID, workspaceUUID, _, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	insertFilestoreCollisionOwner(
		t,
		app,
		organizationUUID,
		workspaceUUID,
		apiKeyUUID,
		filestoreExternalIDForRandomByte(0),
	)
	input := filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID)
	previousRandomReader := cryptorand.Reader
	// 前两个块覆盖 filesystem ID 的冲突与重试；后续块供成功建档后
	// 五个固定根目录生成 Resource UUID/外部 ID。
	cryptorand.Reader = filestoreRandomReader(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)
	t.Cleanup(func() {
		cryptorand.Reader = previousRandomReader
	})

	created, _, _, _, err := app.db.CreateSession(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	cleanupFilestoreSession(t, app, workspaceUUID, created.ExternalID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceUUID, created.ExternalID)
	if err != nil {
		t.Fatalf("GetFilestoreFilesystemBySession() error = %v", err)
	}
	if filesystem.ExternalID != filestoreExternalIDForRandomByte(1) {
		t.Fatalf("filesystem external ID = %q, want second generated candidate", filesystem.ExternalID)
	}
}

func TestCreateSessionProvisionsFilesystem(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-session-create"))
	t.Cleanup(app.close)
	_, _, organizationUUID, workspaceUUID, _, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	input := filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID)

	created, _, _, _, err := app.db.CreateSession(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	cleanupFilestoreSession(t, app, workspaceUUID, created.ExternalID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceUUID, created.ExternalID)
	if err != nil {
		t.Fatalf("GetFilestoreFilesystemBySession() error = %v", err)
	}
	if !strings.HasPrefix(filesystem.ExternalID, "claude_chat_") || len(filesystem.ExternalID) != len("claude_chat_")+24 {
		t.Fatalf("filesystem external ID = %q, want claude_chat_ plus 24 characters", filesystem.ExternalID)
	}
	for _, character := range strings.TrimPrefix(filesystem.ExternalID, "claude_chat_") {
		if !strings.ContainsRune("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", character) {
			t.Fatalf("filesystem external ID contains non-Base62 character %q", character)
		}
	}
	if filesystem.OrganizationUUID != organizationUUID || filesystem.WorkspaceUUID != workspaceUUID ||
		filesystem.SessionUUID != created.UUID || filesystem.CodeSessionUUID != nil ||
		filesystem.CreatedByAPIKeyUUID == nil || *filesystem.CreatedByAPIKeyUUID != apiKeyUUID {
		t.Fatalf("filesystem stable references = %#v", filesystem)
	}

	var activeCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*) from filestore_filesystems
		where workspace_uuid = $1 and session_uuid = $2 and deleted_at is null
	`, workspaceUUID, created.UUID).Scan(&activeCount); err != nil {
		t.Fatalf("count session filesystems: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active session filesystems = %d, want 1", activeCount)
	}
	rows, err := app.db.Pool.Query(context.Background(), `
		select path, resource_type, parent_path
		from session_resources
		where workspace_uuid = $1
			and session_uuid = $2
			and deleted_at is null
		order by path
	`, workspaceUUID, filesystem.SessionUUID)
	if err != nil {
		t.Fatalf("list Session Filestore roots: %v", err)
	}
	defer rows.Close()
	var rootPaths []string
	for rows.Next() {
		var path, kind, parentPath string
		if err := rows.Scan(&path, &kind, &parentPath); err != nil {
			t.Fatalf("scan Session Filestore root: %v", err)
		}
		if kind != "directory" || parentPath != "/" {
			t.Fatalf("Session Filestore root = %q %q parent %q", path, kind, parentPath)
		}
		rootPaths = append(rootPaths, path)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate Session Filestore roots: %v", err)
	}
	wantRootPaths := []string{"/outputs", "/skills", "/tool_results", "/transcripts", "/uploads"}
	if !reflect.DeepEqual(rootPaths, wantRootPaths) {
		t.Fatalf("Session Filestore roots = %v, want %v", rootPaths, wantRootPaths)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := app.db.CreateCodeSession(context.Background(), db.CreateCodeSessionInput{
			ExternalID:            fmt.Sprintf("cse_filestore_retry_%d_%s", attempt, uuid.NewString()),
			OrganizationUUID:      organizationUUID,
			WorkspaceUUID:         workspaceUUID,
			SessionUUID:           created.UUID,
			SessionExternalID:     created.ExternalID,
			EnvironmentUUID:       created.EnvironmentUUID,
			EnvironmentExternalID: created.EnvironmentExternalID,
			Status:                "active",
			Metadata:              json.RawMessage(`{}`),
			OAuthAccessTokenHash:  fmt.Sprintf("filestore-retry-hash-%d-%s", attempt, uuid.NewString()),
		}); err != nil {
			t.Fatalf("create Code Session attempt %d: %v", attempt, err)
		}
		reused, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceUUID, created.ExternalID)
		if err != nil {
			t.Fatalf("load filesystem after Code Session attempt %d: %v", attempt, err)
		}
		if reused.UUID != filesystem.UUID {
			t.Fatalf("Code Session attempt %d filesystem UUID = %q, want %q", attempt, reused.UUID, filesystem.UUID)
		}
	}

	_, provisioned, err := app.db.ProvisionFilestoreFilesystem(context.Background(), db.ProvisionFilestoreFilesystemInput{
		UUID:                uuid.NewString(),
		ExternalID:          "fs_second_" + uuid.NewString(),
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		SessionUUID:         created.UUID,
		CreatedByAPIKeyUUID: stringPointer(apiKeyUUID),
	})
	if !errors.Is(err, db.ErrDuplicate) || provisioned {
		t.Fatalf("second filesystem provision = created %v, error %v; want duplicate", provisioned, err)
	}
}

func TestListSessionResourceFilesPageWithMapper(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-mapper-list"))
	t.Cleanup(app.close)

	ctx := context.Background()
	_, _, organizationUUID, workspaceUUID, _, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	created, _, _, _, err := app.db.CreateSession(ctx, filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID))
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	cleanupFilestoreSession(t, app, workspaceUUID, created.ExternalID)
	filesystem, err := app.db.GetFilestoreFilesystemBySession(ctx, workspaceUUID, created.ExternalID)
	if err != nil {
		t.Fatalf("GetFilestoreFilesystemBySession() error = %v", err)
	}

	t.Run("rejects a missing directory", func(t *testing.T) {
		_, err := app.db.ListSessionResourceFilesPage(ctx, db.ListSessionResourceFilesPageParams{
			WorkspaceUUID:  workspaceUUID,
			FilesystemUUID: filesystem.UUID,
			DirectoryPath:  "/missing",
		})
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("ListSessionResourceFilesPage() error = %v, want ErrNotFound", err)
		}
	})

	for _, directoryPath := range []string{"/reports", "/reports/june", "/reports/july"} {
		if _, err := app.db.MakeFilestoreDirectory(ctx, db.MakeFilestoreDirectoryInput{
			WorkspaceUUID:  workspaceUUID,
			FilesystemUUID: filesystem.UUID,
			Path:           directoryPath,
		}); err != nil {
			t.Fatalf("MakeFilestoreDirectory(%q) error = %v", directoryPath, err)
		}
	}

	t.Run("maps rows and advances a keyset cursor", func(t *testing.T) {
		directory, err := app.db.GetSessionResourceFile(ctx, workspaceUUID, filesystem.UUID, "/reports")
		if err != nil {
			t.Fatalf("GetSessionResourceFile() error = %v", err)
		}
		if directory.Kind != db.SessionResourceFileKindDirectory || directory.Tags == nil {
			t.Fatalf("GetSessionResourceFile() = %+v, want mapped directory", directory)
		}

		firstPage, err := app.db.ListSessionResourceFilesPage(ctx, db.ListSessionResourceFilesPageParams{
			WorkspaceUUID:  workspaceUUID,
			FilesystemUUID: filesystem.UUID,
			DirectoryPath:  "/reports",
			Limit:          1,
		})
		if err != nil {
			t.Fatalf("first ListSessionResourceFilesPage() error = %v", err)
		}
		if len(firstPage.Entries) != 1 || firstPage.Entries[0].Path != "/reports/july" || !firstPage.HasMore {
			t.Fatalf("first page = %+v, want /reports/july with HasMore", firstPage)
		}
		if firstPage.Entries[0].Tags == nil {
			t.Fatal("first page entry tags = nil, want an empty slice")
		}

		lastEntry := firstPage.Entries[0]
		secondPage, err := app.db.ListSessionResourceFilesPage(ctx, db.ListSessionResourceFilesPageParams{
			WorkspaceUUID:  workspaceUUID,
			FilesystemUUID: filesystem.UUID,
			DirectoryPath:  "/reports",
			Limit:          1,
			Cursor:         &db.SessionResourceFilePageCursor{Path: lastEntry.Path, UUID: lastEntry.UUID},
		})
		if err != nil {
			t.Fatalf("second ListSessionResourceFilesPage() error = %v", err)
		}
		if len(secondPage.Entries) != 1 || secondPage.Entries[0].Path != "/reports/june" || secondPage.HasMore {
			t.Fatalf("second page = %+v, want /reports/june without HasMore", secondPage)
		}
	})
}

func TestSessionResourceFileMutationMapperBinding(t *testing.T) {
	fixture := newWorkspaceStorageFixture(t)
	blob := workspaceStorageBlob(12, nil)
	blob.DetectedMimeType = "text/plain; charset=utf-8"
	blob.Metadata = json.RawMessage(`{"source":"mapper"}`)
	blob.AuthorizationMetadata = json.RawMessage(`{"scope":"workspace"}`)
	blob.Tags = []string{"report", "mapper"}
	blob.Downloadable = true
	blob.S3ETag = "etag-mapper-1"
	blob.S3VersionID = "version-mapper-1"

	created, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceUUID:  fixture.workspaceUUID,
		FilesystemUUID: fixture.filesystem.UUID,
		Path:           "/mapper.txt",
		Blob:           blob,
	})
	if err != nil {
		t.Fatalf("PutFilestoreFile() error = %v", err)
	}
	entry := created.Node
	if entry.Path != "/mapper.txt" ||
		entry.DetectedMimeType == nil || *entry.DetectedMimeType != blob.DetectedMimeType ||
		!reflect.DeepEqual(entry.Tags, blob.Tags) ||
		entry.S3VersionID == nil || *entry.S3VersionID != blob.S3VersionID {
		t.Fatalf("created entry = %+v, want Mapper-bound nullable, JSON, array, and version fields", entry)
	}
	assertRawJSONEqual(t, entry.Metadata, string(blob.Metadata))
	assertRawJSONEqual(t, entry.AuthorizationMetadata, string(blob.AuthorizationMetadata))

	replacement := blob
	replacement.S3Key += "-replacement"
	replacement.S3ETag = "etag-mapper-2"
	replacement.S3VersionID = "version-mapper-2"
	replaced, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceUUID:     fixture.workspaceUUID,
		FilesystemUUID:    fixture.filesystem.UUID,
		Path:              "/mapper.txt",
		Blob:              replacement,
		OverwriteExisting: true,
	})
	if err != nil {
		t.Fatalf("overwrite PutFilestoreFile() error = %v", err)
	}
	if len(replaced.CleanupJobs) != 1 ||
		replaced.CleanupJobs[0].Key != blob.S3Key ||
		replaced.CleanupJobs[0].VersionID != blob.S3VersionID {
		t.Fatalf("overwrite cleanup jobs = %+v, want previous object version", replaced.CleanupJobs)
	}
}

func TestFilestoreObjectCleanupJobStopsAfterRepeatedExpiredLeases(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-expired-leases"))
	t.Cleanup(app.close)

	ctx := context.Background()
	_, _, organizationUUID, workspaceUUID, _, _, _, sessionUUID, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	filesystem, created, err := app.db.ProvisionFilestoreFilesystem(ctx, db.ProvisionFilestoreFilesystemInput{
		UUID:                uuid.NewString(),
		ExternalID:          "claude_chat_expired_leases_" + uuid.NewString(),
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		SessionUUID:         sessionUUID,
		CreatedByAPIKeyUUID: stringPointer(apiKeyUUID),
	})
	if err != nil || !created {
		t.Fatalf("ProvisionFilestoreFilesystem() = created %v, error %v", created, err)
	}
	job, err := app.db.EnqueueFilestoreObjectCleanupJob(ctx, db.EnqueueFilestoreObjectCleanupJobInput{
		WorkspaceUUID:   workspaceUUID,
		FilesystemUUID:  filesystem.UUID,
		EntryExternalID: "file_expired_leases",
		Bucket:          "filestore-expired-leases",
		Key:             "objects/expired-leases",
		Reason:          "expired_lease_test",
		RunAfter:        time.Now().UTC().Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("EnqueueFilestoreObjectCleanupJob() error = %v", err)
	}

	const maxLeaseAttempts = 2
	for attempt := 1; attempt <= maxLeaseAttempts; attempt++ {
		workerID := fmt.Sprintf("crashing-worker-%d", attempt)
		leasedJobs, leaseErr := app.db.LeaseFilestoreObjectCleanupJobs(ctx, workerID, 100, maxLeaseAttempts)
		if leaseErr != nil {
			t.Fatalf("lease attempt %d: %v", attempt, leaseErr)
		}
		var found bool
		for _, leasedJob := range leasedJobs {
			if leasedJob.UUID == job.UUID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("lease attempt %d returned %+v, want job %s", attempt, leasedJobs, job.UUID)
		}
		if _, err := app.db.Pool.Exec(ctx, `
			update jobs set locked_until = now() - interval '1 minute' where uuid = $1
		`, job.UUID); err != nil {
			t.Fatalf("expire lease attempt %d: %v", attempt, err)
		}
	}

	leasedJobs, err := app.db.LeaseFilestoreObjectCleanupJobs(ctx, "worker-after-cap", 100, maxLeaseAttempts)
	if err != nil {
		t.Fatalf("lease after retry cap: %v", err)
	}
	for _, leasedJob := range leasedJobs {
		if leasedJob.UUID == job.UUID {
			t.Fatalf("job %s was leased after %d expired leases", job.UUID, maxLeaseAttempts)
		}
	}

	var status, lastError string
	var hasLeaseAttempts bool
	if err := app.db.Pool.QueryRow(ctx, `
		select status, coalesce(payload->>'last_error', ''), payload ? 'lease_attempts'
		from jobs where uuid = $1
	`, job.UUID).Scan(&status, &lastError, &hasLeaseAttempts); err != nil {
		t.Fatalf("load exhausted cleanup job: %v", err)
	}
	if status != "failed" || lastError == "" || hasLeaseAttempts {
		t.Fatalf(
			"exhausted cleanup job = status %q, last error %q, has lease attempts %v",
			status,
			lastError,
			hasLeaseAttempts,
		)
	}
}

func TestFilestoreObjectCleanupJobMapperLifecycle(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-mapper-cleanup"))
	t.Cleanup(app.close)

	ctx := context.Background()
	_, _, organizationUUID, workspaceUUID, _, _, _, sessionUUID, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	filesystem, created, err := app.db.ProvisionFilestoreFilesystem(ctx, db.ProvisionFilestoreFilesystemInput{
		UUID:                uuid.NewString(),
		ExternalID:          "claude_chat_mapper_" + uuid.NewString(),
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		SessionUUID:         sessionUUID,
		CreatedByAPIKeyUUID: stringPointer(apiKeyUUID),
	})
	if err != nil || !created {
		t.Fatalf("ProvisionFilestoreFilesystem() = created %v, error %v", created, err)
	}
	job, err := app.db.EnqueueFilestoreObjectCleanupJob(ctx, db.EnqueueFilestoreObjectCleanupJobInput{
		WorkspaceUUID:   workspaceUUID,
		FilesystemUUID:  filesystem.UUID,
		EntryExternalID: "file_mapper",
		Bucket:          "filestore-mapper-cleanup",
		Key:             "objects/mapper",
		Reason:          "mapper_test",
		RunAfter:        time.Now().UTC().Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("EnqueueFilestoreObjectCleanupJob() error = %v", err)
	}
	if job.WorkspaceUUID != workspaceUUID || job.FilesystemUUID != filesystem.UUID {
		t.Fatalf("enqueued cleanup stable scope = workspace %q filesystem %q", job.WorkspaceUUID, job.FilesystemUUID)
	}
	var payloadWorkspaceUUID, payloadFilesystemUUID string
	var hasWorkspaceID, hasFilesystemID, hasFilesystemExternalID bool
	if err := app.db.Pool.QueryRow(ctx, `
		select payload->>'workspace_uuid', payload->>'filesystem_uuid',
			payload ? 'workspace_id', payload ? 'filesystem_id', payload ? 'filesystem_external_id'
		from jobs where uuid = $1
	`, job.UUID).Scan(
		&payloadWorkspaceUUID,
		&payloadFilesystemUUID,
		&hasWorkspaceID,
		&hasFilesystemID,
		&hasFilesystemExternalID,
	); err != nil {
		t.Fatalf("load cleanup job payload: %v", err)
	}
	if payloadWorkspaceUUID != workspaceUUID || payloadFilesystemUUID != filesystem.UUID ||
		hasWorkspaceID || hasFilesystemID || hasFilesystemExternalID {
		t.Fatalf("cleanup job payload scope = workspace %q filesystem %q internal keys %v/%v/%v",
			payloadWorkspaceUUID, payloadFilesystemUUID,
			hasWorkspaceID, hasFilesystemID, hasFilesystemExternalID)
	}
	if err := app.db.AttachFilestoreObjectCleanupJobVersion(
		ctx,
		workspaceUUID,
		job.ExternalID,
		"etag-mapper",
		"version-mapper",
	); err != nil {
		t.Fatalf("AttachFilestoreObjectCleanupJobVersion() error = %v", err)
	}

	const workerID = "filestore-mapper-worker"
	leasedJobs, err := app.db.LeaseFilestoreObjectCleanupJobs(ctx, workerID, 100, 10)
	if err != nil {
		t.Fatalf("LeaseFilestoreObjectCleanupJobs() error = %v", err)
	}
	var leased db.FilestoreObjectCleanupJob
	for _, candidate := range leasedJobs {
		if candidate.UUID == job.UUID {
			leased = candidate
			break
		}
	}
	if leased.UUID == "" {
		t.Fatalf("leased jobs = %+v, want job %s", leasedJobs, job.UUID)
	}
	if leased.ETag != "etag-mapper" || leased.VersionID != "version-mapper" ||
		leased.WorkspaceUUID != workspaceUUID ||
		leased.Bucket != "filestore-mapper-cleanup" {
		t.Fatalf("leased job = %+v, want mapped payload fields", leased)
	}
	t.Run("rejects a stale lease owner", func(t *testing.T) {
		err := app.db.CompleteLeasedFilestoreObjectCleanupJob(ctx, leased.UUID, "another-worker")
		if !errors.Is(err, db.ErrVersionConflict) {
			t.Fatalf("CompleteLeasedFilestoreObjectCleanupJob() error = %v, want ErrVersionConflict", err)
		}
	})

	t.Run("completes the current lease", func(t *testing.T) {
		if err := app.db.CompleteLeasedFilestoreObjectCleanupJob(ctx, leased.UUID, workerID); err != nil {
			t.Fatalf("CompleteLeasedFilestoreObjectCleanupJob() error = %v", err)
		}
	})
}

func TestConcurrentProvisionCreatesOneSessionFilesystem(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-session-concurrent"))
	t.Cleanup(app.close)
	_, _, organizationUUID, workspaceUUID, _, _, _, sessionUUID, _, apiKeyUUID := seedFilestoreLookupScope(t, app)

	type result struct {
		created bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for attempt := 1; attempt <= 2; attempt++ {
		workers.Add(1)
		go func(attempt int) {
			defer workers.Done()
			<-start
			_, created, err := app.db.ProvisionFilestoreFilesystem(context.Background(), db.ProvisionFilestoreFilesystemInput{
				UUID:                uuid.NewString(),
				ExternalID:          fmt.Sprintf("claude_chat_concurrent_%d_%s", attempt, uuid.NewString()),
				OrganizationUUID:    organizationUUID,
				WorkspaceUUID:       workspaceUUID,
				SessionUUID:         sessionUUID,
				CreatedByAPIKeyUUID: stringPointer(apiKeyUUID),
			})
			results <- result{created: created, err: err}
		}(attempt)
	}
	close(start)
	workers.Wait()
	close(results)

	var createdCount, duplicateCount int
	for outcome := range results {
		switch {
		case outcome.err == nil && outcome.created:
			createdCount++
		case errors.Is(outcome.err, db.ErrDuplicate) && !outcome.created:
			duplicateCount++
		default:
			t.Fatalf("concurrent provision result = created %v, error %v", outcome.created, outcome.err)
		}
	}
	if createdCount != 1 || duplicateCount != 1 {
		t.Fatalf("concurrent results = created %d, duplicate %d; want 1, 1", createdCount, duplicateCount)
	}

	var activeCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*) from filestore_filesystems
		where workspace_uuid = $1 and session_uuid = $2 and deleted_at is null
	`, workspaceUUID, sessionUUID).Scan(&activeCount); err != nil {
		t.Fatalf("count concurrent filesystems: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active concurrent filesystems = %d, want 1", activeCount)
	}
}

func TestDeleteSessionQueuesBoundedFilesystemCleanup(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-session-delete"))
	t.Cleanup(app.close)
	_, _, organizationUUID, workspaceUUID, _, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	input := filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID)
	created, _, _, _, err := app.db.CreateSession(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	filesystem, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceUUID, created.ExternalID)
	if err != nil {
		t.Fatalf("load filesystem: %v", err)
	}
	if _, err := app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
		WorkspaceUUID: workspaceUUID, FilesystemUUID: filesystem.UUID, Path: "/results",
	}); err != nil {
		t.Fatalf("make cleanup directory: %v", err)
	}
	if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceUUID: workspaceUUID, FilesystemUUID: filesystem.UUID, Path: "/results/output.txt",
		Blob: workspaceStorageBlob(7, nil),
	}); err != nil {
		t.Fatalf("put cleanup file: %v", err)
	}
	malformed, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceUUID: workspaceUUID, FilesystemUUID: filesystem.UUID, Path: "/results/malformed.txt",
		Blob: workspaceStorageBlob(5, nil),
	})
	if err != nil {
		t.Fatalf("put malformed cleanup candidate: %v", err)
	}
	deleteResult, err := app.db.Pool.Exec(context.Background(), `
		delete from files
		where uuid = (
				select file_uuid from session_resources where uuid = $1
		)
	`, malformed.Node.UUID)
	if err != nil {
		t.Fatalf("remove malformed cleanup backing File: %v", err)
	}
	if deleteResult.RowsAffected() != 1 {
		t.Fatalf("removed malformed cleanup backing Files = %d, want 1", deleteResult.RowsAffected())
	}
	cleanupSkillVersionUUID := seedFilestoreSkillVersion(
		t, app, workspaceUUID, apiKeyUUID, "cleanup-skill",
		"filestore-session-delete", "catalog/cleanup-skill.zip", 128, strings.Repeat("a", 64),
	)
	if err := app.db.ReplaceSessionSkillArchiveResources(context.Background(), workspaceUUID, created.ExternalID, []db.SessionSkillArchiveResourceInput{{
		Source:           "custom",
		SkillVersionUUID: cleanupSkillVersionUUID,
		Directory:        "cleanup-skill",
		S3Bucket:         "filestore-session-delete",
		S3Key:            "catalog/cleanup-skill.zip",
		SizeBytes:        128,
		SHA256:           strings.Repeat("a", 64),
	}}); err != nil {
		t.Fatalf("project cleanup skill: %v", err)
	}
	var cleanupSkillFileUUID string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select cast(file_uuid as text)
		from session_resources
		where workspace_uuid = $1 and session_uuid = $2
			and path = '/skills/cleanup-skill' and deleted_at is null
	`, workspaceUUID, filesystem.SessionUUID).Scan(&cleanupSkillFileUUID); err != nil {
		t.Fatalf("load cleanup Skill File snapshot: %v", err)
	}
	var entryOrganizationUUID, entryWorkspaceUUID, entryFilesystemUUID string
	var entryAPIKeyUUID, entrySessionUUID string
	var entryCodeSessionUUID *string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select resource.organization_uuid::text, resource.workspace_uuid::text, filesystem.uuid::text,
			api_key.uuid::text, resource.session_uuid::text, filesystem.code_session_uuid::text
		from session_resources resource
		join filestore_filesystems filesystem on filesystem.session_uuid = resource.session_uuid
			and filesystem.workspace_uuid = resource.workspace_uuid
		join files file on file.uuid = resource.file_uuid
		join api_keys api_key on api_key.uuid = file.created_by_api_key_uuid
		where resource.workspace_uuid = $1 and resource.session_uuid = $2
			and resource.path = '/results/output.txt'
	`, workspaceUUID, filesystem.SessionUUID).Scan(
		&entryOrganizationUUID, &entryWorkspaceUUID, &entryFilesystemUUID,
		&entryAPIKeyUUID, &entrySessionUUID, &entryCodeSessionUUID,
	); err != nil {
		t.Fatalf("load stable Filestore entry references: %v", err)
	}
	if entryOrganizationUUID != organizationUUID || entryWorkspaceUUID != workspaceUUID ||
		entryFilesystemUUID != filesystem.UUID || entryAPIKeyUUID != apiKeyUUID ||
		entrySessionUUID != created.UUID || entryCodeSessionUUID != nil {
		t.Fatalf("Filestore entry stable references = org %q workspace %q filesystem %q api-key %q session %q code-session %v",
			entryOrganizationUUID, entryWorkspaceUUID, entryFilesystemUUID,
			entryAPIKeyUUID, entrySessionUUID, entryCodeSessionUUID)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		update session_resources
		set payload = jsonb_build_object('type', 'file', 'visibility_test', true)
		where workspace_uuid = $1 and session_uuid = $2
			and path = '/results/output.txt'
			and file_ownership = 'owned'
	`, workspaceUUID, filesystem.SessionUUID); err != nil {
		t.Fatalf("make owned cleanup Resource publicly visible: %v", err)
	}

	if _, err := app.db.DeleteSession(context.Background(), workspaceUUID, created.ExternalID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceUUID, created.ExternalID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("deleted session filesystem lookup error = %v, want ErrNotFound", err)
	}
	var ownedAwaitingCleanup bool
	if err := app.db.Pool.QueryRow(context.Background(), `
		select deleted_at is null
		from session_resources
		where workspace_uuid = $1 and session_uuid = $2 and path = '/results/output.txt'
	`, workspaceUUID, filesystem.SessionUUID).Scan(&ownedAwaitingCleanup); err != nil {
		t.Fatalf("load owned Resource after Session delete: %v", err)
	}
	if !ownedAwaitingCleanup {
		t.Fatal("Session delete retired owned Resource before object cleanup")
	}
	var parentJobs, objectJobs int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select
			count(*) filter (where type = 'filestore_filesystem_cleanup'),
			count(*) filter (where type = 'filestore_object_cleanup')
		from jobs
		where payload->>'workspace_uuid' = $1
			and payload->>'filesystem_uuid' = $2
			and not (payload ? 'filesystem_id')
	`, workspaceUUID, filesystem.UUID).Scan(&parentJobs, &objectJobs); err != nil {
		t.Fatalf("count cleanup jobs: %v", err)
	}
	if parentJobs != 1 || objectJobs != 0 {
		t.Fatalf("cleanup jobs after Session delete = parent %d, object %d; want 1, 0", parentJobs, objectJobs)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		update jobs
		set run_after = '1900-01-01T00:00:00Z',
			created_at = '1900-01-01T00:00:00Z'
		where type = 'filestore_filesystem_cleanup'
			and payload->>'filesystem_uuid' = $1
	`, filesystem.UUID); err != nil {
		t.Fatalf("prioritize filesystem cleanup: %v", err)
	}

	jobs, err := app.db.LeaseFilestoreFilesystemCleanupJobs(context.Background(), "session-cleanup-worker", 1, 10)
	if err != nil {
		t.Fatalf("lease filesystem cleanup: %v", err)
	}
	if len(jobs) != 1 ||
		jobs[0].WorkspaceUUID != workspaceUUID ||
		jobs[0].FilesystemUUID != filesystem.UUID {
		t.Fatalf("leased filesystem cleanup jobs = %+v, want filesystem %s", jobs, filesystem.UUID)
	}
	done, anomalies, err := app.db.ProcessLeasedFilestoreFilesystemCleanupJob(context.Background(), jobs[0].UUID, "session-cleanup-worker", 100)
	if err != nil || !done {
		t.Fatalf("process filesystem cleanup = done %v, error %v", done, err)
	}
	if len(anomalies) != 1 || anomalies[0].EntryExternalID != malformed.Node.ExternalID {
		t.Fatalf("filesystem cleanup anomalies = %+v", anomalies)
	}
	var activeEntries, activeSkillArchiveEntries, cleanupObjects int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select
			(select count(*) from session_resources where session_uuid = $1 and deleted_at is null),
			(select count(*) from session_resources
				where session_uuid = $1
					and resource_type = 'skill_archive'
					and deleted_at is null),
			(select count(*) from jobs where type = 'filestore_object_cleanup'
				and payload->>'filesystem_uuid' = $2
				and payload->>'workspace_uuid' = $3
				and not (payload ? 'filesystem_id')
				and payload->>'reason' = 'session_deleted')
	`, filesystem.SessionUUID, filesystem.UUID, workspaceUUID).Scan(&activeEntries, &activeSkillArchiveEntries, &cleanupObjects); err != nil {
		t.Fatalf("load processed cleanup state: %v", err)
	}
	if activeEntries != 0 || activeSkillArchiveEntries != 0 || cleanupObjects != 1 {
		t.Fatalf(
			"processed cleanup = active entries %d, skill Skill Archive Resources %d, object jobs %d; want 0, 0, 1",
			activeEntries,
			activeSkillArchiveEntries,
			cleanupObjects,
		)
	}
	var cleanupSkillFileRetired bool
	if err := app.db.Pool.QueryRow(context.Background(), `
		select deleted_at is not null from files where uuid = $1
	`, cleanupSkillFileUUID).Scan(&cleanupSkillFileRetired); err != nil {
		t.Fatalf("load cleaned-up Skill File snapshot: %v", err)
	}
	if !cleanupSkillFileRetired {
		t.Fatal("cleaned-up Skill File snapshot is still active")
	}
	if _, _, err := app.db.ProcessLeasedFilestoreFilesystemCleanupJob(
		context.Background(),
		jobs[0].UUID,
		"session-cleanup-worker",
		100,
	); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatalf("reprocess completed filesystem cleanup error = %v, want ErrVersionConflict", err)
	}
	storageBytes, err := app.db.WorkspaceStorageBytes(context.Background(), workspaceUUID)
	if err != nil {
		t.Fatalf("load storage usage: %v", err)
	}
	if storageBytes != 0 {
		t.Fatalf("workspace storage bytes = %d, want 0", storageBytes)
	}
}

func TestFilestoreSkillArchivesUseResources(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-archive-entry"))
	t.Cleanup(app.close)
	_, _, organizationUUID, workspaceUUID, _, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	created, _, _, _, err := app.db.CreateSession(
		context.Background(),
		filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID),
	)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	filesystem, err := app.db.GetFilestoreFilesystemBySession(
		context.Background(),
		workspaceUUID,
		created.ExternalID,
	)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}

	var legacyTable *string
	if err := app.db.Pool.QueryRow(
		context.Background(),
		`select cast(to_regclass('filestore_skill_archives') as text)`,
	).Scan(&legacyTable); err != nil {
		t.Fatalf("check legacy archive table: %v", err)
	}
	if legacyTable != nil {
		t.Fatalf("legacy archive table still exists: %s", *legacyTable)
	}

	versionUUID := uuid.NewString()
	skillUUID := uuid.NewString()
	skillExternalID := "skill_" + uuid.NewString()
	if err := app.db.Pool.QueryRow(context.Background(), `
		insert into skills (
			uuid, external_id, workspace_uuid, source, display_title,
			latest_version, created_by_api_key_uuid
		)
		values ($1, $2, $3, 'custom', 'Demo', 'v1', $4)
		returning uuid::text
	`, skillUUID, skillExternalID, workspaceUUID, apiKeyUUID).Scan(&skillUUID); err != nil {
		t.Fatalf("insert archive test skill: %v", err)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into skill_versions (
			uuid, external_id, workspace_uuid, skill_uuid, skill_external_id,
			version, name, directory, s3_bucket, s3_key, size_bytes, sha256,
			created_by_api_key_uuid
		)
		values ($1, $2, $3, $4, $5, 'v1', 'Demo', 'demo',
			'filestore-archive-entry', 'catalog/demo.zip', 128, $6, $7)
	`, versionUUID, "skver_"+uuid.NewString(), workspaceUUID, skillUUID,
		skillExternalID, strings.Repeat("a", 64), apiKeyUUID); err != nil {
		t.Fatalf("insert archive test skill version: %v", err)
	}
	t.Cleanup(func() {
		_, _ = app.db.Pool.Exec(context.Background(), `delete from skill_versions where skill_uuid = $1`, skillUUID)
		_, _ = app.db.Pool.Exec(context.Background(), `delete from skills where uuid = $1`, skillUUID)
	})
	err = app.db.ReplaceSessionSkillArchiveResources(
		context.Background(),
		workspaceUUID,
		created.ExternalID,
		[]db.SessionSkillArchiveResourceInput{{
			Source:           "custom",
			SkillVersionUUID: versionUUID,
			Directory:        "demo",
			S3Bucket:         "filestore-archive-entry",
			S3Key:            "catalog/demo.zip",
			SizeBytes:        128,
			SHA256:           strings.Repeat("a", 64),
		}},
	)
	if err != nil {
		t.Fatalf("replace Skill Archive Resources: %v", err)
	}

	entries, err := app.db.ListSessionSkillArchiveResources(
		context.Background(),
		workspaceUUID,
		filesystem.UUID,
	)
	if err != nil {
		t.Fatalf("list Skill Archive Resources: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Skill Archive Resource count = %d, want 1", len(entries))
	}
	entry := entries[0]
	replacedEntryUUID := entry.ID
	var snapshotFileUUID string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select cast(resource.file_uuid as text)
		from session_resources resource
		join files file
			on file.uuid = resource.file_uuid
		where resource.id = $1
			and file.filename = 'demo.zip'
			and file.deleted_at is null
	`, replacedEntryUUID).Scan(&snapshotFileUUID); err != nil {
		t.Fatalf("load Skill Archive File snapshot: %v", err)
	}
	var metadata struct {
		SkillSource string `json:"skill_source"`
	}
	if err := json.Unmarshal(entry.Metadata, &metadata); err != nil {
		t.Fatalf("decode Skill Archive Resource metadata: %v", err)
	}
	if entry.Kind != db.SessionResourceFileKindArchive ||
		entry.Path != "/skills/demo" ||
		entry.ParentPath == nil ||
		*entry.ParentPath != "/skills" ||
		entry.FileOwnership != "" ||
		entry.S3Key == nil ||
		*entry.S3Key != "catalog/demo.zip" ||
		metadata.SkillSource != "custom" {
		t.Fatalf("Skill Archive Resource = %#v, metadata = %#v", entry, metadata)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		update skill_versions
		set s3_key = 'catalog/changed.zip', size_bytes = 256,
			sha256 = $2, deleted_at = now()
		where uuid = $1
	`, versionUUID, strings.Repeat("b", 64)); err != nil {
		t.Fatalf("change and soft delete snapshotted skill version: %v", err)
	}
	entries, err = app.db.ListSessionSkillArchiveResources(
		context.Background(),
		workspaceUUID,
		filesystem.UUID,
	)
	if err != nil {
		t.Fatalf("list archive Resources after catalog soft delete: %v", err)
	}
	if len(entries) != 1 || entries[0].S3Key == nil || *entries[0].S3Key != "catalog/demo.zip" {
		t.Fatalf("archive Resource lost object facts after catalog soft delete: %#v", entries)
	}

	page, err := app.db.ListSessionResourceFilesPage(context.Background(), db.ListSessionResourceFilesPageParams{
		WorkspaceUUID:  workspaceUUID,
		FilesystemUUID: filesystem.UUID,
		DirectoryPath:  "/",
		Recursive:      true,
		Limit:          100,
	})
	if err != nil {
		t.Fatalf("list ordinary entries: %v", err)
	}
	for _, listedEntry := range page.Entries {
		if listedEntry.Kind == db.SessionResourceFileKindArchive {
			t.Fatalf("ordinary entry listing exposed archive: %#v", listedEntry)
		}
	}
	storageBytes, err := app.db.WorkspaceStorageBytes(context.Background(), workspaceUUID)
	if err != nil {
		t.Fatalf("load workspace storage: %v", err)
	}
	if storageBytes != 0 {
		t.Fatalf("workspace storage bytes = %d, want 0", storageBytes)
	}

	if err := app.db.ReplaceSessionSkillArchiveResources(
		context.Background(),
		workspaceUUID,
		created.ExternalID,
		nil,
	); err != nil {
		t.Fatalf("clear Skill Archive Resources: %v", err)
	}
	entries, err = app.db.ListSessionSkillArchiveResources(
		context.Background(),
		workspaceUUID,
		filesystem.UUID,
	)
	if err != nil {
		t.Fatalf("list cleared Skill Archive Resources: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Skill Archive Resources after clear = %#v", entries)
	}
	var retiredAt *time.Time
	if err := app.db.Pool.QueryRow(context.Background(), `
		select deleted_at
		from session_resources
		where id = $1
	`, replacedEntryUUID).Scan(&retiredAt); err != nil {
		t.Fatalf("load retired Skill Archive Resource: %v", err)
	}
	if retiredAt == nil {
		t.Fatal("retired Skill Archive Resource deleted_at = nil")
	}
	var snapshotFileRetiredAt *time.Time
	if err := app.db.Pool.QueryRow(context.Background(), `
		select deleted_at from files where uuid = $1
	`, snapshotFileUUID).Scan(&snapshotFileRetiredAt); err != nil {
		t.Fatalf("load retired Skill Archive File snapshot: %v", err)
	}
	if snapshotFileRetiredAt == nil {
		t.Fatal("retired Skill Archive File deleted_at = nil")
	}
	skillsRoot, err := app.db.GetSessionResourceFile(
		context.Background(),
		workspaceUUID,
		filesystem.UUID,
		"/skills",
	)
	if err != nil {
		t.Fatalf("load /skills root: %v", err)
	}
	if skillsRoot.Kind != db.SessionResourceFileKindDirectory {
		t.Fatalf("/skills kind = %q, want directory", skillsRoot.Kind)
	}
}

func seedFilestoreSkillVersion(
	t *testing.T,
	app *testApp,
	workspaceUUID, apiKeyUUID string,
	directory, bucket, objectKey string,
	sizeBytes int64,
	checksum string,
) string {
	t.Helper()
	skillUUID := uuid.NewString()
	skillExternalID := "skill_" + uuid.NewString()
	if err := app.db.Pool.QueryRow(context.Background(), `
		insert into skills (
			uuid, external_id, workspace_uuid, source, display_title,
			latest_version, created_by_api_key_uuid
		)
		values ($1, $2, $3, 'custom', $4, 'v1', $5)
		returning uuid::text
	`, skillUUID, skillExternalID, workspaceUUID, directory, apiKeyUUID).Scan(&skillUUID); err != nil {
		t.Fatalf("insert Filestore skill: %v", err)
	}
	versionUUID := uuid.NewString()
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into skill_versions (
			uuid, external_id, workspace_uuid, skill_uuid, skill_external_id,
			version, name, directory, s3_bucket, s3_key, size_bytes, sha256,
			created_by_api_key_uuid
		)
		values ($1, $2, $3, $4, $5, 'v1', $6, $6, $7, $8, $9, $10, $11)
	`, versionUUID, "skver_"+uuid.NewString(), workspaceUUID, skillUUID, skillExternalID,
		directory, bucket, objectKey, sizeBytes, checksum, apiKeyUUID); err != nil {
		t.Fatalf("insert Filestore skill version: %v", err)
	}
	t.Cleanup(func() {
		_, _ = app.db.Pool.Exec(context.Background(), `delete from skill_versions where skill_uuid = $1`, skillUUID)
		_, _ = app.db.Pool.Exec(context.Background(), `delete from skills where uuid = $1`, skillUUID)
	})
	return versionUUID
}

func TestFilestoreFilesystemLookupPrefersExactExternalID(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-lookup-priority-bucket"))
	t.Cleanup(app.close)

	ctx := context.Background()
	_, _, organizationUUID, workspaceUUID, _, _, _, sessionUUID, codeSessionUUID, apiKeyUUID := seedFilestoreLookupScope(t, app)
	firstUUID := uuid.NewString()
	secondUUID := uuid.NewString()
	firstExternalID := "fs_" + uuid.NewString()
	secondExternalID := firstUUID
	secondSessionUUID := uuid.NewString()
	secondSessionExternalID := "session_filestore_lookup_second_" + uuid.NewString()
	now := time.Now().UTC()
	if _, err := app.db.Pool.Exec(ctx, `
		insert into sessions (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			environment_uuid, environment_external_id, agent_uuid, agent_external_id,
			agent_version, agent_snapshot, status
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, '{}'::jsonb, 'idle')
	`, secondSessionUUID, secondSessionExternalID, organizationUUID, workspaceUUID, apiKeyUUID,
		uuid.NewString(), "env_filestore_lookup_second",
		uuid.NewString(), "agent_filestore_lookup_second"); err != nil {
		t.Fatalf("insert second lookup session: %v", err)
	}

	for _, filesystem := range []struct {
		filesystemUUID  string
		externalID      string
		sessionUUID     string
		codeSessionUUID *string
	}{
		{filesystemUUID: firstUUID, externalID: firstExternalID, sessionUUID: sessionUUID, codeSessionUUID: stringPointer(codeSessionUUID)},
		{filesystemUUID: secondUUID, externalID: secondExternalID, sessionUUID: secondSessionUUID},
	} {
		if _, err := app.db.Pool.Exec(ctx, `
			insert into filestore_filesystems (
				uuid, external_id, organization_uuid, workspace_uuid,
				session_uuid, code_session_uuid, created_by_api_key_uuid, created_at, updated_at
			)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		`, filesystem.filesystemUUID, filesystem.externalID, organizationUUID, workspaceUUID,
			filesystem.sessionUUID, filesystem.codeSessionUUID, apiKeyUUID, now); err != nil {
			t.Fatalf("insert filesystem %q: %v", filesystem.externalID, err)
		}
	}

	assertExternalID := func(name string, filesystem db.FilestoreFilesystem, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s lookup: %v", name, err)
		}
		if filesystem.ExternalID != secondExternalID || filesystem.UUID != secondUUID {
			t.Fatalf("%s lookup = external %q uuid %q, want exact external %q uuid %q", name, filesystem.ExternalID, filesystem.UUID, secondExternalID, secondUUID)
		}
		if filesystem.OrganizationUUID != organizationUUID || filesystem.WorkspaceUUID != workspaceUUID ||
			filesystem.SessionUUID != secondSessionUUID || filesystem.CodeSessionUUID != nil ||
			filesystem.CreatedByAPIKeyUUID == nil || *filesystem.CreatedByAPIKeyUUID != apiKeyUUID {
			t.Fatalf("%s stable references = %#v", name, filesystem)
		}
	}

	filesystem, err := app.db.GetFilestoreFilesystem(ctx, workspaceUUID, secondExternalID)
	assertExternalID("workspace", filesystem, err)

	filesystem, err = app.db.GetFilestoreFilesystemBySession(ctx, workspaceUUID, secondSessionExternalID)
	assertExternalID("session", filesystem, err)

	filesystem, created, err := app.db.ProvisionFilestoreFilesystem(ctx, db.ProvisionFilestoreFilesystemInput{
		UUID:                uuid.NewString(),
		ExternalID:          secondExternalID,
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		SessionUUID:         secondSessionUUID,
		CreatedByAPIKeyUUID: stringPointer(apiKeyUUID),
		Now:                 now,
	})
	if created {
		t.Fatal("provision created a filesystem instead of selecting the exact external ID")
	}
	assertExternalID("provision", filesystem, err)
}

func seedFilestoreLookupScope(t *testing.T, app *testApp) (int64, int64, string, string, int64, int64, int64, string, string, string) {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()
	var organizationID, workspaceID, apiKeyID, sessionID, codeSessionID int64
	var organizationUUID, workspaceUUID, codeSessionUUID string
	t.Cleanup(func() {
		for _, cleanup := range []struct {
			statement string
			argument  any
		}{
			{statement: `delete from jobs where workspace_uuid = $1`, argument: workspaceUUID},
			{statement: `delete from session_resources where workspace_uuid = $1`, argument: workspaceUUID},
			{statement: `delete from filestore_filesystems where workspace_uuid = $1`, argument: workspaceUUID},
			{statement: `delete from files where workspace_uuid = $1`, argument: workspaceUUID},
			{statement: `delete from workspace_storage_usage where workspace_uuid = $1`, argument: workspaceUUID},
			{statement: `delete from environment_work where workspace_uuid = $1`, argument: workspaceUUID},
			{statement: `delete from code_sessions where uuid = $1`, argument: codeSessionUUID},
			{statement: `delete from sessions where workspace_uuid = $1`, argument: workspaceUUID},
			{statement: `delete from api_keys where id = $1`, argument: apiKeyID},
			{statement: `delete from workspaces where id = $1`, argument: workspaceID},
			{statement: `delete from organizations where id = $1`, argument: organizationID},
		} {
			if _, err := app.db.Pool.Exec(context.Background(), cleanup.statement, cleanup.argument); err != nil {
				t.Errorf("clean up Filestore lookup fixture: %v", err)
			}
		}
	})
	if err := app.db.Pool.QueryRow(ctx, `
		insert into organizations (name)
		values ($1)
		returning id, uuid::text
	`, "Filestore lookup "+suffix).Scan(&organizationID, &organizationUUID); err != nil {
		t.Fatalf("insert lookup organization: %v", err)
	}
	if err := app.db.Pool.QueryRow(ctx, `
		insert into workspaces (external_id, organization_uuid, name)
		select $1, uuid, $3
		from organizations
		where id = $2
		returning id, uuid::text
	`, "wrkspc_filestore_lookup_"+suffix, organizationID, "Filestore lookup "+suffix).Scan(&workspaceID, &workspaceUUID); err != nil {
		t.Fatalf("insert lookup workspace: %v", err)
	}
	apiKeyUUID := uuid.NewString()
	if err := app.db.Pool.QueryRow(ctx, `
		insert into api_keys (uuid, external_id, workspace_uuid, key_hash, status)
		values ($1, $2, $3, $4, 'active')
		returning id
	`, apiKeyUUID, "api_key_filestore_lookup_"+suffix, workspaceUUID, "hash_filestore_lookup_"+suffix).Scan(&apiKeyID); err != nil {
		t.Fatalf("insert lookup API key: %v", err)
	}
	sessionUUID := uuid.NewString()
	sessionExternalID := "session_filestore_lookup_" + suffix
	if err := app.db.Pool.QueryRow(ctx, `
		insert into sessions (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			environment_uuid, environment_external_id, agent_uuid, agent_external_id,
			agent_version, agent_snapshot, status
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, '{}'::jsonb, 'idle')
		returning id
	`, sessionUUID, sessionExternalID, organizationUUID, workspaceUUID, apiKeyUUID,
		uuid.NewString(), "env_filestore_lookup_"+suffix,
		uuid.NewString(), "agent_filestore_lookup_"+suffix).Scan(&sessionID); err != nil {
		t.Fatalf("insert lookup session: %v", err)
	}
	codeSessionUUID = uuid.NewString()
	codeSessionExternalID := "codesession_filestore_lookup_" + suffix
	if err := app.db.Pool.QueryRow(ctx, `
		insert into code_sessions (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
			session_external_id, environment_uuid, environment_external_id, status
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, 'active')
		returning id
	`, codeSessionUUID, codeSessionExternalID, organizationUUID, workspaceUUID, sessionUUID,
		sessionExternalID, uuid.NewString(), "env_filestore_lookup_"+suffix).Scan(&codeSessionID); err != nil {
		t.Fatalf("insert lookup code session: %v", err)
	}
	return organizationID, workspaceID, organizationUUID, workspaceUUID, apiKeyID, sessionID, codeSessionID, sessionUUID, codeSessionUUID, apiKeyUUID
}

func insertFilestoreCollisionOwner(
	t *testing.T,
	app *testApp,
	organizationUUID string,
	workspaceUUID string,
	apiKeyUUID string,
	filesystemExternalID string,
) {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	sessionUUID := uuid.NewString()
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into sessions (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			environment_uuid, environment_external_id, agent_uuid, agent_external_id,
			agent_version, agent_snapshot, status
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, '{}'::jsonb, 'idle')
	`, sessionUUID, "sesn_filestore_collision_"+suffix, organizationUUID, workspaceUUID, apiKeyUUID,
		uuid.NewString(), "env_filestore_collision_"+suffix,
		uuid.NewString(), "agent_filestore_collision_"+suffix); err != nil {
		t.Fatalf("insert collision owner Session: %v", err)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into filestore_filesystems (
			external_id, organization_uuid, workspace_uuid, session_uuid, created_by_api_key_uuid
		)
		values ($1, $2, $3, $4, $5)
	`, filesystemExternalID, organizationUUID, workspaceUUID, sessionUUID, apiKeyUUID); err != nil {
		t.Fatalf("insert collision filesystem %q: %v", filesystemExternalID, err)
	}
}

func filestoreRandomReader(values ...byte) *bytes.Reader {
	// ids.New 每次会多读少量字节，为 Base62 拒绝采样预留空间；
	// 测试源也必须按一次完整读取分块，才能让每次重试得到单一、可预测的候选值。
	const randomReadSize = 28
	randomBytes := make([]byte, 0, len(values)*randomReadSize)
	for _, value := range values {
		randomBytes = append(randomBytes, bytes.Repeat([]byte{value}, randomReadSize)...)
	}
	return bytes.NewReader(randomBytes)
}

func filestoreExternalIDForRandomByte(value byte) string {
	const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	return "claude_chat_" + strings.Repeat(string(base62[int(value)%len(base62)]), 24)
}

func stringPointer(value string) *string {
	return &value
}

func cleanupFilestoreSession(t *testing.T, app *testApp, workspaceUUID string, sessionExternalID string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := app.db.DeleteSession(context.Background(), workspaceUUID, sessionExternalID); err != nil &&
			!errors.Is(err, db.ErrNotFound) {
			t.Errorf("cleanup Filestore Session %s: %v", sessionExternalID, err)
		}
	})
}

func filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID string) db.CreateSessionInput {
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UTC()
	environmentExternalID := "env_filestore_session_" + suffix
	environmentUUID := uuid.NewString()
	agentUUID := uuid.NewString()
	sessionExternalID := "sesn_filestore_session_" + suffix
	sessionUUID := uuid.NewString()
	return db.CreateSessionInput{
		Session: db.Session{
			UUID:                  sessionUUID,
			ExternalID:            sessionExternalID,
			OrganizationUUID:      organizationUUID,
			WorkspaceUUID:         workspaceUUID,
			CreatedByAPIKeyUUID:   apiKeyUUID,
			EnvironmentUUID:       environmentUUID,
			EnvironmentExternalID: environmentExternalID,
			AgentUUID:             agentUUID,
			AgentExternalID:       "agent_filestore_session_" + suffix,
			AgentVersion:          1,
			AgentSnapshot:         json.RawMessage(`{}`),
			Metadata:              json.RawMessage(`{}`),
			VaultIDs:              json.RawMessage(`[]`),
			Status:                "idle",
			Usage:                 json.RawMessage(`{}`),
			Stats:                 json.RawMessage(`{}`),
			OutcomeEvaluations:    json.RawMessage(`[]`),
			CreatedAt:             now,
			UpdatedAt:             now,
		},
		Thread: db.SessionThread{
			UUID:              uuid.NewString(),
			ExternalID:        "sthr_filestore_session_" + suffix,
			OrganizationUUID:  organizationUUID,
			WorkspaceUUID:     workspaceUUID,
			SessionUUID:       sessionUUID,
			SessionExternalID: sessionExternalID,
			AgentSnapshot:     json.RawMessage(`{}`),
			Status:            "idle",
			Usage:             json.RawMessage(`{}`),
			Stats:             json.RawMessage(`{}`),
			CreatedAt:         now,
		},
		Work: db.EnvironmentWork{
			UUID:                  uuid.NewString(),
			ExternalID:            "work_filestore_session_" + suffix,
			OrganizationUUID:      organizationUUID,
			WorkspaceUUID:         workspaceUUID,
			EnvironmentUUID:       environmentUUID,
			EnvironmentExternalID: environmentExternalID,
			Data:                  json.RawMessage(`{"id":"` + sessionExternalID + `","type":"session"}`),
			Metadata:              json.RawMessage(`{}`),
			State:                 "queued",
			CreatedAt:             now,
		},
	}
}
