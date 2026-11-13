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

	if _, err := database.ExecContext(ctx, `UPDATE agents SET skills = '[]'::jsonb, current_version = 2 WHERE uuid = $1`, directAgentUUID); err != nil {
		t.Fatal(err)
	}

	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_versions (uuid, external_id, agent_external_id, version, name, model, skills, agent_created_at, agent_updated_at, workspace_uuid, agent_uuid)
		VALUES ($1, 'agent_ver_old', 'agent_direct', 1, 'Direct Agent', '{"id":"deepseek-flash"}', $2::jsonb, NOW(), NOW(), $3, $4)
	`, uuid.New().String(), `[{"type":"custom","skill_id":"skill_ref_check","version":"latest"}]`, workspaceUUID, directAgentUUID); err != nil {
		t.Fatal(err)
	}

	multiAgentUUID := uuid.New().String()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agents (uuid, external_id, workspace_uuid, current_version, name, model, skills)
		VALUES ($1, 'agent_multi', $2, 1, 'Multi Agent', '{"id":"deepseek-flash"}', '[]'::jsonb)
	`, multiAgentUUID, workspaceUUID); err != nil {
		t.Fatal(err)
	}
	for _, roster := range []string{
		`{"type":"coordinator","agents":[{"type":"agent","id":"agent_direct","version":1}]}`,
		`{"type":"multiagent_20261001","subagents":{"type":"enabled","predefined_agents":[{"type":"agent","id":"agent_direct","version":1}]}}`,
		`{"type":"multiagent_20261001","workflows":{"type":"enabled","predefined_agents":[{"type":"agent","id":"agent_direct","version":1}]}}`,
	} {
		if _, err := database.ExecContext(ctx, `UPDATE agents SET multiagent = $1::jsonb WHERE uuid = $2`, roster, multiAgentUUID); err != nil {
			t.Fatal(err)
		}
		_, _, err = store.SoftDeleteSkill(ctx, workspaceUUID, skillExternalID, true)
		inUse, ok := errors.AsType[*SkillInUseError](err)
		if !ok || len(inUse.Agents) != 1 || inUse.Agents[0].ExternalID != "agent_multi" {
			t.Fatalf("SoftDeleteSkill with pinned child = %v, want SkillInUseError for parent; roster=%s", err, roster)
		}
	}
	for _, roster := range []string{
		`{"type":"multiagent_20261001","subagents":{"type":"disabled","predefined_agents":[{"type":"agent","id":"agent_direct","version":1}]}}`,
		`{"type":"coordinator","agents":[{"type":"agent","id":"agent_direct","version":2}]}`,
		`null`,
	} {
		if _, err := database.ExecContext(ctx, `UPDATE agents SET multiagent = $1::jsonb WHERE uuid = $2`, roster, multiAgentUUID); err != nil {
			t.Fatal(err)
		}
		refs, err := NewSkillMapper(store.mapperDB).ListReferencingActiveAgents(ctx, workspaceUUID, skillExternalID)
		if err != nil || len(refs) != 0 {
			t.Fatalf("inactive or removed child reference must not block: %+v, %v", refs, err)
		}
	}
	if _, _, err := store.SoftDeleteSkill(ctx, workspaceUUID, skillExternalID, true); err != nil {
		t.Fatalf("SoftDeleteSkill after removing active ref failed: %v", err)
	}
}
