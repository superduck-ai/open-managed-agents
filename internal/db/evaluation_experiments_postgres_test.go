//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestEvaluationExperimentsPostgres(t *testing.T) {
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
	if version < 99 {
		t.Fatalf("goose version = %d, want at least 99", version)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	organizationUUID := "10000000-0000-4000-8000-000000000001"
	workspaceUUID := "20000000-0000-4000-8000-000000000002"
	otherWorkspaceUUID := "20000000-0000-4000-8000-000000000099"

	_, err := store.CreateEvaluationExperiment(ctx, experimentCreate(organizationUUID, workspaceUUID, "nightly", now, 0))
	if err == nil {
		t.Fatal("timeout 0 insert succeeded")
	}
	first, err := store.CreateEvaluationExperiment(ctx, experimentCreate(organizationUUID, workspaceUUID, "nightly", now, 60))
	if err != nil || first.ExternalID != "evalexp_one" || first.ItemCount != 1 || first.EvaluatorCount != 1 || string(first.Env) == "" {
		t.Fatalf("create = (%+v, %v)", first, err)
	}
	if _, err := store.CreateEvaluationExperiment(ctx, experimentCreate(organizationUUID, workspaceUUID, "nightly", now.Add(time.Minute), 60)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate name = %v", err)
	}
	secondInput := experimentCreate(organizationUUID, workspaceUUID, "later", now.Add(time.Minute), 120)
	secondInput.Experiment.ExternalID = "evalexp_two"
	secondInput.Experiment.UUID = "50000000-0000-4000-8000-000000000002"
	secondInput.Experiment.Status = EvaluationExperimentStatusCompleted
	secondInput.Bindings[0].UUID = "51000000-0000-4000-8000-000000000002"
	secondInput.Bindings[0].ExternalID = "evalbind_two"
	secondInput.Bindings[0].ExperimentUUID = secondInput.Experiment.UUID
	secondInput.Items[0].UUID = "52000000-0000-4000-8000-000000000002"
	secondInput.Items[0].ExternalID = "evalitem_two"
	secondInput.Items[0].ExperimentUUID = secondInput.Experiment.UUID
	second, err := store.CreateEvaluationExperiment(ctx, secondInput)
	if err != nil || second.Status != EvaluationExperimentStatusCompleted {
		t.Fatalf("second = (%+v, %v)", second, err)
	}
	if _, err := store.GetEvaluationExperiment(ctx, organizationUUID, otherWorkspaceUUID, "evalexp_one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other workspace = %v", err)
	}
	if err := store.SetEvaluationExperimentItemSession(ctx, EvaluationExperimentItemSession{
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, ExternalID: "evalitem_one",
		SessionExternalID: "sesn_case", Status: EvaluationExperimentStatusRunning, UpdatedAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateEvaluationExperimentStatus(ctx, organizationUUID, workspaceUUID, "evalexp_one", EvaluationExperimentStatusRunning, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	runningItems, err := store.ListRunningEvaluationExperimentItemsBySession(ctx, organizationUUID, workspaceUUID, "sesn_case")
	if err != nil || len(runningItems) != 1 || runningItems[0].ExperimentExternalID != "evalexp_one" || runningItems[0].Item.ExternalID != "evalitem_one" {
		t.Fatalf("running items = (%+v, %v)", runningItems, err)
	}
	otherItems, err := store.ListRunningEvaluationExperimentItemsBySession(ctx, organizationUUID, otherWorkspaceUUID, "sesn_case")
	if err != nil || len(otherItems) != 0 {
		t.Fatalf("other workspace items = (%+v, %v)", otherItems, err)
	}
	finished, err := store.FinishRunningEvaluationExperimentItem(ctx, organizationUUID, workspaceUUID, "evalitem_one", EvaluationExperimentStatusCompleted, "", now.Add(3*time.Minute))
	if err != nil || !finished {
		t.Fatalf("finish = (%v, %v)", finished, err)
	}
	finishedAgain, err := store.FinishRunningEvaluationExperimentItem(ctx, organizationUUID, workspaceUUID, "evalitem_one", EvaluationExperimentStatusFailed, "late", now.Add(4*time.Minute))
	if err != nil || finishedAgain {
		t.Fatalf("second finish = (%v, %v)", finishedAgain, err)
	}
	missingScore, err := store.CreateEvaluationExperimentScore(ctx, EvaluationExperimentScore{
		UUID: "53000000-0000-4000-8000-000000000001", ExternalID: "evalscore_missing",
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, ExperimentUUID: first.UUID,
		ItemUUID: "52000000-0000-4000-8000-000000000001", ItemExternalID: "evalitem_one",
		EvaluatorUUID: "40000000-0000-4000-8000-000000000001", EvaluatorExternalID: "evalev_test",
		VersionUUID: "41000000-0000-4000-8000-000000000001", VersionExternalID: "eev_test",
		Position: 1, Status: EvaluationExperimentScoreStatusCompleted, CreatedAt: now.Add(5 * time.Minute),
	})
	if err == nil {
		t.Fatalf("completed score without a value = %+v", missingScore)
	}
	scoreValue := 0.75
	score, err := store.CreateEvaluationExperimentScore(ctx, EvaluationExperimentScore{
		UUID: "53000000-0000-4000-8000-000000000002", ExternalID: "evalscore_one",
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, ExperimentUUID: first.UUID,
		ItemUUID: "52000000-0000-4000-8000-000000000001", ItemExternalID: "evalitem_one",
		EvaluatorUUID: "40000000-0000-4000-8000-000000000001", EvaluatorExternalID: "evalev_test",
		VersionUUID: "41000000-0000-4000-8000-000000000001", VersionExternalID: "eev_test",
		Position: 1, Status: EvaluationExperimentScoreStatusCompleted, Score: &scoreValue, Explanation: "matches",
		Details: json.RawMessage(`{"pass":true}`), CreatedAt: now.Add(5 * time.Minute),
	})
	if err != nil || score.ExternalID != "evalscore_one" || score.Score == nil || *score.Score != 0.75 {
		t.Fatalf("score = (%+v, %v)", score, err)
	}
	if _, err := store.CreateEvaluationExperimentScore(ctx, EvaluationExperimentScore{
		UUID: "53000000-0000-4000-8000-000000000003", ExternalID: "evalscore_dup",
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, ExperimentUUID: first.UUID,
		ItemUUID: "52000000-0000-4000-8000-000000000001", ItemExternalID: "evalitem_one",
		EvaluatorUUID: "40000000-0000-4000-8000-000000000001", EvaluatorExternalID: "evalev_test",
		VersionUUID: "41000000-0000-4000-8000-000000000001", VersionExternalID: "eev_test",
		Position: 1, Status: EvaluationExperimentScoreStatusFailed, ErrorMessage: "again", CreatedAt: now.Add(6 * time.Minute),
	}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate score = %v", err)
	}
	loaded, err := store.GetEvaluationExperiment(ctx, organizationUUID, workspaceUUID, "evalexp_one")
	if err != nil || loaded.Status != EvaluationExperimentStatusRunning || loaded.Items[0].SessionExternalID != "sesn_case" || loaded.SessionCount != 1 || len(loaded.Scores) != 1 || loaded.Scores[0].ExternalID != "evalscore_one" {
		t.Fatalf("loaded = (%+v, %v)", loaded, err)
	}
	var env map[string]string
	if err := json.Unmarshal(loaded.Env, &env); err != nil || env["TRACE"] != "1" {
		t.Fatalf("env = %s err=%v", loaded.Env, err)
	}
	matches, err := store.ListCompletedEvaluationExperiments(ctx, organizationUUID, workspaceUUID, "edv_test", loaded.EvaluatorSetKey)
	if err != nil || len(matches) != 1 || matches[0].ExternalID != "evalexp_two" || len(matches[0].Bindings) != 1 {
		t.Fatalf("compare = (%+v, %v)", matches, err)
	}
	listed, _, err := store.ListEvaluationExperimentsPage(ctx, ListEvaluationExperimentsPageParams{
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Limit: 20,
	})
	if err != nil || len(listed) != 2 || listed[0].ExternalID != "evalexp_two" || listed[1].ExternalID != "evalexp_one" || len(listed[0].Items) != 0 {
		t.Fatalf("list = (%+v, %v)", listed, err)
	}
}

func experimentCreate(organizationUUID, workspaceUUID, name string, now time.Time, timeout int) EvaluationExperimentCreate {
	experimentUUID := "50000000-0000-4000-8000-000000000001"
	return EvaluationExperimentCreate{
		Experiment: EvaluationExperiment{
			UUID: experimentUUID, ExternalID: "evalexp_one", OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID,
			Name: name, Description: "note", DatasetUUID: "30000000-0000-4000-8000-000000000001", DatasetExternalID: "evalds_test",
			VersionUUID: "31000000-0000-4000-8000-000000000001", VersionExternalID: "edv_test",
			AgentExternalID: "agent_test", AgentVersion: 4, EnvironmentExternalID: "env_test", TimeoutSeconds: timeout,
			Env: json.RawMessage(`{"TRACE":"1"}`), EvaluatorSetKey: "eev_test", Status: EvaluationExperimentStatusPending,
			CreatedAt: now, UpdatedAt: now,
		},
		Bindings: []EvaluationExperimentBinding{{
			UUID: "51000000-0000-4000-8000-000000000001", ExternalID: "evalbind_one",
			OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, ExperimentUUID: experimentUUID,
			Position: 1, EvaluatorUUID: "40000000-0000-4000-8000-000000000001", EvaluatorExternalID: "evalev_test",
			VersionUUID: "41000000-0000-4000-8000-000000000001", VersionExternalID: "eev_test", CreatedAt: now,
		}},
		Items: []EvaluationExperimentItem{{
			UUID: "52000000-0000-4000-8000-000000000001", ExternalID: "evalitem_one",
			OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, ExperimentUUID: experimentUUID,
			CaseUUID: "32000000-0000-4000-8000-000000000001", CaseExternalID: "evalcase_test", Position: 1,
			Status: EvaluationExperimentStatusPending, CreatedAt: now, UpdatedAt: now,
		}},
	}
}
