package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type memoryMapperContract struct {
	statement      yourbatis.Statement
	bound          yourbatis.BoundSQL
	id             string
	kind           yourbatis.StatementKind
	argumentNames  []string
	fragments      []string
	sensitiveNames []string
}

func TestMemoryStoreMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	insertParams := insertMemoryStoreParams{
		UUID: "store-uuid", ExternalID: "store-id", OrganizationUUID: "org-uuid",
		WorkspaceUUID: "workspace-uuid", CreatedByAPIKeyUUID: nullableString("key-uuid"),
		Name: "store", Description: "description", Metadata: []byte(`{"key":"value"}`), CreatedAt: now,
	}
	updateParams := updateMemoryStoreParams{
		WorkspaceUUID: "workspace-uuid", ExternalID: "store-id", Name: "store",
		Description: "description", Metadata: []byte(`{}`), UpdatedAt: now,
	}
	listParams := listMemoryStoresParams{
		WorkspaceUUID: "workspace-uuid", Limit: 21, HasCreatedAtGTE: true,
		CreatedAtGTE: now.Add(-time.Hour), HasCreatedAtLTE: true, CreatedAtLTE: now,
		HasCursor: true, CursorCreatedAt: now, CursorUUID: "cursor-uuid",
	}
	tests := []memoryMapperContract{
		{
			statement: memoryStoreMapperInsertStatement, bound: buildMemoryStoreMapperInsert(yourbatis.DialectPostgres, insertParams),
			id: "MemoryStoreMapper.Insert", kind: yourbatis.StatementInsert,
			argumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.CreatedByAPIKeyUUID", "params.Name", "params.Description", "params.Metadata",
				"params.CreatedAt", "params.CreatedAt",
			},
			fragments:      []string{"INSERT INTO memory_stores", "CAST($8 AS jsonb)", "RETURNING"},
			sensitiveNames: []string{"params.Metadata"},
		},
		{
			statement: memoryStoreMapperFindByExternalIDStatement,
			bound:     buildMemoryStoreMapperFindByExternalID(yourbatis.DialectPostgres, "workspace-uuid", "store-id"),
			id:        "MemoryStoreMapper.FindByExternalID", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "externalID"},
			fragments:     []string{"FROM memory_stores", "workspace_uuid = $1", "external_id = $2", "deleted_at IS NULL"},
		},
		{
			statement: memoryStoreMapperFindByOrganizationAndExternalIDStatement,
			bound:     buildMemoryStoreMapperFindByOrganizationAndExternalID(yourbatis.DialectPostgres, "org-uuid", "store-id"),
			id:        "MemoryStoreMapper.FindByOrganizationAndExternalID", kind: yourbatis.StatementSelect,
			argumentNames: []string{"organizationUUID", "externalID"},
			fragments:     []string{"FROM memory_stores", "organization_uuid = $1", "external_id = $2", "deleted_at IS NULL"},
		},
		{
			statement: memoryStoreMapperFindForUpdateStatement,
			bound:     buildMemoryStoreMapperFindForUpdate(yourbatis.DialectPostgres, "workspace-uuid", "store-id"),
			id:        "MemoryStoreMapper.FindForUpdate", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "externalID"},
			fragments:     []string{"workspace_uuid = $1", "external_id = $2", "FOR UPDATE"},
		},
		{
			statement: memoryStoreMapperUpdateByExternalIDStatement,
			bound:     buildMemoryStoreMapperUpdateByExternalID(yourbatis.DialectPostgres, updateParams),
			id:        "MemoryStoreMapper.UpdateByExternalID", kind: yourbatis.StatementUpdate,
			argumentNames: []string{
				"params.Name", "params.Description", "params.Metadata", "params.UpdatedAt",
				"params.WorkspaceUUID", "params.ExternalID",
			},
			fragments:      []string{"UPDATE memory_stores", "metadata = CAST($3 AS jsonb)", "workspace_uuid = $5", "RETURNING"},
			sensitiveNames: []string{"params.Metadata"},
		},
		{
			statement: memoryStoreMapperArchiveByExternalIDStatement,
			bound:     buildMemoryStoreMapperArchiveByExternalID(yourbatis.DialectPostgres, "workspace-uuid", "store-id"),
			id:        "MemoryStoreMapper.ArchiveByExternalID", kind: yourbatis.StatementUpdate,
			argumentNames: []string{"workspaceUUID", "externalID"},
			fragments:     []string{"UPDATE memory_stores", "COALESCE(archived_at, NOW())", "workspace_uuid = $1", "RETURNING"},
		},
		{
			statement: memoryStoreMapperFindUUIDForUpdateStatement,
			bound:     buildMemoryStoreMapperFindUUIDForUpdate(yourbatis.DialectPostgres, "workspace-uuid", "store-id"),
			id:        "MemoryStoreMapper.FindUUIDForUpdate", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "externalID"},
			fragments:     []string{"SELECT uuid", "workspace_uuid = $1", "FOR UPDATE"},
		},
		{
			statement: memoryStoreMapperDeleteByUUIDStatement,
			bound:     buildMemoryStoreMapperDeleteByUUID(yourbatis.DialectPostgres, "workspace-uuid", "store-uuid"),
			id:        "MemoryStoreMapper.DeleteByUUID", kind: yourbatis.StatementDelete,
			argumentNames: []string{"workspaceUUID", "storeUUID"},
			fragments:     []string{"DELETE FROM memory_stores", "workspace_uuid = $1", "uuid = $2"},
		},
		{
			statement: memoryStoreMapperListPageStatement,
			bound:     buildMemoryStoreMapperListPage(yourbatis.DialectPostgres, listParams),
			id:        "MemoryStoreMapper.ListPage", kind: yourbatis.StatementSelect,
			argumentNames: []string{
				"params.WorkspaceUUID", "params.CreatedAtGTE", "params.CreatedAtLTE",
				"params.CursorCreatedAt", "params.CursorCreatedAt", "params.CursorUUID", "params.Limit",
			},
			fragments: []string{"archived_at IS NULL", "created_at >= $2", "created_at <= $3", "uuid < $6", "LIMIT $7"},
		},
		{
			statement: memoryStoreMapperExistsStatement,
			bound:     buildMemoryStoreMapperExists(yourbatis.DialectPostgres, "workspace-uuid", "store-id"),
			id:        "MemoryStoreMapper.Exists", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "externalID"},
			fragments:     []string{"SELECT EXISTS", "workspace_uuid = $1", "external_id = $2"},
		},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			assertMemoryMapperContract(t, test)
		})
	}

	t.Run("include archived", func(t *testing.T) {
		listParams.IncludeArchived = true
		bound := buildMemoryStoreMapperListPage(yourbatis.DialectPostgres, listParams)
		if strings.Contains(bound.SQL, "archived_at IS NULL") {
			t.Fatalf("ListPage() includes archived filter: %q", bound.SQL)
		}
	})
}

func TestMemoryMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	insertParams := insertMemoryParams{
		UUID: "memory-uuid", ExternalID: "memory-id", OrganizationUUID: "org-uuid",
		WorkspaceUUID: "workspace-uuid", MemoryStoreUUID: "store-uuid",
		MemoryStoreExternalID: "store-id", CurrentVersionExternalID: "version-id",
		Path: "/test", ContentSizeBytes: 4, ContentSHA256: "sha", S3Bucket: "bucket",
		S3Key: "key", CreatedAt: now,
	}
	updateCurrentParams := updateMemoryCurrentVersionParams{
		WorkspaceUUID: "workspace-uuid", MemoryUUID: "memory-uuid",
		VersionUUID: "version-uuid", VersionExternalID: "version-id",
	}
	updateParams := updateMemoryParams{
		WorkspaceUUID: "workspace-uuid", MemoryStoreExternalID: "store-id",
		MemoryExternalID: "memory-id", VersionUUID: "version-uuid",
		VersionExternalID: "version-id", Path: "/test", ContentSizeBytes: 4,
		ContentSHA256: "sha", S3Bucket: "bucket", S3Key: "key", UpdatedAt: now,
	}
	deleteParams := deleteMemoryParams{
		WorkspaceUUID: "workspace-uuid", MemoryStoreExternalID: "store-id",
		MemoryExternalID: "memory-id", VersionUUID: "version-uuid",
		VersionExternalID: "version-id", UpdatedAt: now,
	}
	listParams := listMemoriesParams{
		WorkspaceUUID: "workspace-uuid", MemoryStoreExternalID: "store-id", Limit: 21,
		PathPrefix: "/test", HasCursor: true, CursorCreatedAt: now,
		CursorUpdatedAt: now, CursorPath: "/test/a", CursorUUID: "cursor-uuid",
		OrderBy: "created_at", Descending: true,
	}
	depthParams := listMemoriesForDepthParams{
		WorkspaceUUID: "workspace-uuid", MemoryStoreExternalID: "store-id", PathPrefix: "/test",
	}
	tests := []memoryMapperContract{
		{
			statement: memoryMapperInsertStatement, bound: buildMemoryMapperInsert(yourbatis.DialectPostgres, insertParams),
			id: "MemoryMapper.Insert", kind: yourbatis.StatementInsert,
			argumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.MemoryStoreUUID", "params.MemoryStoreExternalID", "params.CurrentVersionExternalID",
				"params.Path", "params.ContentSizeBytes", "params.ContentSHA256", "params.S3Bucket",
				"params.S3Key", "params.CreatedAt", "params.CreatedAt",
			},
			fragments: []string{"INSERT INTO memories", "RETURNING"},
		},
		{
			statement: memoryMapperFindByExternalIDStatement,
			bound:     buildMemoryMapperFindByExternalID(yourbatis.DialectPostgres, "workspace-uuid", "store-id", "memory-id"),
			id:        "MemoryMapper.FindByExternalID", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "memoryStoreExternalID", "memoryExternalID"},
			fragments:     []string{"FROM memories", "workspace_uuid = $1", "memory_store_external_id = $2", "external_id = $3"},
		},
		{
			statement: memoryMapperFindForUpdateStatement,
			bound:     buildMemoryMapperFindForUpdate(yourbatis.DialectPostgres, "workspace-uuid", "store-id", "memory-id"),
			id:        "MemoryMapper.FindForUpdate", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "memoryStoreExternalID", "memoryExternalID"},
			fragments:     []string{"workspace_uuid = $1", "memory_store_external_id = $2", "FOR UPDATE"},
		},
		{
			statement: memoryMapperUpdateCurrentVersionStatement,
			bound:     buildMemoryMapperUpdateCurrentVersion(yourbatis.DialectPostgres, updateCurrentParams),
			id:        "MemoryMapper.UpdateCurrentVersion", kind: yourbatis.StatementUpdate,
			argumentNames: []string{"params.VersionUUID", "params.VersionExternalID", "params.WorkspaceUUID", "params.MemoryUUID"},
			fragments:     []string{"UPDATE memories", "current_version_uuid = $1", "workspace_uuid = $3", "RETURNING"},
		},
		{
			statement: memoryMapperUpdateByExternalIDStatement,
			bound:     buildMemoryMapperUpdateByExternalID(yourbatis.DialectPostgres, updateParams),
			id:        "MemoryMapper.UpdateByExternalID", kind: yourbatis.StatementUpdate,
			argumentNames: []string{
				"params.VersionUUID", "params.VersionExternalID", "params.Path", "params.ContentSizeBytes",
				"params.ContentSHA256", "params.S3Bucket", "params.S3Key", "params.UpdatedAt",
				"params.WorkspaceUUID", "params.MemoryStoreExternalID", "params.MemoryExternalID",
			},
			fragments: []string{"UPDATE memories", "path = $3", "workspace_uuid = $9", "RETURNING"},
		},
		{
			statement: memoryMapperSoftDeleteByExternalIDStatement,
			bound:     buildMemoryMapperSoftDeleteByExternalID(yourbatis.DialectPostgres, deleteParams),
			id:        "MemoryMapper.SoftDeleteByExternalID", kind: yourbatis.StatementUpdate,
			argumentNames: []string{
				"params.VersionUUID", "params.VersionExternalID", "params.UpdatedAt", "params.UpdatedAt",
				"params.WorkspaceUUID", "params.MemoryStoreExternalID", "params.MemoryExternalID",
			},
			fragments: []string{"UPDATE memories", "deleted_at = $4", "workspace_uuid = $5"},
		},
		{
			statement: memoryMapperListPageStatement,
			bound:     buildMemoryMapperListPage(yourbatis.DialectPostgres, listParams),
			id:        "MemoryMapper.ListPage", kind: yourbatis.StatementSelect,
			argumentNames: []string{
				"params.WorkspaceUUID", "params.MemoryStoreExternalID", "params.PathPrefix", "params.PathPrefix",
				"params.CursorCreatedAt", "params.CursorCreatedAt", "params.CursorUUID", "params.Limit",
			},
			fragments: []string{"LEFT(path, LENGTH($3)) = $4", "created_at < $5", "uuid < $7", "created_at\n        DESC, uuid DESC", "LIMIT $8"},
		},
		{
			statement: memoryMapperListForDepthStatement,
			bound:     buildMemoryMapperListForDepth(yourbatis.DialectPostgres, depthParams),
			id:        "MemoryMapper.ListForDepth", kind: yourbatis.StatementSelect,
			argumentNames: []string{"params.WorkspaceUUID", "params.MemoryStoreExternalID", "params.PathPrefix", "params.PathPrefix"},
			fragments:     []string{"FROM memories", "LEFT(path, LENGTH($3)) = $4", "ORDER BY path ASC, uuid ASC"},
		},
		{
			statement: memoryMapperFindPathConflictStatement,
			bound:     buildMemoryMapperFindPathConflict(yourbatis.DialectPostgres, "workspace-uuid", "store-uuid", "/test", "memory-uuid"),
			id:        "MemoryMapper.FindPathConflict", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "storeUUID", "path", "excludeMemoryUUID"},
			fragments:     []string{"workspace_uuid = $1", "memory_store_uuid = $2", "path = $3", "uuid <> $4", "LIMIT 1"},
		},
		{
			statement: memoryMapperCountActiveHeadStatement,
			bound:     buildMemoryMapperCountActiveHead(yourbatis.DialectPostgres, "workspace-uuid", "store-id", "version-uuid"),
			id:        "MemoryMapper.CountActiveHead", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "memoryStoreExternalID", "versionUUID"},
			fragments:     []string{"CAST(COUNT(*) AS integer)", "workspace_uuid = $1", "current_version_uuid = $3"},
		},
		{
			statement: memoryMapperDeleteByStoreUUIDStatement,
			bound:     buildMemoryMapperDeleteByStoreUUID(yourbatis.DialectPostgres, "workspace-uuid", "store-uuid"),
			id:        "MemoryMapper.DeleteByStoreUUID", kind: yourbatis.StatementDelete,
			argumentNames: []string{"workspaceUUID", "storeUUID"},
			fragments:     []string{"DELETE FROM memories", "workspace_uuid = $1", "memory_store_uuid = $2"},
		},
		{
			statement: memoryMapperCountActiveStatement,
			bound:     buildMemoryMapperCountActive(yourbatis.DialectPostgres, "workspace-uuid", "store-id"),
			id:        "MemoryMapper.CountActive", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "memoryStoreExternalID"},
			fragments:     []string{"CAST(COUNT(*) AS integer)", "workspace_uuid = $1", "memory_store_external_id = $2"},
		},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			assertMemoryMapperContract(t, test)
		})
	}
}

