package tests

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
)

func installWebhookMutationFailure(t *testing.T, app *testApp, table, operation, condition string) func() {
	t.Helper()
	identifier := pgx.Identifier{"test_webhook_mutation_" + uuid.NewV4().String()}.Sanitize()
	tableName := pgx.Identifier{table}.Sanitize()
	_, err := app.pool.Exec(t.Context(), `CREATE FUNCTION `+identifier+`() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF `+condition+` THEN RAISE EXCEPTION 'test mutation rejected'; END IF;
 IF TG_OP = 'DELETE' THEN RETURN OLD; END IF; RETURN NEW; END $$;
 CREATE TRIGGER `+identifier+` AFTER `+operation+` ON `+tableName+` FOR EACH ROW EXECUTE FUNCTION `+identifier+`() `)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	remove := func() {
		once.Do(func() {
			if _, err := app.pool.Exec(context.Background(), `DROP TRIGGER `+identifier+` ON `+tableName+`; DROP FUNCTION `+identifier+`() `); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(remove)
	return remove
}

// Keep payload validation at the wire boundary: only identifiers belong in resource events.
func assertResourceWebhookPayload(t *testing.T, body []byte) {
	t.Helper()
	var event struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatal(err)
	}
	if len(event.Data) != 4 {
		t.Fatalf("unexpected resource payload fields: %v", event.Data)
	}
	for _, key := range []string{"type", "id", "organization_id", "workspace_id"} {
		if _, ok := event.Data[key]; !ok {
			t.Fatalf("missing payload field: %s", key)
		}
	}
}
