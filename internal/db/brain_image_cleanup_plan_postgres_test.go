//go:build integration

package db

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

type brainImageCleanupPlan struct {
	Relation string                  `json:"Relation Name"`
	Rows     float64                 `json:"Actual Rows"`
	Loops    float64                 `json:"Actual Loops"`
	Removed  float64                 `json:"Rows Removed by Filter"`
	Plans    []brainImageCleanupPlan `json:"Plans"`
}

func TestBrainImageCleanupDeletedHistoryPlan(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	execClaimTest(t, ctx, database, `INSERT INTO sessions (
        external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
        environment_uuid, environment_external_id, agent_uuid, agent_external_id,
        agent_version, agent_snapshot, status, deleted_at)
        SELECT 'sesn_deleted_history_' || n, organization_uuid, workspace_uuid, created_by_api_key_uuid,
        environment_uuid, environment_external_id, agent_uuid, agent_external_id,
        agent_version, agent_snapshot, 'idle', now()
        FROM sessions CROSS JOIN generate_series(1, 20000) n WHERE external_id = 'sesn_claim_1'`)
	execClaimTest(t, ctx, database, `INSERT INTO brain_image_attachments (
        external_id, organization_uuid, workspace_uuid, session_uuid, sha256,
        media_type, size_bytes, bucket, object_key, state)
        SELECT 'bimg_plan_' || n, organization_uuid, workspace_uuid, uuid, lpad(to_hex(n), 64, '0'),
        'image/png', 42, 'images', 'images/plan/' || n, 'attached'
        FROM sessions CROSS JOIN generate_series(1, 20000) n WHERE external_id = 'sesn_claim_1'`)
	execClaimTest(t, ctx, database, "VACUUM ANALYZE sessions")
	execClaimTest(t, ctx, database, "VACUUM ANALYZE brain_image_attachments")
	bound := buildBrainImageAttachmentMapperScheduleCleanup(yourbatis.DialectPostgres, 10)
	var encoded []byte
	if err := database.QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+bound.SQL, bound.Values()...).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var plans []struct {
		Plan         brainImageCleanupPlan `json:"Plan"`
		Milliseconds float64               `json:"Execution Time"`
	}
	if err := json.Unmarshal(encoded, &plans); err != nil {
		t.Fatal(err)
	}
	var visited float64
	var lookups float64
	var walk func(brainImageCleanupPlan)
	walk = func(plan brainImageCleanupPlan) {
		if plan.Relation == "sessions" {
			visited += (plan.Rows + plan.Removed) * plan.Loops
			lookups += plan.Loops
		}
		for _, child := range plan.Plans {
			walk(child)
		}
	}
	walk(plans[0].Plan)
	t.Logf("deleted history=20000, active images=20000, session rows visited=%.0f, session lookups=%.0f, execution_ms=%.3f", visited, lookups, plans[0].Milliseconds)
	if visited > 1 || lookups > 1 {
		t.Fatalf("cleanup scanned deleted history unrelated to the sole image owner: %.0f rows", visited)
	}
}
