//go:build integration

package db

import (
	"errors"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

func TestSoftDeleteSkillChecksActiveAgentReferences(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}

	orgUUID := uuid.New().String()
	workspaceUUID := uuid.New().String()
	if _, err := database.ExecContext(ctx, `INSERT INTO organizations (uuid, name) VALUES ($1, 'Test Org')`, orgUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO workspaces (uuid, organization_uuid, external_id, name) VALUES ($1, $2, 'wrkspc_test', 'Test WS')`, workspaceUUID, orgUUID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	skillExternalID := "skill_ref_check"
	latest := "1"
	if _, _, err := store.CreateSkillWithVersion(
		ctx,
		Skill{
			UUID:          uuid.New().String(),
			ExternalID:    skillExternalID,
			WorkspaceUUID: workspaceUUID,
			DisplayTitle:  nullableString("Ref Skill"),
			LatestVersion: &latest,
			CreatedAt:     now,
		},
		SkillVersion{
			UUID:            uuid.New().String(),
			ExternalID:      "skvr_ref_check",
			WorkspaceUUID:   workspaceUUID,
			SkillExternalID: skillExternalID,
			Version:         latest,
			Name:            "ref-skill",
			Description:     "desc",
			Directory:       "ref-skill",
			S3Bucket:        "bucket",
			S3Key:           "key",
			SizeBytes:       10,
			SHA256:          "sha",
			CreatedAt:       now,
		},
		false,
	); err != nil {
		t.Fatal(err)
	}

	directAgentUUID := uuid.New().String()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agents (uuid, external_id, workspace_uuid, current_version, name, model, skills)
		VALUES ($1, 'agent_direct', $2, 1, 'Direct Agent', '{"id":"deepseek-flash"}', $3::jsonb)
	`, directAgentUUID, workspaceUUID, `[{"type":"custom","skill_id":"skill_ref_check","version":"latest"}]`); err != nil {
		t.Fatal(err)
	}

	_, _, err := store.SoftDeleteSkill(ctx, workspaceUUID, skillExternalID, true)
	var inUse *SkillInUseError
	if !errors.As(err, &inUse) || len(inUse.Agents) != 1 || inUse.Agents[0].ExternalID != "agent_direct" {
		t.Fatalf("SoftDeleteSkill with direct agent ref = %v, want SkillInUseError for agent_direct", err)
	}

	if _, err := database.ExecContext(ctx, `UPDATE agents SET archived_at = NOW() WHERE uuid = $1`, directAgentUUID); err != nil {
		t.Fatal(err)
	}

	multiAgentUUID := uuid.New().String()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agents (uuid, external_id, workspace_uuid, current_version, name, model, skills, multiagent)
		VALUES ($1, 'agent_multi', $2, 1, 'Multi Agent', '{"id":"deepseek-flash"}', '[]'::jsonb, $3::jsonb)
	`, multiAgentUUID, workspaceUUID, `{"type":"predefined_agents","predefined_agents":[{"skills":[{"type":"custom","skill_id":"skill_ref_check","version":"latest"}]}]}`); err != nil {
		t.Fatal(err)
	}

	_, _, err = store.SoftDeleteSkill(ctx, workspaceUUID, skillExternalID, true)
	inUse = nil
	if !errors.As(err, &inUse) || len(inUse.Agents) != 1 || inUse.Agents[0].ExternalID != "agent_multi" {
		t.Fatalf("SoftDeleteSkill with multiagent ref = %v, want SkillInUseError for agent_multi", err)
	}

	if _, err := database.ExecContext(ctx, `UPDATE agents SET multiagent = NULL, current_version = 2 WHERE uuid = $1`, multiAgentUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_versions (uuid, external_id, agent_external_id, version, name, model, skills, agent_created_at, agent_updated_at, workspace_uuid, agent_uuid)
		VALUES ($1, 'agent_ver_old', 'agent_multi', 1, 'Multi Agent', '{"id":"deepseek-flash"}', $2::jsonb, NOW(), NOW(), $3, $4)
	`, uuid.New().String(), `[{"type":"custom","skill_id":"skill_ref_check","version":"latest"}]`, workspaceUUID, multiAgentUUID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.SoftDeleteSkill(ctx, workspaceUUID, skillExternalID, true); err != nil {
		t.Fatalf("SoftDeleteSkill after removing active ref failed: %v", err)
	}
}
