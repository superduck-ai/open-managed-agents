package tests

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestProvisionFilestoreFilesystemFixedRoots(t *testing.T) {
	t.Run("rejects an active file at a fixed root", func(t *testing.T) {
		app := newTestAppWithStore(t, nil, newFakeStore("filestore-provision-root-file"))
		t.Cleanup(app.close)
		_, workspaceID, organizationUUID, workspaceUUID, _, _, _, sessionUUID, codeSessionUUID, apiKeyUUID := seedFilestoreLookupScope(t, app)
		input := newFilestoreProvisionInput(
			organizationUUID,
			workspaceUUID,
			sessionUUID,
			codeSessionUUID,
			apiKeyUUID,
		)
		filesystem := insertFilestoreFilesystemWithoutRoots(t, app, workspaceID, input)
		if _, err := app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:  workspaceID,
			FilesystemID: filesystem.ID,
			Path:         "/uploads",
			Blob:         workspaceStorageBlob(1, nil),
		}); err != nil {
			t.Fatalf("PutFilestoreFile(/uploads) error = %v", err)
		}

		_, created, err := app.db.ProvisionFilestoreFilesystem(context.Background(), input)
		if !errors.Is(err, db.ErrFilestorePathExists) || created {
			t.Fatalf("ProvisionFilestoreFilesystem() = created %v, error %v; want active root file conflict", created, err)
		}
		if got := filestoreRootKinds(t, app, filesystem); !reflect.DeepEqual(got, map[string]string{
			"/uploads": db.FilestoreEntryKindFile,
		}) {
			t.Fatalf("roots after rejected provision = %v, want only the original /uploads file", got)
		}
	})

	t.Run("creates all roots with a new filesystem", func(t *testing.T) {
		app := newTestAppWithStore(t, nil, newFakeStore("filestore-provision-new-roots"))
		t.Cleanup(app.close)
		_, _, organizationUUID, workspaceUUID, _, _, _, sessionUUID, codeSessionUUID, apiKeyUUID := seedFilestoreLookupScope(t, app)
		input := newFilestoreProvisionInput(
			organizationUUID,
			workspaceUUID,
			sessionUUID,
			codeSessionUUID,
			apiKeyUUID,
		)

		filesystem, created, err := app.db.ProvisionFilestoreFilesystem(context.Background(), input)
		if err != nil || !created {
			t.Fatalf("ProvisionFilestoreFilesystem() = created %v, error %v", created, err)
		}
		assertFixedFilestoreRoots(t, app, filesystem)
	})

	t.Run("repairs missing roots on an existing filesystem", func(t *testing.T) {
		app := newTestAppWithStore(t, nil, newFakeStore("filestore-provision-repair-roots"))
		t.Cleanup(app.close)
		_, workspaceID, organizationUUID, workspaceUUID, _, _, _, sessionUUID, codeSessionUUID, apiKeyUUID := seedFilestoreLookupScope(t, app)
		input := newFilestoreProvisionInput(
			organizationUUID,
			workspaceUUID,
			sessionUUID,
			codeSessionUUID,
			apiKeyUUID,
		)
		inserted := insertFilestoreFilesystemWithoutRoots(t, app, workspaceID, input)

		filesystem, created, err := app.db.ProvisionFilestoreFilesystem(context.Background(), input)
		if err != nil || created {
			t.Fatalf("first idempotent ProvisionFilestoreFilesystem() = created %v, error %v", created, err)
		}
		if filesystem.ID != inserted.ID {
			t.Fatalf("reused filesystem ID = %d, want %d", filesystem.ID, inserted.ID)
		}
		assertFixedFilestoreRoots(t, app, filesystem)

		if _, err := app.db.Pool.Exec(context.Background(), `
			delete from filestore_entries
			where filesystem_uuid = $1 and path = '/transcripts'
		`, filesystem.UUID); err != nil {
			t.Fatalf("delete fixed root for repair test: %v", err)
		}
		repaired, created, err := app.db.ProvisionFilestoreFilesystem(context.Background(), input)
		if err != nil || created {
			t.Fatalf("second idempotent ProvisionFilestoreFilesystem() = created %v, error %v", created, err)
		}
		if repaired.ID != filesystem.ID {
			t.Fatalf("repaired filesystem ID = %d, want %d", repaired.ID, filesystem.ID)
		}
		assertFixedFilestoreRoots(t, app, repaired)
	})
}

