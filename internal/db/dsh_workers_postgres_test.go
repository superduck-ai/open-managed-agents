//go:build integration

package db

import (
	"errors"
	"os"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestDSHWorkersPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var forbidden int
	err := database.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='dsh_control_receipts') + (SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='dsh_worker_states' AND column_name IN ('status','model','trace_turns','model_control_sequence','model_control_input_id')) + (SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='dsh_public_receipts' AND column_name='payload')`).Scan(&forbidden)
	if err != nil || forbidden != 0 {
		t.Fatalf("obsolete schema=%d %v", forbidden, err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	id := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET current_worker_epoch=7 WHERE external_id=$1`, id)
	record, found, err := store.GetCodeSession(ctx, id)
	if err != nil || !found {
		t.Fatalf("load=%v,%v", found, err)
	}
	scope := DSHScope{CodeSessionID: id, OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID, CodeSessionUUID: record.UUID, Epoch: 7}
	old := scope
	old.Epoch = 6
	if _, err := store.RegisterDSHWorker(ctx, old); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("old register=%v", err)
	}
	if _, err := store.HeartbeatDSHWorker(ctx, scope); !errors.Is(err, ErrWorkerLeaseExpired) {
		t.Fatalf("unregistered heartbeat=%v", err)
	}
	if _, err := store.RegisterDSHWorker(ctx, scope); err != nil {
		t.Fatal(err)
	}

	t.Run("configuration freezes once across owner replacement", func(t *testing.T) {
		other := scope
		other.WorkspaceUUID = "53000000-0000-0000-0000-000000000099"
		if _, err := store.FreezeDSHContext(ctx, other, []byte(`{"model":"cross-tenant"}`)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross tenant snapshot=%v", err)
		}
		if _, err := store.FreezeDSHContext(ctx, old, []byte(`{"model":"stale"}`)); !errors.Is(err, ErrWorkerEpochMismatch) {
			t.Fatalf("stale snapshot=%v", err)
		}
		for _, invalid := range []string{`[]`, `null`} {
			if _, err := store.FreezeDSHContext(ctx, scope, []byte(invalid)); err == nil {
				t.Fatalf("invalid snapshot=%s", invalid)
			}
		}
		first, err := store.FreezeDSHContext(ctx, scope, []byte(`{"model":"frozen","config":{}}`))
		if err != nil {
			t.Fatal(err)
		}
		second, err := store.FreezeDSHContext(ctx, scope, []byte(`{"model":"changed"}`))
		if err != nil || string(first) != string(second) {
			t.Fatalf("snapshot changed=%s %s %v", first, second, err)
		}
		if _, err := store.RegisterDSHWorker(ctx, scope); err != nil {
			t.Fatal(err)
		}
		actual, err := store.GetDSHContextSnapshot(ctx, scope)
		if err != nil || string(actual) != string(first) {
			t.Fatalf("register changed snapshot=%s %v", actual, err)
		}

		execClaimTest(t, ctx, database, `UPDATE code_sessions SET current_worker_epoch=8 WHERE external_id=$1`, id)
		scope.Epoch = 8
		if _, err := store.RegisterDSHWorker(ctx, scope); err != nil {
			t.Fatal(err)
		}
		replacement, err := store.FreezeDSHContext(ctx, scope, []byte(`{"model":"replacement"}`))
		if err != nil || string(replacement) != string(first) {
			t.Fatalf("replacement snapshot=%s %v", replacement, err)
		}
		if _, err := store.HeartbeatDSHWorker(ctx, scope); err != nil {
			t.Fatal(err)
		}
		actual, err = store.GetDSHContextSnapshot(ctx, other)
		if err != nil || len(actual) != 0 {
			t.Fatalf("snapshot leaked=%s %v", actual, err)
		}
	})
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET worker_lease_expires_at=now()-interval '31 seconds' WHERE external_id=$1`, id)
	if _, err := store.HeartbeatDSHWorker(ctx, scope); !errors.Is(err, ErrWorkerLeaseExpired) {
		t.Fatalf("expired heartbeat=%v", err)
	}
	if _, err := store.RegisterDSHWorker(ctx, scope); err != nil {
		t.Fatal(err)
	}
}
