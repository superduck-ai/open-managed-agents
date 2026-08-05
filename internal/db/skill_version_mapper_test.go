package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSkillVersionMapperBuilderContract(t *testing.T) {
	params := sessionSkillArchiveValidationParams{
		Source:        "custom",
		WorkspaceUUID: "workspace-uuid",
		VersionUUID:   "version-uuid",
		Directory:     "/skills/example",
		S3Bucket:      "skills",
		S3Key:         "example/version.zip",
		SizeBytes:     42,
		SHA256:        "sha256",
	}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: skillVersionMapperValidateSkillArchiveVersionStatement,
		bound:     buildSkillVersionMapperValidateSkillArchiveVersion(yourbatis.DialectPostgres, params),
		wantID:    "SkillVersionMapper.ValidateSkillArchiveVersion",
		wantKind:  yourbatis.StatementSelect,
		wantArgumentNames: []string{
			"params.Source",
			"params.WorkspaceUUID",
			"params.VersionUUID",
			"params.Directory",
			"params.S3Bucket",
			"params.S3Key",
			"params.SizeBytes",
			"params.SHA256",
			"params.VersionUUID",
			"params.Directory",
			"params.S3Bucket",
			"params.S3Key",
			"params.SizeBytes",
			"params.SHA256",
		},
		wantSQLFragments: []string{
			"CASE CAST($1 AS text)",
			"FROM skill_versions version",
			"FROM builtin_skill_versions version",
			"version.workspace_uuid = $2",
			"ELSE false",
		},
	})
}

func TestSkillVersionMapperExecutionSemantics(t *testing.T) {
	params := sessionSkillArchiveValidationParams{Source: "custom"}

	t.Run("query error", func(t *testing.T) {
		queryErr := errors.New("query failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: queryErr})
		_, err := NewSkillVersionMapper(executor).ValidateSkillArchiveVersion(context.Background(), params)
		if !errors.Is(err, queryErr) {
			t.Fatalf("ValidateSkillArchiveVersion error = %v, want %v", err, queryErr)
		}
		assertMapperTestExecution(t, executor, "SkillVersionMapper.ValidateSkillArchiveVersion", yourbatis.StatementSelect, []any{"custom", "", "", "", "", "", int64(0), "", "", "", "", "", int64(0), ""}, "CASE CAST($1 AS text)")
	})

	t.Run("scan error", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"valid"},
			rows:    [][]driver.Value{{"not-a-bool"}},
		})
		_, err := NewSkillVersionMapper(executor).ValidateSkillArchiveVersion(context.Background(), params)
		if err == nil {
			t.Fatal("ValidateSkillArchiveVersion error = nil, want scan error")
		}
	})

	t.Run("zero rows", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"valid"}})
		_, err := NewSkillVersionMapper(executor).ValidateSkillArchiveVersion(context.Background(), params)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("ValidateSkillArchiveVersion error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"valid"},
			rows:    [][]driver.Value{{true}},
		})
		valid, err := NewSkillVersionMapper(executor).ValidateSkillArchiveVersion(context.Background(), params)
		if err != nil || !valid {
			t.Fatalf("ValidateSkillArchiveVersion = (%t, %v), want (true, nil)", valid, err)
		}
	})
}