func TestMemoryMapperListPageBranches(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	tests := []struct {
		name       string
		orderBy    string
		descending bool
		column     string
		op         string
		direction  string
	}{
		{"path ascending", "path", false, "path", ">", "ASC"},
		{"path descending", "path", true, "path", "<", "DESC"},
		{"created ascending", "created_at", false, "created_at", ">", "ASC"},
		{"created descending", "created_at", true, "created_at", "<", "DESC"},
		{"updated ascending", "updated_at", false, "updated_at", ">", "ASC"},
		{"updated descending", "updated_at", true, "updated_at", "<", "DESC"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bound := buildMemoryMapperListPage(yourbatis.DialectPostgres, listMemoriesParams{
				WorkspaceUUID: "workspace", MemoryStoreExternalID: "store", Limit: 2,
				HasCursor: true, CursorPath: "/test", CursorCreatedAt: now,
				CursorUpdatedAt: now, CursorUUID: "cursor", OrderBy: test.orderBy,
				Descending: test.descending,
			})
			compactSQL := compactMemoryMapperSQL(bound.SQL)
			if !strings.Contains(compactSQL, test.column+" "+test.op+" $3") ||
				!strings.Contains(compactSQL, "ORDER BY "+test.column+" "+test.direction+", uuid "+test.direction) {
				t.Fatalf("ListPage() SQL = %q, want %s %s cursor and %s order", bound.SQL, test.column, test.op, test.direction)
			}
			assertMemoryArgumentsNotSensitive(t, bound, nil)
		})
	}

	t.Run("no cursor and no prefix", func(t *testing.T) {
		bound := buildMemoryMapperListPage(yourbatis.DialectPostgres, listMemoriesParams{
			WorkspaceUUID: "workspace", MemoryStoreExternalID: "store", Limit: 2,
		})
		if strings.Contains(bound.SQL, "LEFT(path") || strings.Contains(bound.SQL, " OR (") {
			t.Fatalf("ListPage() unexpectedly includes optional filters: %q", bound.SQL)
		}
	})

	t.Run("path conflict without exclusion", func(t *testing.T) {
		bound := buildMemoryMapperFindPathConflict(yourbatis.DialectPostgres, "workspace", "store", "/test", "")
		if strings.Contains(bound.SQL, "uuid <>") || len(bound.Args) != 3 {
			t.Fatalf("FindPathConflict() SQL = %q args = %#v", bound.SQL, bound.Args)
		}
	})
}

func TestMemoryVersionMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	path := "/test"
	size := int64(4)
	insertParams := insertMemoryVersionParams{
		UUID: "version-uuid", ExternalID: "version-id", OrganizationUUID: "org-uuid",
		WorkspaceUUID: "workspace-uuid", MemoryStoreUUID: "store-uuid",
		MemoryStoreExternalID: "store-id", MemoryUUID: "memory-uuid",
		MemoryExternalID: "memory-id", Operation: "created", Path: &path,
		ContentSizeBytes: &size, ContentSHA256: &path, S3Bucket: &path, S3Key: &path,
		CreatedByActorType: "api_actor", CreatedByAPIKeyUUID: &path,
		CreatedByAPIKeyExternalID: &path, CreatedBySessionID: &path,
		CreatedByUserID: &path, CreatedAt: now,
	}
	listParams := listMemoryVersionsParams{
		WorkspaceUUID: "workspace-uuid", MemoryStoreExternalID: "store-id", Limit: 21,
		MemoryExternalID: "memory-id", Operation: "created", APIKeyExternalID: "key-id",
		SessionID: "session-id", HasCreatedAtGTE: true, CreatedAtGTE: now.Add(-time.Hour),
		HasCreatedAtLTE: true, CreatedAtLTE: now, HasCursor: true,
		CursorCreatedAt: now, CursorUUID: "cursor-uuid",
	}
	redactParams := redactMemoryVersionParams{
		WorkspaceUUID: "workspace-uuid", MemoryStoreExternalID: "store-id",
		VersionExternalID: "version-id", RedactedAt: now, RedactedByActorType: "api_actor",
		RedactedByAPIKeyUUID: &path, RedactedByAPIKeyExternalID: &path,
		RedactedBySessionID: &path, RedactedByUserID: &path,
	}
	tests := []memoryMapperContract{
		{
			statement: memoryVersionMapperInsertStatement,
			bound:     buildMemoryVersionMapperInsert(yourbatis.DialectPostgres, insertParams),
			id:        "MemoryVersionMapper.Insert", kind: yourbatis.StatementInsert,
			argumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.MemoryStoreUUID", "params.MemoryStoreExternalID", "params.MemoryUUID",
				"params.MemoryExternalID", "params.Operation", "params.Path", "params.ContentSizeBytes",
				"params.ContentSHA256", "params.S3Bucket", "params.S3Key", "params.CreatedByActorType",
				"params.CreatedByAPIKeyUUID", "params.CreatedByAPIKeyExternalID", "params.CreatedBySessionID",
				"params.CreatedByUserID", "params.CreatedAt",
			},
			fragments: []string{"INSERT INTO memory_versions", "RETURNING"},
		},
		{
			statement: memoryVersionMapperFindByExternalIDStatement,
			bound:     buildMemoryVersionMapperFindByExternalID(yourbatis.DialectPostgres, "workspace-uuid", "store-id", "version-id"),
			id:        "MemoryVersionMapper.FindByExternalID", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "memoryStoreExternalID", "versionExternalID"},
			fragments:     []string{"FROM memory_versions", "workspace_uuid = $1", "external_id = $3"},
		},
		{
			statement: memoryVersionMapperFindForUpdateStatement,
			bound:     buildMemoryVersionMapperFindForUpdate(yourbatis.DialectPostgres, "workspace-uuid", "store-id", "version-id"),
			id:        "MemoryVersionMapper.FindForUpdate", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "memoryStoreExternalID", "versionExternalID"},
			fragments:     []string{"workspace_uuid = $1", "external_id = $3", "FOR UPDATE"},
		},
		{
			statement: memoryVersionMapperListPageStatement,
			bound:     buildMemoryVersionMapperListPage(yourbatis.DialectPostgres, listParams),
			id:        "MemoryVersionMapper.ListPage", kind: yourbatis.StatementSelect,
			argumentNames: []string{
				"params.WorkspaceUUID", "params.MemoryStoreExternalID", "params.MemoryExternalID",
				"params.Operation", "params.APIKeyExternalID", "params.SessionID", "params.CreatedAtGTE",
				"params.CreatedAtLTE", "params.CursorCreatedAt", "params.CursorCreatedAt",
				"params.CursorUUID", "params.Limit",
			},
			fragments: []string{"memory_external_id = $3", "operation = $4", "created_by_session_id = $6", "created_at >= $7", "uuid < $11", "LIMIT $12"},
		},
		{
			statement: memoryVersionMapperListObjectRefsByStoreUUIDStatement,
			bound:     buildMemoryVersionMapperListObjectRefsByStoreUUID(yourbatis.DialectPostgres, "workspace-uuid", "store-uuid"),
			id:        "MemoryVersionMapper.ListObjectRefsByStoreUUID", kind: yourbatis.StatementSelect,
			argumentNames: []string{"workspaceUUID", "storeUUID"},
			fragments:     []string{"FROM memory_versions", "workspace_uuid = $1", "memory_store_uuid = $2", "s3_key IS NOT NULL"},
		},
		{
			statement: memoryVersionMapperRedactByExternalIDStatement,
			bound:     buildMemoryVersionMapperRedactByExternalID(yourbatis.DialectPostgres, redactParams),
			id:        "MemoryVersionMapper.RedactByExternalID", kind: yourbatis.StatementUpdate,
			argumentNames: []string{
				"params.RedactedAt", "params.RedactedByActorType", "params.RedactedByAPIKeyUUID",
				"params.RedactedByAPIKeyExternalID", "params.RedactedBySessionID", "params.RedactedByUserID",
				"params.WorkspaceUUID", "params.MemoryStoreExternalID", "params.VersionExternalID",
			},
			fragments: []string{"UPDATE memory_versions", "path = NULL", "redacted_at = $1", "workspace_uuid = $7", "RETURNING"},
		},
		{
			statement: memoryVersionMapperDeleteByStoreUUIDStatement,
			bound:     buildMemoryVersionMapperDeleteByStoreUUID(yourbatis.DialectPostgres, "workspace-uuid", "store-uuid"),
			id:        "MemoryVersionMapper.DeleteByStoreUUID", kind: yourbatis.StatementDelete,
			argumentNames: []string{"workspaceUUID", "storeUUID"},
			fragments:     []string{"DELETE FROM memory_versions", "workspace_uuid = $1", "memory_store_uuid = $2"},
		},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			assertMemoryMapperContract(t, test)
		})
	}

	t.Run("list without filters", func(t *testing.T) {
		bound := buildMemoryVersionMapperListPage(yourbatis.DialectPostgres, listMemoryVersionsParams{
			WorkspaceUUID: "workspace", MemoryStoreExternalID: "store", Limit: 2,
		})
		for _, fragment := range []string{"memory_external_id =", "operation =", "created_by_session_id =", "created_at >=", "created_at <"} {
			if strings.Contains(bound.SQL, fragment) {
				t.Fatalf("ListPage() unexpectedly includes %q: %q", fragment, bound.SQL)
			}
		}
	})
}

