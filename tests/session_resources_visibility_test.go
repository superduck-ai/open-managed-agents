package tests

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

type sessionResourceVisibilityFixture struct {
	app               *testApp
	workspaceUUID     string
	sessionExternalID string
	inputResourceID   string
	inputFileID       string
	outputResourceID  string
	outputFileID      string
	expiredResourceID string
	repositoryID      string
	directoryID       string
	archiveID         string
}

func seedSessionResourceVisibility(t *testing.T) sessionResourceVisibilityFixture {
	t.Helper()
	ctx := context.Background()
	app := newTestAppWithStore(t, nil, newFakeStore("session-resource-visibility"))
	t.Cleanup(app.close)
	_, _, organizationUUID, workspaceUUID, _, _, _, _, _, apiKeyUUID := seedFilestoreLookupScope(t, app)

	inputFileID := mustNewTestID(t, "file_")
	inputResourceID := mustNewTestID(t, "sesrsc_")
	repositoryID := mustNewTestID(t, "sesrsc_")
	scopeType := "session"
	input := filestoreSessionCreateInput(organizationUUID, workspaceUUID, apiKeyUUID)
	if err := app.db.CreateFile(ctx, db.FileRecord{
		UUID:                uuid.NewString(),
		ExternalID:          inputFileID,
		WorkspaceUUID:       workspaceUUID,
		Filename:            "input.csv",
		MimeType:            "text/csv",
		SizeBytes:           4,
		SHA256:              strings.Repeat("c", 64),
		S3Bucket:            app.store.Name(),
		S3Key:               "visibility/" + inputFileID,
		ScopeType:           &scopeType,
		ScopeID:             &input.Session.ExternalID,
		CreatedByAPIKeyUUID: apiKeyUUID,
		CreatedAt:           input.Session.CreatedAt,
	}); err != nil {
		t.Fatalf("create input file: %v", err)
	}
	input.Resources = []db.CreateSessionResourceInput{
		{
			Resource: newVisibilityResource(input, inputResourceID, db.SessionResourceTypeFile,
				`{"type":"file","file_id":"`+inputFileID+`","mount_path":"/uploads/data.csv"}`),
			FileMount: &db.SessionFileMount{
				ResourceExternalID: inputResourceID,
				FileExternalID:     inputFileID,
				Path:               "/uploads/data.csv",
			},
		},
		{
			Resource: newVisibilityResource(input, repositoryID, "github_repository",
				`{"type":"github_repository","url":"https://github.com/example/repo","mount_path":"/workspace/repo"}`),
		},
	}
	created, _, _, _, err := app.db.CreateSession(ctx, input)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	cleanupFilestoreSession(t, app, workspaceUUID, created.ExternalID)

	return sessionResourceVisibilityFixture{
		app:               app,
		workspaceUUID:     workspaceUUID,
		sessionExternalID: created.ExternalID,
		inputResourceID:   inputResourceID,
		inputFileID:       inputFileID,
		repositoryID:      repositoryID,
	}
}

func newVisibilityResource(
	input db.CreateSessionInput,
	externalID string,
	resourceType string,
	payload string,
) db.SessionResource {
	return db.SessionResource{
		UUID:              uuid.NewString(),
		ExternalID:        externalID,
		OrganizationUUID:  input.Session.OrganizationUUID,
		WorkspaceUUID:     input.Session.WorkspaceUUID,
		SessionExternalID: input.Session.ExternalID,
		ResourceType:      resourceType,
		Payload:           json.RawMessage(payload),
		SecretPayload:     json.RawMessage(`{}`),
		CreatedAt:         input.Session.CreatedAt,
		UpdatedAt:         input.Session.CreatedAt,
	}
}

func mustNewTestID(t *testing.T, prefix string) string {
	t.Helper()
	value, err := ids.New(prefix)
	if err != nil {
		t.Fatalf("new %s id: %v", prefix, err)
	}
	return value
}

