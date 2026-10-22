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

func TestEvaluationDatasetsPostgres(t *testing.T) {
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
	if version < 95 {
		t.Fatalf("goose version = %d, want at least 95", version)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	now := time.Date(2026, time.October, 22, 1, 2, 3, 0, time.UTC)
	organizationUUID := "10000000-0000-4000-8000-000000000001"
	workspaceUUID := "20000000-0000-4000-8000-000000000002"
	otherWorkspaceUUID := "20000000-0000-4000-8000-000000000099"

	t.Run("empty name violates check", func(t *testing.T) {
		_, err := store.CreateEvaluationDataset(ctx, EvaluationDataset{
			UUID: "30000000-0000-4000-8000-000000000001", ExternalID: "evalds_empty",
			OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, CreatedAt: now, UpdatedAt: now,
			Version: EvaluationDatasetVersion{
				UUID: "31000000-0000-4000-8000-000000000001", ExternalID: "edv_empty", Version: 1, CreatedAt: now,
			},
		})
		if err == nil {
			t.Fatal("empty name insert succeeded")
		}
	})

	var dataset EvaluationDataset
	t.Run("create list and import", func(t *testing.T) {
		created, err := store.CreateEvaluationDataset(ctx, EvaluationDataset{
			UUID: "30000000-0000-4000-8000-000000000002", ExternalID: "evalds_billing",
			OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Name: "billing", Description: "nightly",
			CreatedByUserUUID: "40000000-0000-4000-8000-000000000004",
			CreatedAt:         now, UpdatedAt: now,
			Version: EvaluationDatasetVersion{
				UUID: "31000000-0000-4000-8000-000000000002", ExternalID: "edv_billing", Version: 1, CreatedAt: now,
			},
		})
		if err != nil || created.Version.ExternalID != "edv_billing" || created.Version.Version != 1 || created.Version.CaseCount != 0 {
			t.Fatalf("create = (%+v, %v)", created, err)
		}
		plain, err := store.CreateEvaluationDataset(ctx, EvaluationDataset{
			UUID: "30000000-0000-4000-8000-000000000003", ExternalID: "evalds_plain",
			OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Name: "plain",
			CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
			Version: EvaluationDatasetVersion{
				UUID: "31000000-0000-4000-8000-000000000003", ExternalID: "edv_plain", Version: 1, CreatedAt: now,
			},
		})
		if err != nil || plain.Version.Version != 1 {
			t.Fatalf("plain create = (%+v, %v)", plain, err)
		}
		cases, err := store.CreateEvaluationCases(ctx, organizationUUID, workspaceUUID, created.ExternalID, []EvaluationCaseWrite{
			{UUID: "50000000-0000-4000-8000-000000000001", ExternalID: "evalcase_one", Name: "one", Turns: []string{"hello"}, Checkpoints: json.RawMessage(`["done"]`)},
			{UUID: "50000000-0000-4000-8000-000000000002", ExternalID: "evalcase_two", Name: "two", Turns: []string{"again", "more"}, InputFiles: json.RawMessage(`[]`)},
		}, now)
		if err != nil || len(cases) != 2 || cases[0].Position != 1 || cases[1].Position != 2 || cases[0].VersionExternalID != "edv_billing" || string(cases[0].Checkpoints) != `["done"]` {
			t.Fatalf("import = (%+v, %v)", cases, err)
		}
		more, err := store.CreateEvaluationCases(ctx, organizationUUID, workspaceUUID, created.ExternalID, []EvaluationCaseWrite{
			{UUID: "50000000-0000-4000-8000-000000000003", ExternalID: "evalcase_three", Name: "three", Turns: []string{"third"}},
		}, now)
		if err != nil || len(more) != 1 || more[0].Position != 3 {
			t.Fatalf("second import = (%+v, %v)", more, err)
		}
		listed, _, err := store.ListEvaluationDatasetsPage(ctx, ListEvaluationDatasetsPageParams{
			OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Limit: 20,
		})
		if err != nil || len(listed) != 2 || listed[0].ExternalID != "evalds_billing" || listed[0].Version.CaseCount != 3 {
			t.Fatalf("datasets = (%+v, %v)", listed, err)
		}
		listedCases, _, err := store.ListEvaluationCasesPage(ctx, ListEvaluationCasesPageParams{
			OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, DatasetExternalID: created.ExternalID, Limit: 20,
		})
		if err != nil || len(listedCases) != 3 || listedCases[0].Turns[0] != "hello" || listedCases[2].Position != 3 {
			t.Fatalf("cases = (%+v, %v)", listedCases, err)
		}
		dataset = created
	})

	t.Run("update and delete the current version", func(t *testing.T) {
		updated, err := store.UpdateEvaluationCase(ctx, organizationUUID, workspaceUUID, dataset.ExternalID, "evalcase_one", EvaluationCaseWrite{
			Name: "one edited", Description: "note", Turns: []string{"hello again"}, Checkpoints: json.RawMessage(`{"ready":true}`),
			InputFiles: json.RawMessage(`[{"name":"brief.txt","file_id":"file_brief"}]`),
		}, now.Add(time.Minute))
		if err != nil || updated.Name != "one edited" || updated.Position != 1 || updated.Turns[0] != "hello again" || string(updated.InputFiles) != `[{"name":"brief.txt","file_id":"file_brief"}]` || updated.VersionExternalID != "edv_billing" {
			t.Fatalf("update = (%+v, %v)", updated, err)
		}
		deleted, err := store.DeleteEvaluationCase(ctx, organizationUUID, workspaceUUID, dataset.ExternalID, "evalcase_three")
		if err != nil || deleted.ExternalID != "evalcase_three" || deleted.Name != "three" {
			t.Fatalf("delete = (%+v, %v)", deleted, err)
		}
		if _, err := store.DeleteEvaluationCase(ctx, organizationUUID, workspaceUUID, dataset.ExternalID, "evalcase_three"); !errors.Is(err, ErrEvaluationCaseMissing) {
			t.Fatalf("second delete = %v", err)
		}
		listed, _, err := store.ListEvaluationDatasetsPage(ctx, ListEvaluationDatasetsPageParams{
			OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Limit: 20,
		})
		if err != nil || len(listed) != 2 || listed[0].Version.CaseCount != 2 {
			t.Fatalf("case count = (%+v, %v)", listed, err)
		}
	})

	t.Run("other workspace cannot read dataset", func(t *testing.T) {
		_, err := store.GetEvaluationDataset(ctx, organizationUUID, otherWorkspaceUUID, dataset.ExternalID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("other workspace get = %v, want ErrNotFound", err)
		}
	})

	t.Run("archived dataset is hidden", func(t *testing.T) {
		if _, err := database.ExecContext(ctx, `update evaluation_datasets set archived_at = now() where external_id = $1`, dataset.ExternalID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetEvaluationDataset(ctx, organizationUUID, workspaceUUID, dataset.ExternalID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("archived get = %v, want ErrNotFound", err)
		}
		listed, _, err := store.ListEvaluationDatasetsPage(ctx, ListEvaluationDatasetsPageParams{
			OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, Limit: 20,
		})
		if err != nil || len(listed) != 1 || listed[0].ExternalID != "evalds_plain" {
			t.Fatalf("archived list = (%+v, %v)", listed, err)
		}
	})
}
