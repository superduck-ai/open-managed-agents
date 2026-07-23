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

func TestFilestoreEntryReferenceColumnsUseUUID(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-entry-schema"))
	t.Cleanup(app.close)

	expectedOrdinals := map[string]int{
		"organization_uuid":            4,
		"workspace_uuid":               5,
		"filesystem_uuid":              6,
		"created_by_api_key_uuid":      24,
		"created_by_session_uuid":      25,
		"created_by_code_session_uuid": 26,
	}
	rows, err := app.db.Pool.Query(context.Background(), `
		select column_name, data_type, ordinal_position
		from information_schema.columns
		where table_schema = current_schema()
			and table_name = 'filestore_entries'
			and column_name = any($1::text[])
	`, []string{
		"organization_uuid", "workspace_uuid", "filesystem_uuid",
		"created_by_api_key_uuid", "created_by_session_uuid", "created_by_code_session_uuid",
	})
	if err != nil {
		t.Fatalf("query Filestore entry UUID columns: %v", err)
	}
	defer rows.Close()
	seen := make(map[string]bool, len(expectedOrdinals))
	for rows.Next() {
		var name, dataType string
		var ordinal int
		if err := rows.Scan(&name, &dataType, &ordinal); err != nil {
			t.Fatalf("scan Filestore entry UUID column: %v", err)
		}
		if dataType != "uuid" || ordinal != expectedOrdinals[name] {
			t.Errorf("column %s = type %s ordinal %d, want uuid at %d", name, dataType, ordinal, expectedOrdinals[name])
		}
		seen[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate Filestore entry UUID columns: %v", err)
	}
	if len(seen) != len(expectedOrdinals) {
		t.Fatalf("found Filestore entry UUID columns %v, want %v", seen, expectedOrdinals)
	}

	var legacyColumnCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from information_schema.columns
		where table_schema = current_schema()
			and table_name = 'filestore_entries'
			and column_name = any($1::text[])
	`, []string{
		"organization_id", "workspace_id", "filesystem_id", "filesystem_external_id",
		"created_by_api_key_id", "created_by_session_id", "created_by_code_session_id",
	}).Scan(&legacyColumnCount); err != nil {
		t.Fatalf("query legacy Filestore entry columns: %v", err)
	}
	if legacyColumnCount != 0 {
		t.Fatalf("legacy Filestore entry reference columns = %d, want 0", legacyColumnCount)
	}
}

func TestCreateSessionRollsBackWhenFilesystemScopeIsInvalid(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-session-rollback"))
	t.Cleanup(app.close)
	organizationID, workspaceID, _, _, apiKeyID, _, _, _, _, _ := seedFilestoreLookupScope(t, app)
	input := filestoreSessionCreateInput(organizationID, workspaceID, apiKeyID+1_000_000)

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
	organizationID, workspaceID, organizationUUID, workspaceUUID, apiKeyID, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	for _, value := range []byte{0, 1, 2} {
		insertFilestoreCollisionOwner(
			t,
			app,
			organizationID,
			workspaceID,
			organizationUUID,
			workspaceUUID,
			apiKeyID,
			apiKeyUUID,
			filestoreExternalIDForRandomByte(value),
		)
	}
	input := filestoreSessionCreateInput(organizationID, workspaceID, apiKeyID)
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
	organizationID, workspaceID, organizationUUID, workspaceUUID, apiKeyID, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	insertFilestoreCollisionOwner(
		t,
		app,
		organizationID,
		workspaceID,
		organizationUUID,
		workspaceUUID,
		apiKeyID,
		apiKeyUUID,
		filestoreExternalIDForRandomByte(0),
	)
	input := filestoreSessionCreateInput(organizationID, workspaceID, apiKeyID)
	previousRandomReader := cryptorand.Reader
	cryptorand.Reader = filestoreRandomReader(0, 1)
	t.Cleanup(func() {
		cryptorand.Reader = previousRandomReader
	})

	created, _, _, _, err := app.db.CreateSession(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	filesystem, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceID, created.ExternalID)
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
	organizationID, workspaceID, organizationUUID, workspaceUUID, apiKeyID, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	input := filestoreSessionCreateInput(organizationID, workspaceID, apiKeyID)

	created, _, _, _, err := app.db.CreateSession(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	filesystem, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceID, created.ExternalID)
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
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := app.db.CreateCodeSession(context.Background(), db.CreateCodeSessionInput{
			ExternalID:            fmt.Sprintf("cse_filestore_retry_%d_%s", attempt, uuid.NewString()),
			OrganizationID:        organizationID,
			WorkspaceID:           workspaceID,
			SessionID:             created.ID,
			SessionExternalID:     created.ExternalID,
			EnvironmentID:         created.EnvironmentID,
			EnvironmentExternalID: created.EnvironmentExternalID,
			Status:                "active",
			Metadata:              json.RawMessage(`{}`),
			OAuthAccessTokenHash:  fmt.Sprintf("filestore-retry-hash-%d-%s", attempt, uuid.NewString()),
		}); err != nil {
			t.Fatalf("create Code Session attempt %d: %v", attempt, err)
		}
		reused, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceID, created.ExternalID)
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

func TestListFilestoreEntriesPageWithSQLX(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-sqlx-list"))
	t.Cleanup(app.close)

	ctx := context.Background()
	organizationID, workspaceID, _, _, apiKeyID, _, _, _, _, _ := seedFilestoreLookupScope(t, app)
	created, _, _, _, err := app.db.CreateSession(ctx, filestoreSessionCreateInput(organizationID, workspaceID, apiKeyID))
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	filesystem, err := app.db.GetFilestoreFilesystemBySession(ctx, workspaceID, created.ExternalID)
	if err != nil {
		t.Fatalf("GetFilestoreFilesystemBySession() error = %v", err)
	}

	t.Run("rejects a missing directory", func(t *testing.T) {
		_, err := app.db.ListFilestoreEntriesPage(ctx, db.ListFilestoreEntriesPageParams{
			WorkspaceID:   workspaceID,
			FilesystemID:  filesystem.ID,
			DirectoryPath: "/missing",
		})
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("ListFilestoreEntriesPage() error = %v, want ErrNotFound", err)
		}
	})

	for _, directoryPath := range []string{"/reports", "/reports/june", "/reports/july"} {
		if _, err := app.db.MakeFilestoreDirectory(ctx, db.MakeFilestoreDirectoryInput{
			WorkspaceID:  workspaceID,
			FilesystemID: filesystem.ID,
			Path:         directoryPath,
		}); err != nil {
			t.Fatalf("MakeFilestoreDirectory(%q) error = %v", directoryPath, err)
		}
	}

	t.Run("maps rows and advances a keyset cursor", func(t *testing.T) {
		directory, err := app.db.GetFilestoreEntry(ctx, workspaceID, filesystem.ID, "/reports")
		if err != nil {
			t.Fatalf("GetFilestoreEntry() error = %v", err)
		}
		if directory.Kind != db.FilestoreEntryKindDirectory || directory.Tags == nil {
			t.Fatalf("GetFilestoreEntry() = %+v, want mapped directory", directory)
		}

		firstPage, err := app.db.ListFilestoreEntriesPage(ctx, db.ListFilestoreEntriesPageParams{
			WorkspaceID:   workspaceID,
			FilesystemID:  filesystem.ID,
			DirectoryPath: "/reports",
			Limit:         1,
		})
		if err != nil {
			t.Fatalf("first ListFilestoreEntriesPage() error = %v", err)
		}
		if len(firstPage.Entries) != 1 || firstPage.Entries[0].Path != "/reports/july" || !firstPage.HasMore {
			t.Fatalf("first page = %+v, want /reports/july with HasMore", firstPage)
		}
		if firstPage.Entries[0].Tags == nil {
			t.Fatal("first page entry tags = nil, want an empty slice")
		}

		lastEntry := firstPage.Entries[0]
		secondPage, err := app.db.ListFilestoreEntriesPage(ctx, db.ListFilestoreEntriesPageParams{
			WorkspaceID:   workspaceID,
			FilesystemID:  filesystem.ID,
			DirectoryPath: "/reports",
			Limit:         1,
			Cursor:        &db.FilestoreEntryPageCursor{Path: lastEntry.Path, ID: lastEntry.ID},
		})
		if err != nil {
			t.Fatalf("second ListFilestoreEntriesPage() error = %v", err)
		}
		if len(secondPage.Entries) != 1 || secondPage.Entries[0].Path != "/reports/june" || secondPage.HasMore {
			t.Fatalf("second page = %+v, want /reports/june without HasMore", secondPage)
		}
	})
}

func TestFilestoreEntryMutationSQLXBinding(t *testing.T) {
	fixture := newWorkspaceStorageFixture(t)
	blob := workspaceStorageBlob(12, nil)
	blob.DetectedMimeType = "text/plain; charset=utf-8"
	blob.Metadata = json.RawMessage(`{"source":"sqlx"}`)
	blob.AuthorizationMetadata = json.RawMessage(`{"scope":"workspace"}`)
	blob.Tags = []string{"report", "sqlx"}
	blob.Downloadable = true
	blob.S3ETag = "etag-sqlx-1"
	blob.S3VersionID = "version-sqlx-1"

	created, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:  fixture.workspaceID,
		FilesystemID: fixture.filesystem.ID,
		Path:         "/sqlx.txt",
		Blob:         blob,
	})
	if err != nil {
		t.Fatalf("PutFilestoreFile() error = %v", err)
	}
	entry := created.Entry
	if entry.Path != "/sqlx.txt" ||
		entry.DetectedMimeType == nil || *entry.DetectedMimeType != blob.DetectedMimeType ||
		!reflect.DeepEqual(entry.Tags, blob.Tags) ||
		entry.S3VersionID == nil || *entry.S3VersionID != blob.S3VersionID {
		t.Fatalf("created entry = %+v, want SQLX-bound nullable, JSON, array, and version fields", entry)
	}
	assertRawJSONEqual(t, entry.Metadata, string(blob.Metadata))
	assertRawJSONEqual(t, entry.AuthorizationMetadata, string(blob.AuthorizationMetadata))

	replacement := blob
	replacement.S3Key += "-replacement"
	replacement.S3ETag = "etag-sqlx-2"
	replacement.S3VersionID = "version-sqlx-2"
	replaced, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID:       fixture.workspaceID,
		FilesystemID:      fixture.filesystem.ID,
		Path:              "/sqlx.txt",
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
	_, workspaceID, organizationUUID, workspaceUUID, _, _, _, sessionUUID, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
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
		WorkspaceID:     workspaceID,
		FilesystemID:    filesystem.ID,
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
			if leasedJob.ID == job.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("lease attempt %d returned %+v, want job %d", attempt, leasedJobs, job.ID)
		}
		if _, err := app.db.Pool.Exec(ctx, `
			update jobs set locked_until = now() - interval '1 minute' where id = $1
		`, job.ID); err != nil {
			t.Fatalf("expire lease attempt %d: %v", attempt, err)
		}
	}

	leasedJobs, err := app.db.LeaseFilestoreObjectCleanupJobs(ctx, "worker-after-cap", 100, maxLeaseAttempts)
	if err != nil {
		t.Fatalf("lease after retry cap: %v", err)
	}
	for _, leasedJob := range leasedJobs {
		if leasedJob.ID == job.ID {
			t.Fatalf("job %d was leased after %d expired leases", job.ID, maxLeaseAttempts)
		}
	}

	var status, lastError string
	var hasLeaseAttempts bool
	if err := app.db.Pool.QueryRow(ctx, `
		select status, coalesce(payload->>'last_error', ''), payload ? 'lease_attempts'
		from jobs where id = $1
	`, job.ID).Scan(&status, &lastError, &hasLeaseAttempts); err != nil {
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

func TestFilestoreObjectCleanupJobSQLXLifecycle(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-sqlx-cleanup"))
	t.Cleanup(app.close)

	ctx := context.Background()
	_, workspaceID, organizationUUID, workspaceUUID, _, _, _, sessionUUID, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	filesystem, created, err := app.db.ProvisionFilestoreFilesystem(ctx, db.ProvisionFilestoreFilesystemInput{
		UUID:                uuid.NewString(),
		ExternalID:          "claude_chat_sqlx_" + uuid.NewString(),
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		SessionUUID:         sessionUUID,
		CreatedByAPIKeyUUID: stringPointer(apiKeyUUID),
	})
	if err != nil || !created {
		t.Fatalf("ProvisionFilestoreFilesystem() = created %v, error %v", created, err)
	}
	job, err := app.db.EnqueueFilestoreObjectCleanupJob(ctx, db.EnqueueFilestoreObjectCleanupJobInput{
		WorkspaceID:     workspaceID,
		FilesystemID:    filesystem.ID,
		EntryExternalID: "file_sqlx",
		Bucket:          "filestore-sqlx-cleanup",
		Key:             "objects/sqlx",
		Reason:          "sqlx_test",
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
		from jobs where id = $1
	`, job.ID).Scan(
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
	_, staleWorkspaceID, _, _, _, _, _, _, _, _ := seedFilestoreLookupScope(t, app)
	if _, err := app.db.Pool.Exec(ctx, `update jobs set workspace_id = $1 where id = $2`, staleWorkspaceID, job.ID); err != nil {
		t.Fatalf("replace cleanup job workspace cache: %v", err)
	}
	if err := app.db.AttachFilestoreObjectCleanupJobVersion(
		ctx,
		workspaceID,
		job.ExternalID,
		"etag-sqlx",
		"version-sqlx",
	); err != nil {
		t.Fatalf("AttachFilestoreObjectCleanupJobVersion() error = %v", err)
	}

	const workerID = "filestore-sqlx-worker"
	leasedJobs, err := app.db.LeaseFilestoreObjectCleanupJobs(ctx, workerID, 100, 10)
	if err != nil {
		t.Fatalf("LeaseFilestoreObjectCleanupJobs() error = %v", err)
	}
	var leased db.FilestoreObjectCleanupJob
	for _, candidate := range leasedJobs {
		if candidate.ID == job.ID {
			leased = candidate
			break
		}
	}
	if leased.ID == 0 {
		t.Fatalf("leased jobs = %+v, want job %d", leasedJobs, job.ID)
	}
	if leased.ETag != "etag-sqlx" || leased.VersionID != "version-sqlx" ||
		leased.WorkspaceID != workspaceID || leased.WorkspaceUUID != workspaceUUID ||
		leased.FilesystemID != filesystem.ID || leased.FilesystemUUID != filesystem.UUID ||
		leased.Bucket != "filestore-sqlx-cleanup" {
		t.Fatalf("leased job = %+v, want mapped payload fields", leased)
	}
	var repairedWorkspaceID int64
	if err := app.db.Pool.QueryRow(ctx, `select workspace_id from jobs where id = $1`, job.ID).Scan(&repairedWorkspaceID); err != nil {
		t.Fatalf("load repaired cleanup job workspace cache: %v", err)
	}
	if repairedWorkspaceID != workspaceID {
		t.Fatalf("repaired cleanup job workspace_id = %d, want %d", repairedWorkspaceID, workspaceID)
	}

	t.Run("rejects a stale lease owner", func(t *testing.T) {
		err := app.db.CompleteLeasedFilestoreObjectCleanupJob(ctx, leased.ID, "another-worker")
		if !errors.Is(err, db.ErrVersionConflict) {
			t.Fatalf("CompleteLeasedFilestoreObjectCleanupJob() error = %v, want ErrVersionConflict", err)
		}
	})

	t.Run("completes the current lease", func(t *testing.T) {
		if err := app.db.CompleteLeasedFilestoreObjectCleanupJob(ctx, leased.ID, workerID); err != nil {
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
	organizationID, workspaceID, organizationUUID, workspaceUUID, apiKeyID, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)
	input := filestoreSessionCreateInput(organizationID, workspaceID, apiKeyID)
	created, _, _, _, err := app.db.CreateSession(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	filesystem, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceID, created.ExternalID)
	if err != nil {
		t.Fatalf("load filesystem: %v", err)
	}
	if _, err := app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
		WorkspaceID: workspaceID, FilesystemID: filesystem.ID, Path: "/results",
	}); err != nil {
		t.Fatalf("make cleanup directory: %v", err)
	}
	if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
		WorkspaceID: workspaceID, FilesystemID: filesystem.ID, Path: "/results/output.txt",
		Blob: workspaceStorageBlob(7, nil),
	}); err != nil {
		t.Fatalf("put cleanup file: %v", err)
	}
	var entryOrganizationUUID, entryWorkspaceUUID, entryFilesystemUUID string
	var entryAPIKeyUUID, entrySessionUUID string
	var entryCodeSessionUUID *string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select organization_uuid::text, workspace_uuid::text, filesystem_uuid::text,
			created_by_api_key_uuid::text, created_by_session_uuid::text,
			created_by_code_session_uuid::text
		from filestore_entries
		where workspace_uuid = $1 and filesystem_uuid = $2 and path = '/results/output.txt'
	`, workspaceUUID, filesystem.UUID).Scan(
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

	if _, err := app.db.DeleteSession(context.Background(), workspaceID, created.ExternalID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := app.db.GetFilestoreFilesystemBySession(context.Background(), workspaceID, created.ExternalID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("deleted session filesystem lookup error = %v, want ErrNotFound", err)
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
	_, staleWorkspaceID, _, _, _, _, _, _, _, _ := seedFilestoreLookupScope(t, app)
	if _, err := app.db.Pool.Exec(context.Background(), `
		update jobs
		set workspace_id = $1,
			run_after = '1900-01-01T00:00:00Z',
			created_at = '1900-01-01T00:00:00Z'
		where type = 'filestore_filesystem_cleanup'
			and payload->>'filesystem_uuid' = $2
	`, staleWorkspaceID, filesystem.UUID); err != nil {
		t.Fatalf("prioritize filesystem cleanup: %v", err)
	}

	jobs, err := app.db.LeaseFilestoreFilesystemCleanupJobs(context.Background(), "session-cleanup-worker", 1, 10)
	if err != nil {
		t.Fatalf("lease filesystem cleanup: %v", err)
	}
	if len(jobs) != 1 ||
		jobs[0].WorkspaceID != workspaceID ||
		jobs[0].WorkspaceUUID != workspaceUUID ||
		jobs[0].FilesystemID != filesystem.ID ||
		jobs[0].FilesystemUUID != filesystem.UUID {
		t.Fatalf("leased filesystem cleanup jobs = %+v, want filesystem %d", jobs, filesystem.ID)
	}
	done, err := app.db.ProcessLeasedFilestoreFilesystemCleanupJob(context.Background(), jobs[0].ID, "session-cleanup-worker", 100)
	if err != nil || !done {
		t.Fatalf("process filesystem cleanup = done %v, error %v", done, err)
	}
	var activeEntries, cleanupObjects int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select
			(select count(*) from filestore_entries where filesystem_uuid = $1 and deleted_at is null),
			(select count(*) from jobs where type = 'filestore_object_cleanup'
				and payload->>'filesystem_uuid' = $1::text
				and payload->>'workspace_uuid' = $2
				and not (payload ? 'filesystem_id')
				and payload->>'reason' = 'session_deleted')
	`, filesystem.UUID, workspaceUUID).Scan(&activeEntries, &cleanupObjects); err != nil {
		t.Fatalf("load processed cleanup state: %v", err)
	}
	if activeEntries != 0 || cleanupObjects != 1 {
		t.Fatalf("processed cleanup = active entries %d, object jobs %d; want 0, 1", activeEntries, cleanupObjects)
	}
	if _, err := app.db.ProcessLeasedFilestoreFilesystemCleanupJob(
		context.Background(),
		jobs[0].ID,
		"session-cleanup-worker",
		100,
	); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatalf("reprocess completed filesystem cleanup error = %v, want ErrVersionConflict", err)
	}
	storageBytes, err := app.db.WorkspaceStorageBytes(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("load storage usage: %v", err)
	}
	if storageBytes != 0 {
		t.Fatalf("workspace storage bytes = %d, want 0", storageBytes)
	}
}