func (f *sessionResourceVisibilityFixture) seedFilestoreEntries(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	filesystem, err := f.app.db.GetFilestoreFilesystemBySession(ctx, f.workspaceUUID, f.sessionExternalID)
	if err != nil {
		t.Fatalf("load Session filesystem: %v", err)
	}
	if _, err := f.app.db.MakeFilestoreDirectory(ctx, db.MakeFilestoreDirectoryInput{
		WorkspaceUUID:  f.workspaceUUID,
		FilesystemUUID: filesystem.UUID,
		Path:           "/outputs/reports",
	}); err != nil {
		t.Fatalf("make output directory: %v", err)
	}
	f.directoryID = f.resourceExternalIDByPath(t, "/outputs/reports")

	if _, err := f.app.db.PutFilestoreFile(ctx, db.PutFilestoreFileInput{
		WorkspaceUUID:  f.workspaceUUID,
		FilesystemUUID: filesystem.UUID,
		Path:           "/outputs/reports/summary.txt",
		Blob:           workspaceStorageBlob(9, nil),
	}); err != nil {
		t.Fatalf("put output file: %v", err)
	}
	f.outputResourceID = f.resourceExternalIDByPath(t, "/outputs/reports/summary.txt")
	f.outputFileID = f.fileExternalIDByPath(t, "/outputs/reports/summary.txt")

	expiresAt := time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := f.app.db.PutFilestoreFile(ctx, db.PutFilestoreFileInput{
		WorkspaceUUID:  f.workspaceUUID,
		FilesystemUUID: filesystem.UUID,
		Path:           "/outputs/reports/expired.txt",
		Blob:           workspaceStorageBlob(5, &expiresAt),
	}); err != nil {
		t.Fatalf("put expired output file: %v", err)
	}
	f.expiredResourceID = f.resourceExternalIDByPath(t, "/outputs/reports/expired.txt")

	if _, err := f.app.db.PutFilestoreFile(ctx, db.PutFilestoreFileInput{
		WorkspaceUUID:  f.workspaceUUID,
		FilesystemUUID: filesystem.UUID,
		Path:           "/tool_results/scratch.txt",
		Blob:           workspaceStorageBlob(3, nil),
	}); err != nil {
		t.Fatalf("put non-output owned file: %v", err)
	}

	f.seedSkillArchive(t)
}

func (f *sessionResourceVisibilityFixture) seedSkillArchive(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	skillUUID := uuid.NewString()
	skillExternalID := mustNewTestID(t, "skill_")
	versionUUID := uuid.NewString()
	if _, err := f.app.pool.Exec(ctx, `
		insert into skills (
			uuid, external_id, workspace_uuid, source, display_title,
			latest_version, created_by_api_key_uuid
		)
		select $1, $2, $3, 'custom', 'Visibility', 'v1', created_by_api_key_uuid
		from sessions where workspace_uuid = $3 and external_id = $4
	`, skillUUID, skillExternalID, f.workspaceUUID, f.sessionExternalID); err != nil {
		t.Fatalf("insert visibility skill: %v", err)
	}
	if _, err := f.app.pool.Exec(ctx, `
		insert into skill_versions (
			uuid, external_id, workspace_uuid, skill_uuid, skill_external_id,
			version, name, directory, s3_bucket, s3_key, size_bytes, sha256,
			created_by_api_key_uuid
		)
		select $1, $2, $3, $4, $5, 'v1', 'Visibility', 'visibility',
			'session-resource-visibility', 'catalog/visibility.zip', 128, $6,
			created_by_api_key_uuid
		from sessions where workspace_uuid = $3 and external_id = $7
	`, versionUUID, mustNewTestID(t, "skver_"), f.workspaceUUID, skillUUID,
		skillExternalID, strings.Repeat("a", 64), f.sessionExternalID); err != nil {
		t.Fatalf("insert visibility skill version: %v", err)
	}
	t.Cleanup(func() {
		_, _ = f.app.pool.Exec(context.Background(), `delete from skill_versions where skill_uuid = $1`, skillUUID)
		_, _ = f.app.pool.Exec(context.Background(), `delete from skills where uuid = $1`, skillUUID)
	})
	if err := f.app.db.ReplaceSessionSkillArchiveResources(
		ctx,
		f.workspaceUUID,
		f.sessionExternalID,
		[]db.SessionSkillArchiveResourceInput{{
			Source:           "custom",
			SkillVersionUUID: versionUUID,
			Directory:        "visibility",
			S3Bucket:         "session-resource-visibility",
			S3Key:            "catalog/visibility.zip",
			SizeBytes:        128,
			SHA256:           strings.Repeat("a", 64),
		}},
	); err != nil {
		t.Fatalf("replace Skill Archive Resources: %v", err)
	}
	f.archiveID = f.resourceExternalIDByPath(t, "/skills/visibility")
}

func (f *sessionResourceVisibilityFixture) resourceExternalIDByPath(t *testing.T, path string) string {
	t.Helper()
	var externalID string
	if err := f.app.pool.QueryRow(context.Background(), `
		select external_id
		from session_resources
		where workspace_uuid = $1 and session_external_id = $2
			and path = $3 and deleted_at is null
	`, f.workspaceUUID, f.sessionExternalID, path).Scan(&externalID); err != nil {
		t.Fatalf("load resource external id for %s: %v", path, err)
	}
	return externalID
}