func TestMemoryMapperExecutionModes(t *testing.T) {
	ctx := context.Background()
	tests := []mapperExecutionErrorContract{
		{statementID: "MemoryStoreMapper.Insert", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryStoreMapper(executor).Insert(ctx, insertMemoryStoreParams{})
			return err
		}},
		{statementID: "MemoryStoreMapper.FindByExternalID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryStoreMapper(executor).FindByExternalID(ctx, "workspace", "store")
			return err
		}},
		{statementID: "MemoryStoreMapper.FindByOrganizationAndExternalID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryStoreMapper(executor).FindByOrganizationAndExternalID(ctx, "org", "store")
			return err
		}},
		{statementID: "MemoryStoreMapper.FindForUpdate", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryStoreMapper(executor).FindForUpdate(ctx, "workspace", "store")
			return err
		}},
		{statementID: "MemoryStoreMapper.UpdateByExternalID", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryStoreMapper(executor).UpdateByExternalID(ctx, updateMemoryStoreParams{})
			return err
		}},
		{statementID: "MemoryStoreMapper.ArchiveByExternalID", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryStoreMapper(executor).ArchiveByExternalID(ctx, "workspace", "store")
			return err
		}},
		{statementID: "MemoryStoreMapper.FindUUIDForUpdate", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryStoreMapper(executor).FindUUIDForUpdate(ctx, "workspace", "store")
			return err
		}},
		{statementID: "MemoryStoreMapper.DeleteByUUID", kind: yourbatis.StatementDelete, call: func(executor yourbatis.Executor) error {
			return NewMemoryStoreMapper(executor).DeleteByUUID(ctx, "workspace", "store")
		}},
		{statementID: "MemoryStoreMapper.ListPage", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryStoreMapper(executor).ListPage(ctx, listMemoryStoresParams{})
			return err
		}},
		{statementID: "MemoryStoreMapper.Exists", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryStoreMapper(executor).Exists(ctx, "workspace", "store")
			return err
		}},
		{statementID: "MemoryMapper.Insert", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryMapper(executor).Insert(ctx, insertMemoryParams{})
			return err
		}},
		{statementID: "MemoryMapper.FindByExternalID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryMapper(executor).FindByExternalID(ctx, "workspace", "store", "memory")
			return err
		}},
		{statementID: "MemoryMapper.FindForUpdate", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryMapper(executor).FindForUpdate(ctx, "workspace", "store", "memory")
			return err
		}},
		{statementID: "MemoryMapper.UpdateCurrentVersion", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryMapper(executor).UpdateCurrentVersion(ctx, updateMemoryCurrentVersionParams{})
			return err
		}},
		{statementID: "MemoryMapper.UpdateByExternalID", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryMapper(executor).UpdateByExternalID(ctx, updateMemoryParams{})
			return err
		}},
		{statementID: "MemoryMapper.SoftDeleteByExternalID", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryMapper(executor).SoftDeleteByExternalID(ctx, deleteMemoryParams{})
			return err
		}},
		{statementID: "MemoryMapper.ListPage", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryMapper(executor).ListPage(ctx, listMemoriesParams{})
			return err
		}},
		{statementID: "MemoryMapper.ListForDepth", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryMapper(executor).ListForDepth(ctx, listMemoriesForDepthParams{})
			return err
		}},
		{statementID: "MemoryMapper.FindPathConflict", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, _, err := NewMemoryMapper(executor).FindPathConflict(ctx, "workspace", "store", "/test", "")
			return err
		}},
		{statementID: "MemoryMapper.CountActiveHead", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryMapper(executor).CountActiveHead(ctx, "workspace", "store", "version")
			return err
		}},
		{statementID: "MemoryMapper.DeleteByStoreUUID", kind: yourbatis.StatementDelete, call: func(executor yourbatis.Executor) error {
			return NewMemoryMapper(executor).DeleteByStoreUUID(ctx, "workspace", "store")
		}},
		{statementID: "MemoryVersionMapper.Insert", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryVersionMapper(executor).Insert(ctx, insertMemoryVersionParams{})
			return err
		}},
		{statementID: "MemoryVersionMapper.FindByExternalID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryVersionMapper(executor).FindByExternalID(ctx, "workspace", "store", "version")
			return err
		}},
		{statementID: "MemoryVersionMapper.FindForUpdate", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryVersionMapper(executor).FindForUpdate(ctx, "workspace", "store", "version")
			return err
		}},
		{statementID: "MemoryVersionMapper.ListPage", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryVersionMapper(executor).ListPage(ctx, listMemoryVersionsParams{})
			return err
		}},
		{statementID: "MemoryVersionMapper.ListObjectRefsByStoreUUID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryVersionMapper(executor).ListObjectRefsByStoreUUID(ctx, "workspace", "store")
			return err
		}},
		{statementID: "MemoryVersionMapper.RedactByExternalID", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewMemoryVersionMapper(executor).RedactByExternalID(ctx, redactMemoryVersionParams{})
			return err
		}},
		{statementID: "MemoryVersionMapper.DeleteByStoreUUID", kind: yourbatis.StatementDelete, call: func(executor yourbatis.Executor) error {
			return NewMemoryVersionMapper(executor).DeleteByStoreUUID(ctx, "workspace", "store")
		}},
	}
	for _, test := range tests {
		t.Run(test.statementID, func(t *testing.T) {
			assertMapperExecutionError(t, test)
		})
	}
}

func TestMemoryMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	t.Run("single row not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: memoryStoreMapperTestColumns()})
		_, err := NewMemoryStoreMapper(executor).FindByExternalID(ctx, "workspace", "store")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("optional path conflict not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"external_id"}})
		value, found, err := NewMemoryMapper(executor).FindPathConflict(ctx, "workspace", "store", "/test", "")
		if err != nil || found || value != "" {
			t.Fatalf("FindPathConflict() = (%q, %t, %v)", value, found, err)
		}
	})

	t.Run("string UUID and JSON row", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: memoryStoreMapperTestColumns(),
			rows:    [][]driver.Value{memoryStoreMapperTestRow()},
		})
		row, err := NewMemoryStoreMapper(executor).Insert(ctx, insertMemoryStoreParams{})
		store, mapErr := memoryStoreFromMapperRow(row, err)
		if mapErr != nil || store.UUID != "00000000-0000-4000-8000-000000000001" || string(store.Metadata) != `{"key":"value"}` {
			t.Fatalf("Insert() = (%+v, %v)", store, mapErr)
		}
	})

	t.Run("nullable string UUID row", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: memoryVersionMapperTestColumns(),
			rows:    [][]driver.Value{memoryVersionMapperTestRow()},
		})
		row, err := NewMemoryVersionMapper(executor).FindByExternalID(ctx, "workspace", "store", "version")
		version, mapErr := memoryVersionFromMapperRow(row, err)
		if mapErr != nil || version.CreatedBy.APIKeyUUID != "" || version.RedactedBy == nil || version.RedactedBy.APIKeyUUID == "" {
			t.Fatalf("FindByExternalID() = (%+v, %v)", version, mapErr)
		}
	})

	t.Run("many rows, scalar, and rows affected", func(t *testing.T) {
		listExecutor := newMapperTestExecutor(t, mapperTestResponse{columns: memoryStoreMapperTestColumns()})
		rows, err := NewMemoryStoreMapper(listExecutor).ListPage(ctx, listMemoryStoresParams{})
		if err != nil || len(rows) != 0 {
			t.Fatalf("ListPage() = (%+v, %v)", rows, err)
		}
		existsExecutor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"exists"}, rows: [][]driver.Value{{true}},
		})
		exists, err := NewMemoryStoreMapper(existsExecutor).Exists(ctx, "workspace", "store")
		if err != nil || !exists {
			t.Fatalf("Exists() = (%t, %v)", exists, err)
		}
		rowsExecutor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		rowsAffected, err := NewMemoryMapper(rowsExecutor).SoftDeleteByExternalID(ctx, deleteMemoryParams{})
		if err != nil || rowsAffected != 1 {
			t.Fatalf("SoftDeleteByExternalID() = (%d, %v)", rowsAffected, err)
		}
	})
}

