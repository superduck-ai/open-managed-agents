package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestModelsAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("models-bucket"))
	defer app.close()

	t.Run("failure missing api key", func(t *testing.T) {
		resp := app.do(t, http.MethodGet, "/v1/models?limit=1000", nil, "", false, "")
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("success list models", func(t *testing.T) {
		resp := app.do(t, http.MethodGet, "/v1/models?limit=1000", nil, defaultTestKey, false, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)

		data, ok := body["data"].([]any)
		if !ok || len(data) != 1 || body["has_more"] != false || body["first_id"] != "test/model" || body["last_id"] != "test/model" {
			t.Fatalf("models page = %#v", body)
		}

		first, _ := data[0].(map[string]any)
		if first["type"] != "model" || first["id"] != "test/model" || first["display_name"] != "Test model" {
			t.Fatalf("first model = %#v", first)
		}
	})
}

func TestModelCatalogPersistenceKeepsLastSuccessAfterFailure(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("model-catalog-persistence-bucket"))
	defer app.close()

	ctx := context.Background()
	succeededAt := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	failedAt := succeededAt.Add(time.Minute)
	models := json.RawMessage(`[{"id":"provider/model","display_name":"Provider Model"}]`)
	if err := app.db.SaveModelCatalogSuccess(ctx, db.ModelCatalogSnapshot{
		CatalogKey:    "persistence-test",
		Models:        models,
		LastAttemptAt: &succeededAt,
		LastSuccessAt: &succeededAt,
	}); err != nil {
		t.Fatalf("save model catalog success: %v", err)
	}
	if err := app.db.RecordModelCatalogFailure(ctx, "persistence-test", failedAt, "upstream_timeout"); err != nil {
		t.Fatalf("record model catalog failure: %v", err)
	}

	stored, exists, err := app.db.GetModelCatalogSnapshot(ctx, "persistence-test")
	if err != nil {
		t.Fatalf("get model catalog snapshot: %v", err)
	}
	if !exists {
		t.Fatal("model catalog snapshot does not exist")
	}
	assertRawJSONEqual(t, stored.Models, string(models))
	if stored.LastSuccessAt == nil || !stored.LastSuccessAt.Equal(succeededAt) {
		t.Fatalf("last_success_at = %v, want %v", stored.LastSuccessAt, succeededAt)
	}
	if stored.LastAttemptAt == nil || !stored.LastAttemptAt.Equal(failedAt) || stored.LastError != "upstream_timeout" {
		t.Fatalf("failure metadata = attempt:%v error:%q", stored.LastAttemptAt, stored.LastError)
	}
}
