//go:build integration

package db

import (
	"errors"
	"os"
	"testing"
	"time"

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
	err := database.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='dsh_control_receipts') + (SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='dsh_worker_states' AND column_name='status') + (SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='dsh_public_receipts' AND column_name='payload')`).Scan(&forbidden)
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
	model := "selected"
	traces := []byte(`[{"sessionId":"root","traceId":"4bf92f3577b34da6a3ce929d0e0e4736","spanId":"00f067aa0ba902b7","startMs":0}]`)
	state, err := store.PatchDSHWorkerState(ctx, scope, DSHWorkerPatch{ControlInputID: "initial", ControlSequence: "1", ModelSet: true, Model: &model, TraceTurnsSet: true, TraceTurns: traces})
	if err != nil || state.Model == nil || *state.Model != "selected" || len(state.TraceTurns) == 0 {
		t.Fatalf("patch=%+v,%v", state, err)
	}
	if _, err := store.RegisterDSHWorker(ctx, scope); err != nil {
		t.Fatal(err)
	}
	state, err = store.GetDSHWorkerState(ctx, scope)
	if err != nil || state.Model == nil || *state.Model != "selected" {
		t.Fatalf("register reset state=%+v,%v", state, err)
	}
	if expiry, err := store.HeartbeatDSHWorker(ctx, scope); err != nil || !expiry.After(time.Now()) {
		t.Fatalf("heartbeat=%v,%v", expiry, err)
	}
	state, err = store.PatchDSHWorkerState(ctx, scope, DSHWorkerPatch{ModelSet: true, ControlInputID: "clear", ControlSequence: "2", TraceTurnsSet: true})
	if err != nil || state.Model != nil || state.TraceTurns != nil {
		t.Fatalf("clear=%+v,%v", state, err)
	}
	t.Run("model controls are durable and ordered across replay", func(t *testing.T) {
		for _, sequence := range []string{"", "0", "01", "18446744073709551616"} {
			if _, err := store.PatchDSHWorkerState(ctx, scope, DSHWorkerPatch{ModelSet: true, ControlInputID: "bad", ControlSequence: sequence}); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("invalid sequence=%q %v", sequence, err)
			}
		}
		first, second := "model-a", "model-b"
		a := DSHWorkerPatch{ModelSet: true, Model: &first, ControlInputID: "control-a", ControlSequence: "10"}
		b := DSHWorkerPatch{ModelSet: true, Model: &second, ControlInputID: "control-b", ControlSequence: "20"}
		if _, err := store.PatchDSHWorkerState(ctx, scope, a); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PatchDSHWorkerState(ctx, scope, b); err != nil {
			t.Fatal(err)
		}
		current, err := store.PatchDSHWorkerState(ctx, scope, a)
		if err != nil || current.Model == nil || *current.Model != "model-b" {
			t.Fatalf("old replay=%+v,%v", current, err)
		}
		a.ControlInputID = "unseen-older"
		a.ControlSequence = "15"
		current, err = store.PatchDSHWorkerState(ctx, scope, a)
		if err != nil || current.Model == nil || *current.Model != "model-b" {
			t.Fatalf("out of order=%+v,%v", current, err)
		}
		duplicate, err := store.PatchDSHWorkerState(ctx, scope, b)
		if err != nil || duplicate.ModelControlSequence == nil || *duplicate.ModelControlSequence != "20" {
			t.Fatalf("duplicate=%+v,%v", duplicate, err)
		}
		collision := b
		collision.ControlInputID = "same-version-new-id"
		if _, err := store.PatchDSHWorkerState(ctx, scope, collision); !errors.Is(err, ErrDSHControlConflict) {
			t.Fatalf("same version different identity=%v", err)
		}
		b.Model = &first
		if _, err := store.PatchDSHWorkerState(ctx, scope, b); !errors.Is(err, ErrDSHControlConflict) {
			t.Fatalf("changed control=%v", err)
		}
		a.Model = &second
		a.ControlSequence = "10"
		old, err := store.PatchDSHWorkerState(ctx, scope, a)
		if err != nil || old.Model == nil || *old.Model != second {
			t.Fatalf("historical changed payload must be ignored=%+v,%v", old, err)
		}
		execClaimTest(t, ctx, database, `UPDATE code_sessions SET current_worker_epoch=8 WHERE external_id=$1`, id)
		stale := scope
		scope.Epoch = 8
		if _, err := store.RegisterDSHWorker(ctx, scope); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PatchDSHWorkerState(ctx, stale, a); !errors.Is(err, ErrWorkerEpochMismatch) {
			t.Fatalf("stale owner=%v", err)
		}
		latest := DSHWorkerPatch{ModelSet: true, ControlInputID: "clear-max", ControlSequence: "18446744073709551615"}
		cleared, err := store.PatchDSHWorkerState(ctx, scope, latest)
		if err != nil || cleared.Model != nil || cleared.ModelControlSequence == nil || *cleared.ModelControlSequence != latest.ControlSequence {
			t.Fatalf("clear across owner=%+v,%v", cleared, err)
		}
		current, err = store.PatchDSHWorkerState(ctx, scope, b)
		if err != nil || current.Model != nil {
			t.Fatalf("old after clear=%+v,%v", current, err)
		}
	})
	other := scope
	other.WorkspaceUUID = "53000000-0000-0000-0000-000000000099"
	if _, err := store.PatchDSHWorkerState(ctx, other, DSHWorkerPatch{ModelSet: true, Model: &model, ControlInputID: "other", ControlSequence: "30"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross tenant=%v", err)
	}
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET worker_lease_expires_at=now()-interval '31 seconds' WHERE external_id=$1`, id)
	if _, err := store.HeartbeatDSHWorker(ctx, scope); !errors.Is(err, ErrWorkerLeaseExpired) {
		t.Fatalf("expired heartbeat=%v", err)
	}
	if _, err := store.RegisterDSHWorker(ctx, scope); err != nil {
		t.Fatal(err)
	}
}
