//go:build integration

package db

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestEvaluationEvaluatorsPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := database.QueryRowContext(ctx, `select max(version_id) from goose_db_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 96 {
		t.Fatalf("goose version = %d, want at least 96", version)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	organizationUUID := "10000000-0000-4000-8000-000000000001"
	workspaceUUID := "20000000-0000-4000-8000-000000000002"
	otherWorkspaceUUID := "20000000-0000-4000-8000-000000000099"

	t.Run("llm without model violates check", func(t *testing.T) {
		_, err := store.CreateEvaluationEvaluator(ctx, EvaluationEvaluator{
			UUID: "40000000-0000-4000-8000-000000000001", ExternalID: "evalev_empty",
			OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Name: "score", CreatedAt: now, UpdatedAt: now,
			Version: EvaluationEvaluatorVersion{
				UUID: "41000000-0000-4000-8000-000000000001", ExternalID: "eev_empty", Version: 1,
				EvaluatorType: "llm", PromptTemplate: "Score {{input}}", Variables: []string{"input"}, CreatedAt: now,
			},
		})
		if err == nil {
			t.Fatal("llm without model insert succeeded")
		}
	})

	llm, err := store.CreateEvaluationEvaluator(ctx, EvaluationEvaluator{
		UUID: "40000000-0000-4000-8000-000000000002", ExternalID: "evalev_llm",
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Name: "llm score", Description: "nightly",
		CreatedAt: now, UpdatedAt: now,
		Version: EvaluationEvaluatorVersion{
			UUID: "41000000-0000-4000-8000-000000000002", ExternalID: "eev_llm", Version: 1,
			EvaluatorType: "llm", Model: "claude", PromptTemplate: "Score {{input}}", Variables: []string{"input"}, CreatedAt: now,
		},
	})
	if err != nil || llm.Version.ExternalID != "eev_llm" || llm.Version.Version != 1 || llm.Version.Model != "claude" {
		t.Fatalf("create llm = (%+v, %v)", llm, err)
	}
	agent, err := store.CreateEvaluationEvaluator(ctx, EvaluationEvaluator{
		UUID: "40000000-0000-4000-8000-000000000003", ExternalID: "evalev_agent",
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Name: "agent score",
		CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
		Version: EvaluationEvaluatorVersion{
			UUID: "41000000-0000-4000-8000-000000000003", ExternalID: "eev_agent", Version: 1,
			EvaluatorType: "agent", AgentID: "agent_score", Variables: []string{"invocation_result"}, CreatedAt: now.Add(time.Minute),
		},
	})
	if err != nil || agent.Version.AgentID != "agent_score" || len(agent.Version.Variables) != 1 {
		t.Fatalf("create agent = (%+v, %v)", agent, err)
	}

	next, err := store.AppendEvaluationEvaluatorVersion(ctx, organizationUUID, workspaceUUID, llm.ExternalID, EvaluationEvaluatorVersion{
		UUID: "41000000-0000-4000-8000-000000000004", ExternalID: "eev_llm_v2",
		EvaluatorType: "llm", Model: "claude", PromptTemplate: "Score {{input}} again", Variables: []string{"input"}, CreatedAt: now.Add(time.Minute),
	})
	if err != nil || next.Version.Version != 2 || next.Version.ExternalID != "eev_llm_v2" || next.Version.PromptTemplate != "Score {{input}} again" || next.ExternalID != "evalev_llm" {
		t.Fatalf("append version = (%+v, %v)", next, err)
	}
	current, err := store.GetEvaluationEvaluator(ctx, organizationUUID, workspaceUUID, llm.ExternalID)
	if err != nil || current.Version.Version != 2 || current.Version.ExternalID != "eev_llm_v2" {
		t.Fatalf("latest = (%+v, %v)", current, err)
	}
	frozen, err := store.GetEvaluationEvaluatorVersion(ctx, organizationUUID, workspaceUUID, "eev_llm")
	if err != nil || frozen.Version != 1 || frozen.PromptTemplate != "Score {{input}}" {
		t.Fatalf("frozen version = (%+v, %v)", frozen, err)
	}

	listed, _, err := store.ListEvaluationEvaluatorsPage(ctx, ListEvaluationEvaluatorsPageParams{
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Limit: 20,
	})
	if err != nil || len(listed) != 2 || listed[0].ExternalID != "evalev_agent" || listed[1].ExternalID != "evalev_llm" {
		t.Fatalf("list = (%+v, %v)", listed, err)
	}
	if _, err := store.GetEvaluationEvaluator(ctx, organizationUUID, otherWorkspaceUUID, "evalev_llm"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other workspace = %v", err)
	}
	if _, err := database.ExecContext(ctx, `update evaluation_evaluators set archived_at = now() where external_id = 'evalev_agent'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetEvaluationEvaluator(ctx, organizationUUID, workspaceUUID, "evalev_agent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archived get = %v", err)
	}
	listed, _, err = store.ListEvaluationEvaluatorsPage(ctx, ListEvaluationEvaluatorsPageParams{
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Limit: 20,
	})
	if err != nil || len(listed) != 1 || listed[0].ExternalID != "evalev_llm" {
		t.Fatalf("list after archive = (%+v, %v)", listed, err)
	}
}
