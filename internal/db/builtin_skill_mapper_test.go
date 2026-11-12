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

func TestBuiltinSkillMapperStatements(t *testing.T) {
	skillExternalID := "builtin_test"
	skillUUID := "11111111-1111-4111-8111-111111111111"
	version := "20260805"

	tests := []struct {
		name      string
		statement yourbatis.Statement
		bound     yourbatis.BoundSQL
		id        string
		kind      yourbatis.StatementKind
		values    []any
		fragments []string
	}{
		{
			name:      "list skills",
			statement: builtinSkillMapperListSkillsPageStatement,
			bound:     buildBuiltinSkillMapperListSkillsPage(yourbatis.DialectPostgres, 21, 2),
			id:        "BuiltinSkillMapper.ListSkillsPage",
			kind:      yourbatis.StatementSelect,
			values:    []any{21, 2},
			fragments: []string{"created_at DESC, id DESC", "LIMIT $1", "OFFSET $2"},
		},
		{
			name:      "count skills",
			statement: builtinSkillMapperCountSkillsStatement,
			bound:     buildBuiltinSkillMapperCountSkills(yourbatis.DialectPostgres),
			id:        "BuiltinSkillMapper.CountSkills",
			kind:      yourbatis.StatementSelect,
			values:    []any{},
			fragments: []string{"SELECT COUNT(*)", "deleted_at IS NULL"},
		},
		{
			name:      "find skill",
			statement: builtinSkillMapperFindSkillByExternalIDStatement,
			bound:     buildBuiltinSkillMapperFindSkillByExternalID(yourbatis.DialectPostgres, skillExternalID),
			id:        "BuiltinSkillMapper.FindSkillByExternalID",
			kind:      yourbatis.StatementSelect,
			values:    []any{skillExternalID},
			fragments: []string{"external_id = $1", "deleted_at IS NULL"},
		},
		{
			name:      "find skill ID",
			statement: builtinSkillMapperFindSkillUUIDByExternalIDStatement,
			bound:     buildBuiltinSkillMapperFindSkillUUIDByExternalID(yourbatis.DialectPostgres, skillExternalID),
			id:        "BuiltinSkillMapper.FindSkillUUIDByExternalID",
			kind:      yourbatis.StatementSelect,
			values:    []any{skillExternalID},
			fragments: []string{"SELECT uuid", "external_id = $1"},
		},
		{
			name:      "list versions",
			statement: builtinSkillMapperListVersionsPageStatement,
			bound:     buildBuiltinSkillMapperListVersionsPage(yourbatis.DialectPostgres, skillUUID, 21, 2, false),
			id:        "BuiltinSkillMapper.ListVersionsPage",
			kind:      yourbatis.StatementSelect,
			values:    []any{skillUUID, 21, 2},
			fragments: []string{"skill_uuid = $1", "created_at DESC, id DESC", "LIMIT $2", "OFFSET $3"},
		},
		{
			name:      "find version",
			statement: builtinSkillMapperFindVersionStatement,
			bound:     buildBuiltinSkillMapperFindVersion(yourbatis.DialectPostgres, skillExternalID, version),
			id:        "BuiltinSkillMapper.FindVersion",
			kind:      yourbatis.StatementSelect,
			values:    []any{skillExternalID, version, version, version},
			fragments: []string{"skill_external_id = $1", "version = $2", "deleted_at IS NULL"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.statement.ID != test.id || test.statement.Kind != test.kind || test.statement.Source == "" {
				t.Fatalf("statement = %+v, want ID %q, kind %q, and source", test.statement, test.id, test.kind)
			}
			if values := test.bound.Values(); !reflect.DeepEqual(values, test.values) {
				t.Fatalf("values = %#v, want %#v", values, test.values)
			}
			for _, fragment := range test.fragments {
				if !strings.Contains(test.bound.SQL, fragment) {
					t.Fatalf("SQL = %q, want fragment %q", test.bound.SQL, fragment)
				}
			}
			for _, argument := range test.bound.Args {
				if argument.Sensitive {
					t.Fatalf("non-sensitive argument %q was marked sensitive", argument.Name)
				}
			}
		})
	}
}