func TestFilestoreFilesystemLookupPrefersExactExternalID(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-lookup-priority-bucket"))
	t.Cleanup(app.close)

	ctx := context.Background()
	organizationID, workspaceID, organizationUUID, workspaceUUID, apiKeyID, _, _, sessionUUID, codeSessionUUID, apiKeyUUID := seedFilestoreLookupScope(t, app)
	firstUUID := uuid.NewString()
	secondUUID := uuid.NewString()
	firstExternalID := "fs_" + uuid.NewString()
	secondExternalID := firstUUID
	secondSessionUUID := uuid.NewString()
	secondSessionExternalID := "session_filestore_lookup_second_" + uuid.NewString()
	now := time.Now().UTC()
	if _, err := app.db.Pool.Exec(ctx, `
		insert into sessions (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			environment_id, environment_external_id, agent_id, agent_external_id,
			agent_version, agent_snapshot, status
		)
		values ($1, $2, $3, $4, $5, 1, $6, 1, $7, 1, '{}'::jsonb, 'idle')
	`, secondSessionUUID, secondSessionExternalID, organizationID, workspaceID, apiKeyID,
		"env_filestore_lookup_second", "agent_filestore_lookup_second"); err != nil {
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

	filesystem, err := app.db.GetFilestoreFilesystem(ctx, workspaceID, secondExternalID)
	assertExternalID("workspace", filesystem, err)

	filesystem, err = app.db.GetFilestoreFilesystemBySession(ctx, workspaceID, secondSessionExternalID)
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
	var organizationUUID, workspaceUUID string
	t.Cleanup(func() {
		for _, cleanup := range []struct {
			statement string
			argument  any
		}{
			{statement: `delete from jobs where workspace_id = $1`, argument: workspaceID},
			{statement: `delete from filestore_entries where workspace_uuid = $1`, argument: workspaceUUID},
			{statement: `delete from filestore_filesystems where workspace_uuid = $1`, argument: workspaceUUID},
			{statement: `delete from files where workspace_id = $1`, argument: workspaceID},
			{statement: `delete from workspace_storage_usage where workspace_id = $1`, argument: workspaceID},
			{statement: `delete from code_sessions where id = $1`, argument: codeSessionID},
			{statement: `delete from sessions where workspace_id = $1`, argument: workspaceID},
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
		insert into organizations (external_id, name)
		values ($1, $2)
		returning id, uuid::text
	`, "org_filestore_lookup_"+suffix, "Filestore lookup "+suffix).Scan(&organizationID, &organizationUUID); err != nil {
		t.Fatalf("insert lookup organization: %v", err)
	}
	if err := app.db.Pool.QueryRow(ctx, `
		insert into workspaces (external_id, organization_id, name)
		values ($1, $2, $3)
		returning id, uuid::text
	`, "wrkspc_filestore_lookup_"+suffix, organizationID, "Filestore lookup "+suffix).Scan(&workspaceID, &workspaceUUID); err != nil {
		t.Fatalf("insert lookup workspace: %v", err)
	}
	apiKeyUUID := uuid.NewString()
	if err := app.db.Pool.QueryRow(ctx, `
		insert into api_keys (uuid, external_id, workspace_id, key_hash, status)
		values ($1, $2, $3, $4, 'active')
		returning id
	`, apiKeyUUID, "api_key_filestore_lookup_"+suffix, workspaceID, "hash_filestore_lookup_"+suffix).Scan(&apiKeyID); err != nil {
		t.Fatalf("insert lookup API key: %v", err)
	}
	sessionUUID := uuid.NewString()
	sessionExternalID := "session_filestore_lookup_" + suffix
	if err := app.db.Pool.QueryRow(ctx, `
		insert into sessions (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			environment_id, environment_external_id, agent_id, agent_external_id,
			agent_version, agent_snapshot, status
		)
		values ($1, $2, $3, $4, $5, 1, $6, 1, $7, 1, '{}'::jsonb, 'idle')
		returning id
	`, sessionUUID, sessionExternalID, organizationID, workspaceID, apiKeyID, "env_filestore_lookup_"+suffix, "agent_filestore_lookup_"+suffix).Scan(&sessionID); err != nil {
		t.Fatalf("insert lookup session: %v", err)
	}
	codeSessionUUID := uuid.NewString()
	codeSessionExternalID := "codesession_filestore_lookup_" + suffix
	if err := app.db.Pool.QueryRow(ctx, `
		insert into code_sessions (
			uuid, external_id, organization_id, workspace_id, session_id,
			session_external_id, environment_id, environment_external_id, status
		)
		values ($1, $2, $3, $4, $5, $6, 1, $7, 'active')
		returning id
	`, codeSessionUUID, codeSessionExternalID, organizationID, workspaceID, sessionID, sessionExternalID, "env_filestore_lookup_"+suffix).Scan(&codeSessionID); err != nil {
		t.Fatalf("insert lookup code session: %v", err)
	}
	return organizationID, workspaceID, organizationUUID, workspaceUUID, apiKeyID, sessionID, codeSessionID, sessionUUID, codeSessionUUID, apiKeyUUID
}

func insertFilestoreCollisionOwner(
	t *testing.T,
	app *testApp,
	organizationID int64,
	workspaceID int64,
	organizationUUID string,
	workspaceUUID string,
	apiKeyID int64,
	apiKeyUUID string,
	filesystemExternalID string,
) {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	sessionUUID := uuid.NewString()
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into sessions (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			environment_id, environment_external_id, agent_id, agent_external_id,
			agent_version, agent_snapshot, status
		)
		values ($1, $2, $3, $4, $5, 1, $6, 1, $7, 1, '{}'::jsonb, 'idle')
	`, sessionUUID, "sesn_filestore_collision_"+suffix, organizationID, workspaceID, apiKeyID,
		"env_filestore_collision_"+suffix, "agent_filestore_collision_"+suffix); err != nil {
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

func filestoreSessionCreateInput(organizationID, workspaceID, apiKeyID int64) db.CreateSessionInput {
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UTC()
	environmentExternalID := "env_filestore_session_" + suffix
	sessionExternalID := "sesn_filestore_session_" + suffix
	return db.CreateSessionInput{
		Session: db.Session{
			UUID:                  uuid.NewString(),
			ExternalID:            sessionExternalID,
			OrganizationID:        organizationID,
			WorkspaceID:           workspaceID,
			CreatedByAPIKeyID:     apiKeyID,
			EnvironmentID:         1,
			EnvironmentExternalID: environmentExternalID,
			AgentID:               1,
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
			UUID:           uuid.NewString(),
			ExternalID:     "sthr_filestore_session_" + suffix,
			OrganizationID: organizationID,
			WorkspaceID:    workspaceID,
			AgentSnapshot:  json.RawMessage(`{}`),
			Status:         "idle",
			Usage:          json.RawMessage(`{}`),
			Stats:          json.RawMessage(`{}`),
			CreatedAt:      now,
		},
		Work: db.EnvironmentWork{
			UUID:                  uuid.NewString(),
			ExternalID:            "work_filestore_session_" + suffix,
			OrganizationID:        organizationID,
			WorkspaceID:           workspaceID,
			EnvironmentID:         1,
			EnvironmentExternalID: environmentExternalID,
			Data:                  json.RawMessage(`{"id":"` + sessionExternalID + `","type":"session"}`),
			Metadata:              json.RawMessage(`{}`),
			State:                 "queued",
			CreatedAt:             now,
		},
	}
}