func assertMemoryMapperContract(t *testing.T, contract memoryMapperContract) {
	t.Helper()
	if contract.statement.ID != contract.id || contract.statement.Kind != contract.kind || contract.statement.Source == "" {
		t.Fatalf("statement = %+v, want ID %q, kind %q, and source", contract.statement, contract.id, contract.kind)
	}
	argumentNames := make([]string, len(contract.bound.Args))
	for index := range contract.bound.Args {
		argumentNames[index] = contract.bound.Args[index].Name
	}
	if !reflect.DeepEqual(argumentNames, contract.argumentNames) {
		t.Fatalf("argument names = %#v, want %#v", argumentNames, contract.argumentNames)
	}
	if strings.Contains(contract.bound.SQL, "#{") || strings.Contains(contract.bound.SQL, "::") || strings.Contains(contract.bound.SQL, " AS uuid)") {
		t.Fatalf("SQL retains unsupported placeholder or UUID cast: %q", contract.bound.SQL)
	}
	compactSQL := compactMemoryMapperSQL(contract.bound.SQL)
	for _, fragment := range contract.fragments {
		if !strings.Contains(compactSQL, compactMemoryMapperSQL(fragment)) {
			t.Fatalf("SQL = %q, want fragment %q", contract.bound.SQL, fragment)
		}
	}
	assertMemoryArgumentsNotSensitive(t, contract.bound, contract.sensitiveNames)
}

