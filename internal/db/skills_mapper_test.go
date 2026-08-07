package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSkillMapperBuilderContracts(t *testing.T) {
	const (
		workspaceUUID = "00000000-0000-4000-8000-000000000001"
		skillUUID     = "00000000-0000-4000-8000-000000000002"
		externalID    = "skill_test"
	)
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	displayTitle := "Test skill"
	latestVersion := "1.0.0"
	insertParams := insertSkillParams{
		UUID:                skillUUID,
		ExternalID:          externalID,
		WorkspaceUUID:       workspaceUUID,
		CreatedByAPIKeyUUID: "00000000-0000-4000-8000-000000000003",
		DisplayTitle:        &displayTitle,
		LatestVersion:       latestVersion,
		CreatedAt:           now,
	}
	updateParams := updateSkillLatestVersionParams{
		WorkspaceUUID: workspaceUUID,
		ExternalID:    externalID,
		LatestVersion: latestVersion,
		UpdatedAt:     now,
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{
			name: "find display title conflict",
			contract: mapperBuilderContract{
				statement: skillMapperFindExternalIDByDisplayTitleStatement,
				bound: buildSkillMapperFindExternalIDByDisplayTitle(
					yourbatis.DialectPostgres, workspaceUUID, displayTitle,
				),
				wantID:            "SkillMapper.FindExternalIDByDisplayTitle",
				wantKind:          yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "displayTitle"},
				wantSQLFragments:  []string{"FROM skills", "workspace_uuid = $1", "display_title = $2", "deleted_at IS NULL"},
			},
		},
		{
			name: "insert",
			contract: mapperBuilderContract{
				statement: skillMapperInsertStatement,
				bound:     buildSkillMapperInsert(yourbatis.DialectPostgres, insertParams),
				wantID:    "SkillMapper.Insert",
				wantKind:  yourbatis.StatementInsert,
				wantArgumentNames: []string{
					"params.UUID", "params.ExternalID", "params.WorkspaceUUID",
					"params.CreatedByAPIKeyUUID", "params.DisplayTitle", "params.LatestVersion",
					"params.CreatedAt", "params.CreatedAt",
				},
				wantSQLFragments: []string{"INSERT INTO skills", "'custom'", "RETURNING"},
			},
		},
		{
			name: "find",
			contract: mapperBuilderContract{
				statement: skillMapperFindByExternalIDStatement,
				bound:     buildSkillMapperFindByExternalID(yourbatis.DialectPostgres, workspaceUUID, externalID),
				wantID:    "SkillMapper.FindByExternalID", wantKind: yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "externalID"},
				wantSQLFragments:  []string{"FROM skills", "workspace_uuid = $1", "external_id = $2"},
			},
		},
		{
			name: "lock",
			contract: mapperBuilderContract{
				statement: skillMapperFindForUpdateByExternalIDStatement,
				bound:     buildSkillMapperFindForUpdateByExternalID(yourbatis.DialectPostgres, workspaceUUID, externalID),
				wantID:    "SkillMapper.FindForUpdateByExternalID", wantKind: yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "externalID"},
				wantSQLFragments:  []string{"workspace_uuid = $1", "external_id = $2", "FOR UPDATE"},
			},
		},
		{
			name: "list page",
			contract: mapperBuilderContract{
				statement: skillMapperListPageStatement,
				bound:     buildSkillMapperListPage(yourbatis.DialectPostgres, workspaceUUID, 21, 2),
				wantID:    "SkillMapper.ListPage", wantKind: yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "limit", "offset"},
				wantSQLFragments:  []string{"workspace_uuid = $1", "ORDER BY created_at DESC, uuid DESC", "LIMIT $2 OFFSET $3"},
			},
		},
		{
			name: "find UUID",
			contract: mapperBuilderContract{
				statement: skillMapperFindUUIDByExternalIDStatement,
				bound:     buildSkillMapperFindUUIDByExternalID(yourbatis.DialectPostgres, workspaceUUID, externalID),
				wantID:    "SkillMapper.FindUUIDByExternalID", wantKind: yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "externalID"},
				wantSQLFragments:  []string{"SELECT uuid", "workspace_uuid = $1", "external_id = $2"},
			},
		},
		{
			name: "update latest version by external ID",
			contract: mapperBuilderContract{
				statement: skillMapperUpdateLatestVersionByExternalIDStatement,
				bound:     buildSkillMapperUpdateLatestVersionByExternalID(yourbatis.DialectPostgres, updateParams),
				wantID:    "SkillMapper.UpdateLatestVersionByExternalID", wantKind: yourbatis.StatementUpdate,
				wantArgumentNames: []string{
					"params.LatestVersion", "params.UpdatedAt", "params.WorkspaceUUID", "params.ExternalID",
				},
				wantSQLFragments: []string{"UPDATE skills", "latest_version = $1", "workspace_uuid = $3", "RETURNING"},
			},
		},
		{
			name: "soft delete",
			contract: mapperBuilderContract{
				statement: skillMapperSoftDeleteByUUIDStatement,
				bound:     buildSkillMapperSoftDeleteByUUID(yourbatis.DialectPostgres, workspaceUUID, skillUUID),
				wantID:    "SkillMapper.SoftDeleteByUUID", wantKind: yourbatis.StatementUpdate,
				wantArgumentNames: []string{"workspaceUUID", "skillUUID"},
				wantSQLFragments:  []string{"UPDATE skills", "workspace_uuid = $1", "uuid = $2", "RETURNING"},
			},
		},
		{
			name: "update latest version by UUID",
			contract: mapperBuilderContract{
				statement: skillMapperUpdateLatestVersionByUUIDStatement,
				bound: buildSkillMapperUpdateLatestVersionByUUID(
					yourbatis.DialectPostgres, workspaceUUID, skillUUID, &latestVersion,
				),
				wantID:            "SkillMapper.UpdateLatestVersionByUUID",
				wantKind:          yourbatis.StatementUpdate,
				wantArgumentNames: []string{"latestVersion", "workspaceUUID", "skillUUID"},
				wantSQLFragments:  []string{"UPDATE skills", "latest_version = $1", "workspace_uuid = $2", "uuid = $3"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
			if strings.Contains(test.contract.bound.SQL, " AS uuid)") {
				t.Fatalf("mapper SQL contains UUID cast ceremony: %q", test.contract.bound.SQL)
			}
		})
	}
}

