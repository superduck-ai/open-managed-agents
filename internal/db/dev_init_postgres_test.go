package db

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/yourbatis"
	"uuid"
)

func TestDevIdentityPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	input := devIdentityFixture()
	t.Run("failure rolls back the complete identity", func(t *testing.T) {
		invalid := input
		invalid.Workspaces = append([]AdminWorkspace(nil), input.Workspaces...)
		invalid.Workspaces[1].ExternalID = invalid.Workspaces[0].ExternalID
		if err := store.CreateDevIdentity(ctx, invalid); err == nil {
			t.Fatal("duplicate workspace identity was accepted")
		}
		if _, err := store.GetAdminOrganization(ctx, uuid.MustParse(input.OrganizationUUID)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("organization survived rollback: %v", err)
		}
	})
	t.Run("success resumes committed identity and preserves revocation", func(t *testing.T) {
		if err := store.CreateDevIdentity(ctx, input); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateDevIdentity(ctx, input); err != nil {
			t.Fatalf("retry after commit: %v", err)
		}
		user, err := store.GetAdminUser(ctx, input.OrganizationUUID, input.User.ExternalID)
		if err != nil {
			t.Fatal(err)
		}
		workspace := input.Workspaces[1]
		if _, err := NewAdminWorkspaceMemberMapper(store.mapperDB).FindByUserExternalID(ctx, input.OrganizationUUID, workspace.ExternalID, user.ExternalID); err != nil {
			t.Fatalf("secondary membership missing: %v", err)
		}
		workspace = input.Workspaces[0]
		created, err := store.CreateConsoleAPIKeyWithRawKey(ctx, platform.CreateConsoleAPIKeyInput{
			OrgUUID: input.OrganizationUUID, WorkspaceUUID: workspace.UUID, WorkspaceDisplayID: workspace.ExternalID,
			Name: "key", CreatedByUserUUID: &user.UUID,
		}, "apikey_pending", "sk-ant-api03-pending")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetAdminAPIKey(ctx, input.OrganizationUUID, "apikey_pending"); err != nil {
			t.Fatalf("pending key cannot be found after commit: %v", err)
		}
		if _, err := store.UpdateConsoleAPIKeyStatus(ctx, platform.UpdateConsoleAPIKeyStatusInput{
			OrgUUID: input.OrganizationUUID, WorkspaceUUID: workspace.UUID, APIKeyID: created.APIKey.ID, Status: "archived",
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.Seed(ctx, config.BootstrapConfig{
			OrganizationName: input.OrganizationName, WorkspaceExternalID: workspace.ExternalID, WorkspaceName: workspace.Name,
			UserExternalID: user.ExternalID, UserEmail: user.Email, UserName: user.Name,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetAPIKey(ctx, auth.HashAPIKey(created.RawKey)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("startup restored a revoked key: %v", err)
		}
		keys, err := store.ListConsoleAPIKeys(ctx, input.OrganizationUUID, &workspace.UUID)
		if err != nil || len(keys) != 1 || keys[0].Status != "archived" {
			t.Fatalf("Console key state diverged: %+v, %v", keys, err)
		}
	})
}

func devIdentityFixture() DevIdentity {
	input := DevIdentity{
		OrganizationUUID: uuid.NewV4().String(), OrganizationName: "OMA Test",
		User: AdminUser{ExternalID: "user_devinit_test", Email: "test@oma.ai", Name: "Test", Role: "admin"},
	}
	for index, name := range []string{"default", "secondary"} {
		input.Workspaces = append(input.Workspaces, AdminWorkspace{
			UUID: uuid.NewV4().String(), ExternalID: "workspace_" + name, Name: name,
			OrganizationUUID: input.OrganizationUUID, IsDefault: index == 0, CreatedAt: time.Now().UTC(),
			DataResidency: json.RawMessage(`{}`), Tags: json.RawMessage(`{}`),
		})
	}
	return input
}

func TestDevOrganizationInsertBinding(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: adminOrganizationMapperInsertStatement,
		bound:     buildAdminOrganizationMapperInsert(yourbatis.DialectPostgres, "org-uuid", "OMA"),
		wantID:    "AdminOrganizationMapper.Insert", wantKind: yourbatis.StatementInsert,
		wantArgumentNames: []string{"organizationUUID", "name"},
		wantSQLFragments:  []string{"INSERT INTO organizations (uuid, name)", "VALUES ($1, $2)"},
	})
}
