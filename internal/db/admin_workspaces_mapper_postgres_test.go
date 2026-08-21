package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestAdminWorkspaceMappersPostgreSQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires project config: %v", err)
	}
	database, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("open project database: %v", err)
	}
	defer database.Close()

	tx, err := database.mapperDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatalf("begin mapper fixture transaction: %v", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("roll back mapper fixture transaction: %v", rollbackErr)
		}
	}()

	execMapperFixtureSQL(t, ctx, tx, `
		CREATE TEMPORARY TABLE workspaces (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			uuid uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
			external_id text NOT NULL UNIQUE,
			organization_uuid uuid NOT NULL,
			name text NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL,
			archived_at timestamptz,
			compartment_id text NOT NULL,
			display_color text NOT NULL,
			data_residency jsonb NOT NULL,
			external_key_id text,
			tags jsonb NOT NULL
		) ON COMMIT DROP
	`)
	execMapperFixtureSQL(t, ctx, tx, `
		CREATE TEMPORARY TABLE workspace_members (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			uuid uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
			external_id text NOT NULL UNIQUE,
			organization_uuid uuid NOT NULL,
			workspace_uuid uuid NOT NULL,
			workspace_external_id text NOT NULL,
			user_uuid uuid NOT NULL,
			user_external_id text NOT NULL,
			workspace_role text NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL,
			deleted_at timestamptz
		) ON COMMIT DROP
	`)

	workspaceMapper := NewAdminWorkspaceMapper(tx)
	memberMapper := NewAdminWorkspaceMemberMapper(tx)
	organizationUUID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	workspaceUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	baseTime := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)

	t.Run("failure does not find or mutate unknown rows", func(t *testing.T) {
		_, findErr := workspaceMapper.FindByIdentifier(
			ctx,
			organizationUUID.String(),
			"wrkspc_missing",
			"",
		)
		if !errors.Is(findErr, sql.ErrNoRows) {
			t.Fatalf("FindByIdentifier() error = %v, want sql.ErrNoRows", findErr)
		}
		_, archiveErr := workspaceMapper.ArchiveByExternalID(
			ctx,
			organizationUUID.String(),
			"wrkspc_missing",
		)
		if !errors.Is(archiveErr, sql.ErrNoRows) {
			t.Fatalf("ArchiveByExternalID() error = %v, want sql.ErrNoRows", archiveErr)
		}
		_, memberFindErr := memberMapper.FindByUserExternalID(
			ctx,
			organizationUUID.String(),
			"wrkspc_missing",
			"user_missing",
		)
		if !errors.Is(memberFindErr, sql.ErrNoRows) {
			t.Fatalf("FindByUserExternalID() error = %v, want sql.ErrNoRows", memberFindErr)
		}
		_, deleteErr := memberMapper.SoftDeleteByUserExternalID(
			ctx,
			organizationUUID.String(),
			"wrkspc_missing",
			"user_missing",
		)
		if !errors.Is(deleteErr, sql.ErrNoRows) {
			t.Fatalf("SoftDeleteByUserExternalID() error = %v, want sql.ErrNoRows", deleteErr)
		}
	})

	t.Run("success preserves workspace JSON nullable values and cursor semantics", func(t *testing.T) {
		workspaceIDs := []uuid.UUID{
			uuid.MustParse("22222222-2222-4222-8222-222222222222"),
			uuid.MustParse("22222222-2222-4222-8222-222222222223"),
			uuid.MustParse("22222222-2222-4222-8222-222222222224"),
		}
		for index, id := range workspaceIDs {
			created, createErr := workspaceMapper.Insert(ctx, insertAdminWorkspaceParams{
				UUID:             id.String(),
				ExternalID:       "wrkspc_mapper_" + string(rune('a'+index)),
				OrganizationUUID: organizationUUID.String(),
				Name:             "Mapper workspace",
				CreatedAt:        baseTime.Add(time.Duration(index) * time.Minute),
				CompartmentID:    "compartment_mapper",
				DisplayColor:     "#123456",
				DataResidency:    json.RawMessage(`{"region":"us"}`),
				Tags:             json.RawMessage(`[{"key":"team","value":"platform"}]`),
			})
			if createErr != nil || created.UUID != id.String() || created.ExternalKeyID != nil {
				t.Fatalf("Insert() = (%+v, %v)", created, createErr)
			}
			assertAdminWorkspaceJSONField(t, created.DataResidency, "region", "us")
		}

		foundByExternalID, findErr := workspaceMapper.FindByIdentifier(
			ctx,
			organizationUUID.String(),
			"wrkspc_mapper_a",
			"",
		)
		if findErr != nil || foundByExternalID.UUID != workspaceUUID.String() {
			t.Fatalf("FindByIdentifier(external ID) = (%+v, %v)", foundByExternalID, findErr)
		}
		foundByUUID, findErr := workspaceMapper.FindByIdentifier(
			ctx,
			organizationUUID.String(),
			workspaceUUID.String(),
			workspaceUUID.String(),
		)
		if findErr != nil || foundByUUID.ExternalID != "wrkspc_mapper_a" {
			t.Fatalf("FindByIdentifier(UUID) = (%+v, %v)", foundByUUID, findErr)
		}

		listed, listErr := workspaceMapper.ListPage(ctx, organizationUUID.String(), false, nil, false, 2)
		if listErr != nil || len(listed) != 2 || listed[0].ExternalID != "wrkspc_mapper_c" {
			t.Fatalf("ListPage() = (%+v, %v)", listed, listErr)
		}
		anchor, found, anchorErr := workspaceMapper.FindPageAnchorByExternalID(
			ctx,
			organizationUUID.String(),
			"wrkspc_mapper_b",
		)
		if anchorErr != nil || !found {
			t.Fatalf("FindPageAnchorByExternalID() = (%+v, %t, %v)", anchor, found, anchorErr)
		}
		after, listErr := workspaceMapper.ListPage(ctx, organizationUUID.String(), false, &anchor, false, 2)
		if listErr != nil || len(after) != 1 || after[0].ExternalID != "wrkspc_mapper_a" {
			t.Fatalf("ListPage(after) = (%+v, %v)", after, listErr)
		}
		before, listErr := workspaceMapper.ListPage(ctx, organizationUUID.String(), false, &anchor, true, 2)
		if listErr != nil || len(before) != 1 || before[0].ExternalID != "wrkspc_mapper_c" {
			t.Fatalf("ListPage(before) = (%+v, %v)", before, listErr)
		}

		externalKeyID := "key_mapper"
		updated, updateErr := workspaceMapper.UpdateByExternalID(ctx, updateAdminWorkspaceParams{
			OrganizationUUID: organizationUUID.String(),
			ExternalID:       "wrkspc_mapper_a",
			Name:             "Updated workspace",
			DataResidency:    json.RawMessage(`{"region":"eu"}`),
			ExternalKeyID:    &externalKeyID,
			Tags:             json.RawMessage(`[{"key":"tier","value":"gold"}]`),
			UpdatedAt:        baseTime.Add(time.Hour),
		})
		if updateErr != nil || updated.ExternalKeyID == nil || *updated.ExternalKeyID != externalKeyID {
			t.Fatalf("UpdateByExternalID() = (%+v, %v)", updated, updateErr)
		}
		assertAdminWorkspaceJSONField(t, updated.DataResidency, "region", "eu")
		count, countErr := workspaceMapper.CountByExternalKeyID(
			ctx,
			organizationUUID.String(),
			externalKeyID,
		)
		if countErr != nil || count != 1 {
			t.Fatalf("CountByExternalKeyID() = (%d, %v), want 1, nil", count, countErr)
		}

		archived, archiveErr := workspaceMapper.ArchiveByExternalID(
			ctx,
			organizationUUID.String(),
			"wrkspc_mapper_a",
		)
		if archiveErr != nil || archived.ArchivedAt == nil {
			t.Fatalf("ArchiveByExternalID() = (%+v, %v)", archived, archiveErr)
		}
		active, listErr := workspaceMapper.ListPage(ctx, organizationUUID.String(), false, nil, false, 10)
		if listErr != nil || len(active) != 2 {
			t.Fatalf("ListPage(active) = (%+v, %v)", active, listErr)
		}
		all, listErr := workspaceMapper.ListPage(ctx, organizationUUID.String(), true, nil, false, 10)
		if listErr != nil || len(all) != 3 {
			t.Fatalf("ListPage(include archived) = (%+v, %v)", all, listErr)
		}
	})

	t.Run("success preserves member cursor update and soft delete semantics", func(t *testing.T) {
		userIDs := []uuid.UUID{
			uuid.MustParse("33333333-3333-4333-8333-333333333331"),
			uuid.MustParse("33333333-3333-4333-8333-333333333332"),
			uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		}
		for index, userUUID := range userIDs {
			letter := string(rune('a' + index))
			created, createErr := memberMapper.Insert(ctx, insertAdminWorkspaceMemberParams{
				ExternalID:          "wsm_mapper_" + letter,
				OrganizationUUID:    organizationUUID.String(),
				WorkspaceUUID:       workspaceUUID.String(),
				WorkspaceExternalID: "wrkspc_mapper_a",
				UserUUID:            userUUID.String(),
				UserExternalID:      "user_mapper_" + letter,
				WorkspaceRole:       "workspace_developer",
				CreatedAt:           baseTime.Add(time.Duration(index) * time.Minute),
			})
			if createErr != nil || created.UserUUID != userUUID.String() {
				t.Fatalf("Insert() = (%+v, %v)", created, createErr)
			}
		}

		listed, listErr := memberMapper.ListPage(
			ctx,
			organizationUUID.String(),
			workspaceUUID.String(),
			nil,
			false,
			2,
		)
		if listErr != nil || len(listed) != 2 || listed[0].UserExternalID != "user_mapper_c" {
			t.Fatalf("ListPage() = (%+v, %v)", listed, listErr)
		}
		anchor, found, anchorErr := memberMapper.FindPageAnchorByUserExternalID(
			ctx,
			workspaceUUID.String(),
			"user_mapper_b",
		)
		if anchorErr != nil || !found {
			t.Fatalf("FindPageAnchorByUserExternalID() = (%+v, %t, %v)", anchor, found, anchorErr)
		}
		after, listErr := memberMapper.ListPage(
			ctx,
			organizationUUID.String(),
			workspaceUUID.String(),
			&anchor,
			false,
			2,
		)
		if listErr != nil || len(after) != 1 || after[0].UserExternalID != "user_mapper_a" {
			t.Fatalf("ListPage(after) = (%+v, %v)", after, listErr)
		}
		before, listErr := memberMapper.ListPage(
			ctx,
			organizationUUID.String(),
			workspaceUUID.String(),
			&anchor,
			true,
			2,
		)
		if listErr != nil || len(before) != 1 || before[0].UserExternalID != "user_mapper_c" {
			t.Fatalf("ListPage(before) = (%+v, %v)", before, listErr)
		}

		updated, updateErr := memberMapper.UpdateRoleByUserExternalID(
			ctx,
			updateAdminWorkspaceMemberRoleParams{
				OrganizationUUID:    organizationUUID.String(),
				WorkspaceExternalID: "wrkspc_mapper_a",
				UserExternalID:      "user_mapper_a",
				WorkspaceRole:       "workspace_admin",
			},
		)
		if updateErr != nil || updated.WorkspaceRole != "workspace_admin" {
			t.Fatalf("UpdateRoleByUserExternalID() = (%+v, %v)", updated, updateErr)
		}
		deleted, deleteErr := memberMapper.SoftDeleteByUserExternalID(
			ctx,
			organizationUUID.String(),
			"wrkspc_mapper_a",
			"user_mapper_a",
		)
		if deleteErr != nil || deleted.UserExternalID != "user_mapper_a" {
			t.Fatalf("SoftDeleteByUserExternalID() = (%+v, %v)", deleted, deleteErr)
		}
		_, findErr := memberMapper.FindByUserExternalID(
			ctx,
			organizationUUID.String(),
			"wrkspc_mapper_a",
			"user_mapper_a",
		)
		if !errors.Is(findErr, sql.ErrNoRows) {
			t.Fatalf("FindByUserExternalID() after delete error = %v, want sql.ErrNoRows", findErr)
		}
		active, listErr := memberMapper.ListPage(
			ctx,
			organizationUUID.String(),
			workspaceUUID.String(),
			nil,
			false,
			10,
		)
		if listErr != nil || len(active) != 2 {
			t.Fatalf("ListPage() after delete = (%+v, %v)", active, listErr)
		}
	})
}

func assertAdminWorkspaceJSONField(t *testing.T, raw json.RawMessage, key, want string) {
	t.Helper()
	var value map[string]string
	if err := json.Unmarshal(raw, &value); err != nil || value[key] != want {
		t.Fatalf("JSON field %q = %q, want %q (raw: %s, error: %v)", key, value[key], want, raw, err)
	}
}