func TestProvisionFilestoreFilesystemSerializesWithSessionDeletion(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("filestore-provision-delete-race"))
	t.Cleanup(app.close)
	_, workspaceID, organizationUUID, workspaceUUID, _, _, _, sessionUUID, codeSessionUUID, apiKeyUUID := seedFilestoreLookupScope(t, app)
	input := newFilestoreProvisionInput(
		organizationUUID,
		workspaceUUID,
		sessionUUID,
		codeSessionUUID,
		apiKeyUUID,
	)
	var sessionExternalID string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select external_id from sessions where uuid = $1
	`, sessionUUID).Scan(&sessionExternalID); err != nil {
		t.Fatalf("load Session external ID: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	blocker, err := app.db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace lock blocker: %v", err)
	}
	t.Cleanup(func() {
		_ = blocker.Rollback(context.Background())
	})
	if _, err := blocker.Exec(ctx, `select pg_advisory_xact_lock($1)`, workspaceID); err != nil {
		t.Fatalf("lock workspace: %v", err)
	}

	type provisionResult struct {
		filesystem db.FilestoreFilesystem
		created    bool
		err        error
	}
	provisioned := make(chan provisionResult, 1)
	go func() {
		filesystem, created, provisionErr := app.db.ProvisionFilestoreFilesystem(ctx, input)
		provisioned <- provisionResult{filesystem: filesystem, created: created, err: provisionErr}
	}()
	waitForAdvisoryLockWait(t, app)

	deleted := make(chan error, 1)
	go func() {
		_, deleteErr := app.db.DeleteSession(ctx, workspaceID, sessionExternalID)
		deleted <- deleteErr
	}()
	select {
	case deleteErr := <-deleted:
		t.Fatalf("DeleteSession() completed before Provision released its Session lock: %v", deleteErr)
	case <-time.After(200 * time.Millisecond):
	case <-ctx.Done():
		t.Fatalf("wait for blocked DeleteSession(): %v", ctx.Err())
	}

	if err := blocker.Rollback(ctx); err != nil {
		t.Fatalf("release workspace lock: %v", err)
	}
	var result provisionResult
	select {
	case result = <-provisioned:
	case <-ctx.Done():
		t.Fatalf("wait for ProvisionFilestoreFilesystem(): %v", ctx.Err())
	}
	if result.err != nil || !result.created {
		t.Fatalf("ProvisionFilestoreFilesystem() = created %v, error %v", result.created, result.err)
	}
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatalf("DeleteSession() error = %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("wait for DeleteSession(): %v", ctx.Err())
	}

	var activeFilesystems, retiredFilesystems int
	if err := app.db.Pool.QueryRow(ctx, `
		select
			count(*) filter (where deleted_at is null),
			count(*) filter (where deleted_at is not null)
		from filestore_filesystems
		where workspace_uuid = $1 and session_uuid = $2
	`, workspaceUUID, sessionUUID).Scan(&activeFilesystems, &retiredFilesystems); err != nil {
		t.Fatalf("count filesystems after Session deletion: %v", err)
	}
	if activeFilesystems != 0 || retiredFilesystems != 1 {
		t.Fatalf(
			"filesystems after Session deletion = active %d, retired %d; want active 0, retired 1",
			activeFilesystems,
			retiredFilesystems,
		)
	}
	if result.filesystem.SessionUUID != sessionUUID {
		t.Fatalf("provisioned filesystem Session UUID = %q, want %q", result.filesystem.SessionUUID, sessionUUID)
	}
}

func waitForAdvisoryLockWait(t *testing.T, app *testApp) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := app.db.Pool.QueryRow(context.Background(), `
			select exists (
				select 1
				from pg_stat_activity
				where pid <> pg_backend_pid()
					and datname = current_database()
					and wait_event_type = 'Lock'
					and wait_event = 'advisory'
					and query like '%pg_advisory_xact_lock%'
			)
		`).Scan(&waiting); err != nil {
			t.Fatalf("inspect advisory lock wait: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ProvisionFilestoreFilesystem() did not block on the workspace advisory lock")
}

func newFilestoreProvisionInput(
	organizationUUID string,
	workspaceUUID string,
	sessionUUID string,
	codeSessionUUID string,
	apiKeyUUID string,
) db.ProvisionFilestoreFilesystemInput {
	return db.ProvisionFilestoreFilesystemInput{
		UUID:                uuid.NewString(),
		ExternalID:          "claude_chat_roots_" + uuid.NewString(),
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		SessionUUID:         sessionUUID,
		CodeSessionUUID:     stringPointer(codeSessionUUID),
		CreatedByAPIKeyUUID: stringPointer(apiKeyUUID),
		Now:                 time.Now().UTC(),
	}
}

func insertFilestoreFilesystemWithoutRoots(
	t *testing.T,
	app *testApp,
	workspaceID int64,
	input db.ProvisionFilestoreFilesystemInput,
) db.FilestoreFilesystem {
	t.Helper()
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into filestore_filesystems (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
			code_session_uuid, created_by_api_key_uuid, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $8)
	`, input.UUID, input.ExternalID, input.OrganizationUUID, input.WorkspaceUUID,
		input.SessionUUID, input.CodeSessionUUID, input.CreatedByAPIKeyUUID, input.Now); err != nil {
		t.Fatalf("insert filesystem without roots: %v", err)
	}
	filesystem, err := app.db.GetFilestoreFilesystem(context.Background(), workspaceID, input.ExternalID)
	if err != nil {
		t.Fatalf("GetFilestoreFilesystem() error = %v", err)
	}
	return filesystem
}

func assertFixedFilestoreRoots(t *testing.T, app *testApp, filesystem db.FilestoreFilesystem) {
	t.Helper()
	got := filestoreRootKinds(t, app, filesystem)
	want := map[string]string{
		"/outputs":      db.FilestoreEntryKindDirectory,
		"/uploads":      db.FilestoreEntryKindDirectory,
		"/transcripts":  db.FilestoreEntryKindDirectory,
		"/tool_results": db.FilestoreEntryKindDirectory,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fixed Filestore roots = %v, want %v", got, want)
	}
}

func filestoreRootKinds(t *testing.T, app *testApp, filesystem db.FilestoreFilesystem) map[string]string {
	t.Helper()
	rows, err := app.db.Pool.Query(context.Background(), `
		select path, kind
		from filestore_entries
		where filesystem_uuid = $1
			and parent_path = '/'
			and deleted_at is null
		order by path
	`, filesystem.UUID)
	if err != nil {
		t.Fatalf("list fixed Filestore roots: %v", err)
	}
	defer rows.Close()

	roots := make(map[string]string)
	for rows.Next() {
		var path, kind string
		if err := rows.Scan(&path, &kind); err != nil {
			t.Fatalf("scan fixed Filestore root: %v", err)
		}
		roots[path] = kind
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate fixed Filestore roots: %v", err)
	}
	return roots
}