func compactMemoryMapperSQL(query string) string {
	return strings.Join(strings.Fields(query), " ")
}

func assertMemoryArgumentsNotSensitive(t *testing.T, bound yourbatis.BoundSQL, sensitiveNames []string) {
	t.Helper()
	for _, argument := range bound.Args {
		wantSensitive := false
		for _, name := range sensitiveNames {
			wantSensitive = wantSensitive || argument.Name == name
		}
		if argument.Sensitive != wantSensitive {
			t.Fatalf("argument %q sensitive = %t, want %t", argument.Name, argument.Sensitive, wantSensitive)
		}
	}
}

func memoryStoreMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "created_by_api_key_uuid",
		"name", "description", "metadata", "created_at", "updated_at", "archived_at", "deleted_at",
	}
}

func memoryStoreMapperTestRow() []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000001", "store-id",
		"00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000004", "store", "description", []byte(`{"key":"value"}`),
		now, now, nil, nil,
	}
}

func memoryVersionMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "memory_store_uuid",
		"memory_store_external_id", "memory_uuid", "memory_external_id", "operation", "path",
		"content_size_bytes", "content_sha256", "s3_bucket", "s3_key", "created_by_actor_type",
		"created_by_api_key_uuid", "created_by_api_key_external_id", "created_by_session_id",
		"created_by_user_id", "redacted_at", "redacted_by_actor_type", "redacted_by_api_key_uuid",
		"redacted_by_api_key_external_id", "redacted_by_session_id", "redacted_by_user_id", "created_at",
	}
}

func memoryVersionMapperTestRow() []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000001", "version-id",
		"00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000004", "store-id",
		"00000000-0000-4000-8000-000000000005", "memory-id", "modified", "/test", int64(4),
		"sha", "bucket", "key", "session_actor", nil, nil, "session-id", nil, now,
		"api_actor", "00000000-0000-4000-8000-000000000006", "key-id", nil, nil, now,
	}
}