func TestSkillVersionMapperCRUDBuilderContracts(t *testing.T) {
	const (
		workspaceUUID = "00000000-0000-4000-8000-000000000001"
		skillUUID     = "00000000-0000-4000-8000-000000000002"
		externalID    = "skill_test"
		version       = "1.0.0"
	)
	params := insertSkillVersionParams{
		UUID: "00000000-0000-4000-8000-000000000003", ExternalID: "skillver_test",
		WorkspaceUUID: workspaceUUID, SkillUUID: skillUUID, SkillExternalID: externalID,
		Version: version, Name: "test", Description: "description", Directory: "test",
		S3Bucket: "bucket", S3Key: "skills/test", SizeBytes: 42, SHA256: strings.Repeat("a", 64),
		CreatedByAPIKeyUUID: "00000000-0000-4000-8000-000000000004",
		CreatedAt:           time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC),
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{
			name: "insert",
			contract: mapperBuilderContract{
				statement: skillVersionMapperInsertStatement,
				bound:     buildSkillVersionMapperInsert(yourbatis.DialectPostgres, params),
				wantID:    "SkillVersionMapper.Insert", wantKind: yourbatis.StatementInsert,
				wantArgumentNames: []string{
					"params.UUID", "params.ExternalID", "params.WorkspaceUUID", "params.SkillUUID",
					"params.SkillExternalID", "params.Version", "params.Name", "params.Description",
					"params.Directory", "params.S3Bucket", "params.S3Key", "params.SizeBytes",
					"params.SHA256", "params.CreatedByAPIKeyUUID", "params.CreatedAt",
				},
				wantSQLFragments: []string{"INSERT INTO skill_versions", "RETURNING"},
			},
		},
		{
			name: "find",
			contract: mapperBuilderContract{
				statement: skillVersionMapperFindStatement,
				bound:     buildSkillVersionMapperFind(yourbatis.DialectPostgres, workspaceUUID, externalID, version),
				wantID:    "SkillVersionMapper.Find", wantKind: yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "skillExternalID", "version"},
				wantSQLFragments:  []string{"FROM skill_versions", "workspace_uuid = $1", "skill_external_id = $2", "version = $3"},
			},
		},
		{
			name: "find latest",
			contract: mapperBuilderContract{
				statement: skillVersionMapperFindLatestStatement,
				bound:     buildSkillVersionMapperFindLatest(yourbatis.DialectPostgres, workspaceUUID, externalID),
				wantID:    "SkillVersionMapper.FindLatest", wantKind: yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "workspaceUUID", "skillExternalID"},
				wantSQLFragments:  []string{"JOIN skill_versions sv", "s.workspace_uuid = $1", "sv.workspace_uuid = $2", "s.external_id = $3"},
			},
		},
		{
			name: "list page",
			contract: mapperBuilderContract{
				statement: skillVersionMapperListPageBySkillUUIDStatement,
				bound:     buildSkillVersionMapperListPageBySkillUUID(yourbatis.DialectPostgres, workspaceUUID, skillUUID, 21, 2),
				wantID:    "SkillVersionMapper.ListPageBySkillUUID", wantKind: yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "skillUUID", "limit", "offset"},
				wantSQLFragments:  []string{"workspace_uuid = $1", "skill_uuid = $2", "LIMIT $3 OFFSET $4"},
			},
		},
		{
			name: "list by skill",
			contract: mapperBuilderContract{
				statement: skillVersionMapperListBySkillUUIDStatement,
				bound:     buildSkillVersionMapperListBySkillUUID(yourbatis.DialectPostgres, workspaceUUID, skillUUID),
				wantID:    "SkillVersionMapper.ListBySkillUUID", wantKind: yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "skillUUID"},
				wantSQLFragments:  []string{"workspace_uuid = $1", "skill_uuid = $2", "ORDER BY created_at DESC"},
			},
		},
		{
			name: "soft delete by skill",
			contract: mapperBuilderContract{
				statement: skillVersionMapperSoftDeleteBySkillUUIDStatement,
				bound:     buildSkillVersionMapperSoftDeleteBySkillUUID(yourbatis.DialectPostgres, workspaceUUID, skillUUID),
				wantID:    "SkillVersionMapper.SoftDeleteBySkillUUID", wantKind: yourbatis.StatementUpdate,
				wantArgumentNames: []string{"workspaceUUID", "skillUUID"},
				wantSQLFragments:  []string{"UPDATE skill_versions", "workspace_uuid = $1", "skill_uuid = $2"},
			},
		},
		{
			name: "soft delete version",
			contract: mapperBuilderContract{
				statement: skillVersionMapperSoftDeleteByVersionStatement,
				bound:     buildSkillVersionMapperSoftDeleteByVersion(yourbatis.DialectPostgres, workspaceUUID, skillUUID, version),
				wantID:    "SkillVersionMapper.SoftDeleteByVersion", wantKind: yourbatis.StatementUpdate,
				wantArgumentNames: []string{"workspaceUUID", "skillUUID", "version"},
				wantSQLFragments:  []string{"UPDATE skill_versions", "workspace_uuid = $1", "version = $3", "RETURNING"},
			},
		},
		{
			name: "find latest version",
			contract: mapperBuilderContract{
				statement: skillVersionMapperFindLatestVersionStatement,
				bound:     buildSkillVersionMapperFindLatestVersion(yourbatis.DialectPostgres, workspaceUUID, skillUUID),
				wantID:    "SkillVersionMapper.FindLatestVersion", wantKind: yourbatis.StatementSelect,
				wantArgumentNames: []string{"workspaceUUID", "skillUUID"},
				wantSQLFragments:  []string{"SELECT version", "workspace_uuid = $1", "ORDER BY created_at DESC", "LIMIT 1"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
			if strings.Contains(test.contract.bound.SQL, " AS uuid)") {
				t.Fatalf("mapper SQL contains UUID cast ceremony: %q", test.contract.bound.SQL)
			}
		})
	}
}