func (f *sessionResourceVisibilityFixture) fileExternalIDByPath(t *testing.T, path string) string {
	t.Helper()
	var externalID string
	if err := f.app.pool.QueryRow(context.Background(), `
		select file.external_id
		from session_resources resource
		join files file on file.uuid = resource.file_uuid
		where resource.workspace_uuid = $1 and resource.session_external_id = $2
			and resource.path = $3 and resource.deleted_at is null
	`, f.workspaceUUID, f.sessionExternalID, path).Scan(&externalID); err != nil {
		t.Fatalf("load file external id for %s: %v", path, err)
	}
	return externalID
}

func (f *sessionResourceVisibilityFixture) listResources(t *testing.T) map[string]db.SessionResource {
	t.Helper()
	resources, err := f.app.db.ListSessionResources(context.Background(), f.workspaceUUID, f.sessionExternalID)
	if err != nil {
		t.Fatalf("ListSessionResources() error = %v", err)
	}
	indexed := make(map[string]db.SessionResource, len(resources))
	for _, resource := range resources {
		indexed[resource.ExternalID] = resource
	}
	if len(indexed) != len(resources) {
		t.Fatalf("ListSessionResources() returned duplicate external ids: %+v", resources)
	}
	return indexed
}

func TestSessionResourceVisibilityWithPostgres(t *testing.T) {
	fixture := seedSessionResourceVisibility(t)
	fixture.seedFilestoreEntries(t)

	t.Run("failure hides directory skill archive and expired output", func(t *testing.T) {
		resources := fixture.listResources(t)
		for _, hidden := range []struct {
			name       string
			externalID string
		}{
			{name: "output directory", externalID: fixture.directoryID},
			{name: "skill archive", externalID: fixture.archiveID},
			{name: "expired output", externalID: fixture.expiredResourceID},
		} {
			if resource, found := resources[hidden.externalID]; found {
				t.Fatalf("%s is visible: %+v", hidden.name, resource)
			}
		}
	})

	t.Run("failure hides output whose file row is gone", func(t *testing.T) {
		ctx := context.Background()
		orphanPath := "/outputs/reports/orphan.txt"
		filesystem, err := fixture.app.db.GetFilestoreFilesystemBySession(ctx, fixture.workspaceUUID, fixture.sessionExternalID)
		if err != nil {
			t.Fatalf("load Session filesystem: %v", err)
		}
		if _, err := fixture.app.db.PutFilestoreFile(ctx, db.PutFilestoreFileInput{
			WorkspaceUUID:  fixture.workspaceUUID,
			FilesystemUUID: filesystem.UUID,
			Path:           orphanPath,
			Blob:           workspaceStorageBlob(4, nil),
		}); err != nil {
			t.Fatalf("put orphan output file: %v", err)
		}
		orphanID := fixture.resourceExternalIDByPath(t, orphanPath)
		if resource, found := fixture.listResources(t)[orphanID]; !found {
			t.Fatalf("orphan candidate is not visible before the File row is retired: %+v", resource)
		}
		// 只退休 File 行，模拟两次独立查询之间 Owned File 被清理。
		if _, err := fixture.app.pool.Exec(ctx, `
			update files set deleted_at = now()
			where uuid = (
				select file_uuid from session_resources
				where workspace_uuid = $1 and session_external_id = $2 and path = $3
			)
		`, fixture.workspaceUUID, fixture.sessionExternalID, orphanPath); err != nil {
			t.Fatalf("retire orphan File row: %v", err)
		}
		if resource, found := fixture.listResources(t)[orphanID]; found {
			t.Fatalf("output without a File row is visible: %+v", resource)
		}
		if _, err := fixture.app.db.GetSessionResource(ctx, fixture.workspaceUUID, fixture.sessionExternalID, orphanID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("GetSessionResource(orphan) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("success scans output file_id and path", func(t *testing.T) {
		resource, found := fixture.listResources(t)[fixture.outputResourceID]
		if !found {
			t.Fatal("active output file is not visible")
		}
		if resource.Path != "/outputs/reports/summary.txt" {
			t.Fatalf("output resource path = %q, want /outputs/reports/summary.txt", resource.Path)
		}
		if resource.FileExternalID != fixture.outputFileID {
			t.Fatalf("output resource file external id = %q, want %q", resource.FileExternalID, fixture.outputFileID)
		}
		if len(resource.Payload) != 0 {
			t.Fatalf("output resource payload = %s, want empty", resource.Payload)
		}
	})

	t.Run("success keeps input and non file resources with nullable columns", func(t *testing.T) {
		resources := fixture.listResources(t)
		inputResource, found := resources[fixture.inputResourceID]
		if !found {
			t.Fatal("input file resource is not visible")
		}
		if inputResource.Path != "/uploads/data.csv" {
			t.Fatalf("input resource path = %q, want /uploads/data.csv", inputResource.Path)
		}
		if inputResource.FileExternalID != fixture.inputFileID {
			t.Fatalf("input resource file external id = %q, want %q", inputResource.FileExternalID, fixture.inputFileID)
		}
		repository, found := resources[fixture.repositoryID]
		if !found {
			t.Fatal("github_repository resource is not visible")
		}
		if repository.Path != "" || repository.FileExternalID != "" {
			t.Fatalf("github_repository resource = %+v, want empty path and file external id", repository)
		}
	})

	t.Run("success finds the output resource by external id", func(t *testing.T) {
		resource, err := fixture.app.db.GetSessionResource(
			context.Background(),
			fixture.workspaceUUID,
			fixture.sessionExternalID,
			fixture.outputResourceID,
		)
		if err != nil {
			t.Fatalf("GetSessionResource(output) error = %v", err)
		}
		if resource.FileExternalID != fixture.outputFileID || resource.Path != "/outputs/reports/summary.txt" {
			t.Fatalf("GetSessionResource(output) = %+v, want the output file mapping", resource)
		}
	})

	t.Run("success rejects hidden resources by external id", func(t *testing.T) {
		for _, hidden := range []struct {
			name       string
			externalID string
		}{
			{name: "output directory", externalID: fixture.directoryID},
			{name: "skill archive", externalID: fixture.archiveID},
			{name: "expired output", externalID: fixture.expiredResourceID},
		} {
			_, err := fixture.app.db.GetSessionResource(
				context.Background(),
				fixture.workspaceUUID,
				fixture.sessionExternalID,
				hidden.externalID,
			)
			if !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("GetSessionResource(%s) error = %v, want ErrNotFound", hidden.name, err)
			}
		}
	})
}

func TestSessionResourceListCapsOutputFiles(t *testing.T) {
	fixture := seedSessionResourceVisibility(t)
	ctx := context.Background()
	overflow := db.MaxSessionOutputFileResources + 1
	base := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	// 批量插入 Resource 与 Owned File 对，避开逐条 Filestore 写入的开销；created_at
	// 递增，第 0000 条最旧，用来断言截断保留的是最近的那一批。
	if _, err := fixture.app.pool.Exec(ctx, `
		with session_scope as (
			select uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid
			from sessions
			where workspace_uuid = $1 and external_id = $2
		),
		numbers as (select generate_series(0, $3::int - 1) as index),
		inserted_file as (
			insert into files (
				uuid, external_id, workspace_uuid, filename, mime_type, size_bytes,
				sha256, s3_bucket, s3_key, downloadable, scope_type, scope_id,
				created_by_api_key_uuid, created_at
			)
			select gen_random_uuid(), 'file_cap_' || lpad(numbers.index::text, 4, '0'),
				session_scope.workspace_uuid, 'cap-' || numbers.index || '.txt', 'text/plain', 1,
				repeat('d', 64), 'session-resource-visibility',
				'visibility/cap-' || numbers.index, true, 'session', session_scope.external_id,
				session_scope.created_by_api_key_uuid,
				$4::timestamptz + numbers.index * interval '1 second'
			from session_scope cross join numbers
			returning uuid, external_id
		)
		insert into session_resources (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
			session_external_id, resource_type, payload, secret_payload,
			path, parent_path, file_uuid, created_at, updated_at
		)
		select gen_random_uuid(),
			'sesrsc_cap_' || right(inserted_file.external_id, 4),
			session_scope.organization_uuid, session_scope.workspace_uuid, session_scope.uuid,
			session_scope.external_id, 'file', null, null,
			'/outputs/cap-' || right(inserted_file.external_id, 4) || '.txt', '/outputs',
			inserted_file.uuid,
			$4::timestamptz + right(inserted_file.external_id, 4)::int * interval '1 second',
			$4::timestamptz + right(inserted_file.external_id, 4)::int * interval '1 second'
		from inserted_file cross join session_scope
	`, fixture.workspaceUUID, fixture.sessionExternalID, overflow, base); err != nil {
		t.Fatalf("seed %d output files: %v", overflow, err)
	}

	resources := fixture.listResources(t)
	outputs := 0
	for _, resource := range resources {
		if len(resource.Payload) == 0 {
			outputs++
		}
	}
	if outputs != db.MaxSessionOutputFileResources {
		t.Fatalf("output resources = %d, want the %d cap", outputs, db.MaxSessionOutputFileResources)
	}
	if _, found := resources["sesrsc_cap_0000"]; found {
		t.Fatal("oldest output resource survived the cap, want the most recent kept")
	}
	if _, found := resources[fixture.inputResourceID]; !found {
		t.Fatal("input file resource was dropped by the output cap")
	}
	if _, found := resources[fixture.repositoryID]; !found {
		t.Fatal("github_repository resource was dropped by the output cap")
	}
}