func TestBuiltinSkillMapperResultSemantics(t *testing.T) {
	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("query failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewBuiltinSkillMapper(executor).FindSkillByExternalID(context.Background(), "builtin_test")
		if !errors.Is(err, wantErr) {
			t.Fatalf("FindSkillByExternalID() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("single row zero result", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: builtinSkillMapperTestColumns()})
		_, err := NewBuiltinSkillMapper(executor).FindSkillByExternalID(context.Background(), "builtin_test")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindSkillByExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("single row scan and listed state", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: builtinSkillMapperTestColumns(),
			rows:    [][]driver.Value{builtinSkillMapperTestRow("builtin_test")},
		})
		row, err := NewBuiltinSkillMapper(executor).FindSkillByExternalID(context.Background(), "builtin_test")
		if err != nil || row.UUID != "00000000-0000-4000-8000-000000000001" || row.ExternalID != "builtin_test" || !row.Listed || !row.skill().Listed {
			t.Fatalf("FindSkillByExternalID() = (%+v, %v)", row, err)
		}

		unlistedRow := builtinSkillMapperTestRow("builtin_unlisted")
		unlistedRow[5] = false
		unlistedExecutor := newMapperTestExecutor(t, mapperTestResponse{
			columns: builtinSkillMapperTestColumns(),
			rows:    [][]driver.Value{unlistedRow},
		})
		found, err := NewBuiltinSkillMapper(unlistedExecutor).FindSkillByExternalID(context.Background(), "builtin_unlisted")
		if err != nil || found.Listed || found.skill().Listed {
			t.Fatalf("FindSkillByExternalID() = (%+v, %v), want Listed=false", found, err)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		values := builtinSkillMapperTestRow("builtin_test")
		values[7] = "not-a-timestamp"
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: builtinSkillMapperTestColumns(),
			rows:    [][]driver.Value{values},
		})
		_, err := NewBuiltinSkillMapper(executor).FindSkillByExternalID(context.Background(), "builtin_test")
		if err == nil {
			t.Fatal("FindSkillByExternalID() error = nil, want scan error")
		}
	})

	t.Run("many rows empty and populated", func(t *testing.T) {
		emptyExecutor := newMapperTestExecutor(t, mapperTestResponse{columns: builtinSkillVersionMapperTestColumns()})
		rows, err := NewBuiltinSkillMapper(emptyExecutor).ListVersionsPage(context.Background(), "11111111-1111-4111-8111-111111111111", 20, 0, false)
		if err != nil || len(rows) != 0 {
			t.Fatalf("ListVersionsPage() = (%+v, %v), want empty result", rows, err)
		}

		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: builtinSkillVersionMapperTestColumns(),
			rows: [][]driver.Value{
				builtinSkillVersionMapperTestRow("1.0.0"),
				builtinSkillVersionMapperTestRow("2.0.0"),
			},
		})
		rows, err = NewBuiltinSkillMapper(executor).ListVersionsPage(context.Background(), "11111111-1111-4111-8111-111111111111", 20, 0, false)
		if err != nil || len(rows) != 2 || rows[1].Version != "2.0.0" {
			t.Fatalf("ListVersionsPage() = (%+v, %v)", rows, err)
		}
	})

	t.Run("scalar zero and populated", func(t *testing.T) {
		emptyExecutor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"id"}})
		_, err := NewBuiltinSkillMapper(emptyExecutor).FindSkillUUIDByExternalID(context.Background(), "builtin_test")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindSkillUUIDByExternalID() error = %v, want sql.ErrNoRows", err)
		}

		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"count"},
			rows:    [][]driver.Value{{int64(3)}},
		})
		count, err := NewBuiltinSkillMapper(executor).CountSkills(context.Background())
		if err != nil || count != 3 {
			t.Fatalf("CountSkills() = (%d, %v)", count, err)
		}
	})
}

func builtinSkillMapperTestColumns() []string {
	return []string{
		"id", "uuid", "external_id", "display_title", "icon_key", "listed", "latest_version", "created_at", "updated_at", "deleted_at",
	}
}

func builtinSkillMapperTestRow(externalID string) []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		int64(7), "00000000-0000-4000-8000-000000000001", externalID, "Test", "blocks", true, "1.0.0", now, now, nil,
	}
}

func builtinSkillVersionMapperTestColumns() []string {
	return []string{
		"id", "uuid", "external_id", "skill_uuid", "skill_external_id", "version", "name", "description",
		"directory", "s3_bucket", "s3_key", "size_bytes", "sha256", "created_at", "deleted_at",
	}
}

func builtinSkillVersionMapperTestRow(version string) []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		int64(9), "00000000-0000-4000-8000-000000000002", "builtinver_test", "11111111-1111-4111-8111-111111111111",
		"builtin_test", version, "test", "Test", "test", "bucket", "key", int64(10), "sha", now, nil,
	}
}
