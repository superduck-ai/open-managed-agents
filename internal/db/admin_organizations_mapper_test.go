package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestAdminOrganizationMapperFindByUUID(t *testing.T) {
	organizationUUID := "22222222-2222-4222-8222-222222222222"
	wantValues := []any{organizationUUID}

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("find organization failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewAdminOrganizationMapper(executor).FindByUUID(context.Background(), organizationUUID)
		if !errors.Is(err, wantErr) {
			t.Fatalf("FindByUUID() error = %v, want query error", err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminOrganizationMapper.FindByUUID",
			yourbatis.StatementSelect,
			wantValues,
		)
	})

	t.Run("not found", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminOrganizationMapperTestColumns(),
		})
		organization, err := NewAdminOrganizationMapper(executor).FindByUUID(
			context.Background(),
			organizationUUID,
		)
		if !errors.Is(err, sql.ErrNoRows) || organization.UUID != "" {
			t.Fatalf("FindByUUID() = (%+v, %v), want zero and sql.ErrNoRows", organization, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminOrganizationMapper.FindByUUID",
			yourbatis.StatementSelect,
			wantValues,
			"WHERE uuid = $1",
		)
		if strings.Contains(executor.bound.SQL, "CAST(") {
			t.Fatalf("generated SQL contains UUID cast ceremony: %q", executor.bound.SQL)
		}
	})

	t.Run("found", func(t *testing.T) {
		createdAt := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminOrganizationMapperTestColumns(),
			rows: [][]driver.Value{{
				organizationUUID,
				"Mapper organization",
				createdAt,
			}},
		})
		organization, err := NewAdminOrganizationMapper(executor).FindByUUID(
			context.Background(),
			organizationUUID,
		)
		if err != nil || organization.UUID != organizationUUID || organization.Name != "Mapper organization" {
			t.Fatalf("FindByUUID() = (%+v, %v)", organization, err)
		}
		assertMapperTestExecution(
			t,
			executor,
			"AdminOrganizationMapper.FindByUUID",
			yourbatis.StatementSelect,
			wantValues,
			"SELECT uuid, name, created_at",
		)
	})
}

func adminOrganizationMapperTestColumns() []string {
	return []string{"uuid", "name", "created_at"}
}