func TestSkillMapperExecutionErrors(t *testing.T) {
	ctx := context.Background()
	insertParams := insertSkillParams{}
	updateParams := updateSkillLatestVersionParams{}
	versionParams := insertSkillVersionParams{}
	tests := []mapperExecutionErrorContract{
		{statementID: "SkillMapper.FindExternalIDByDisplayTitle", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, _, err := NewSkillMapper(executor).FindExternalIDByDisplayTitle(ctx, "workspace", "title")
			return err
		}},
		{statementID: "SkillMapper.Insert", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillMapper(executor).Insert(ctx, insertParams)
			return err
		}},
		{statementID: "SkillMapper.FindByExternalID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillMapper(executor).FindByExternalID(ctx, "workspace", "skill")
			return err
		}},
		{statementID: "SkillMapper.FindForUpdateByExternalID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillMapper(executor).FindForUpdateByExternalID(ctx, "workspace", "skill")
			return err
		}},
		{statementID: "SkillMapper.ListPage", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillMapper(executor).ListPage(ctx, "workspace", 1, 0)
			return err
		}},
		{statementID: "SkillMapper.FindUUIDByExternalID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillMapper(executor).FindUUIDByExternalID(ctx, "workspace", "skill")
			return err
		}},
		{statementID: "SkillMapper.UpdateLatestVersionByExternalID", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillMapper(executor).UpdateLatestVersionByExternalID(ctx, updateParams)
			return err
		}},
		{statementID: "SkillMapper.SoftDeleteByUUID", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillMapper(executor).SoftDeleteByUUID(ctx, "workspace", "skill")
			return err
		}},
		{statementID: "SkillMapper.UpdateLatestVersionByUUID", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewSkillMapper(executor).UpdateLatestVersionByUUID(ctx, "workspace", "skill", nil)
		}},
		{statementID: "SkillVersionMapper.Insert", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillVersionMapper(executor).Insert(ctx, versionParams)
			return err
		}},
		{statementID: "SkillVersionMapper.Find", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillVersionMapper(executor).Find(ctx, "workspace", "skill", "1")
			return err
		}},
		{statementID: "SkillVersionMapper.FindLatest", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillVersionMapper(executor).FindLatest(ctx, "workspace", "skill")
			return err
		}},
		{statementID: "SkillVersionMapper.ListPageBySkillUUID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillVersionMapper(executor).ListPageBySkillUUID(ctx, "workspace", "skill", 1, 0)
			return err
		}},
		{statementID: "SkillVersionMapper.ListBySkillUUID", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillVersionMapper(executor).ListBySkillUUID(ctx, "workspace", "skill")
			return err
		}},
		{statementID: "SkillVersionMapper.SoftDeleteBySkillUUID", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			return NewSkillVersionMapper(executor).SoftDeleteBySkillUUID(ctx, "workspace", "skill")
		}},
		{statementID: "SkillVersionMapper.SoftDeleteByVersion", kind: yourbatis.StatementUpdate, query: true, call: func(executor yourbatis.Executor) error {
			_, err := NewSkillVersionMapper(executor).SoftDeleteByVersion(ctx, "workspace", "skill", "1")
			return err
		}},
		{statementID: "SkillVersionMapper.FindLatestVersion", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			_, _, err := NewSkillVersionMapper(executor).FindLatestVersion(ctx, "workspace", "skill")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.statementID, func(t *testing.T) {
			assertMapperExecutionError(t, test)
		})
	}
}

func TestSkillMapperOptionalRows(t *testing.T) {
	t.Run("display title conflict not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"external_id"}})
		value, found, err := NewSkillMapper(executor).FindExternalIDByDisplayTitle(
			context.Background(),
			"workspace",
			"title",
		)
		if err != nil || found || value != "" {
			t.Fatalf("FindExternalIDByDisplayTitle = (%q, %t, %v), want empty optional row", value, found, err)
		}
	})

	t.Run("latest version not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"version"}})
		value, found, err := NewSkillVersionMapper(executor).FindLatestVersion(
			context.Background(),
			"workspace",
			"skill",
		)
		if err != nil || found || value != "" {
			t.Fatalf("FindLatestVersion = (%q, %t, %v), want empty optional row", value, found, err)
		}
	})
}
