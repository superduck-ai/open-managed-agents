//go:build integration

package db

import (
	"errors"
	"testing"
	"time"
	"uuid"
)

func TestDSHDeliveriesPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	id := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_protocol}','"dsr1"'),worker_lease_expires_at=now()+interval '1 hour' WHERE external_id=$1`, id)
	code, _, err := store.GetCodeSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	scope := DSHScope{CodeSessionID: id, CodeSessionUUID: code.UUID, SessionID: code.SessionExternalID, OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, Epoch: 1}
	mapper := NewDSHDeliveryMapper(store.mapperDB)
	execution := NewDSHExecutionMapper(store.mapperDB)
	if _, err := store.RecordDSHDelivery(ctx, scope, "missing", "ack", ""); !errors.Is(err, ErrDSHInputNotEligible) {
		t.Fatalf("missing input=%v", err)
	}
	if _, err := execution.InsertExecution(ctx, scope, "input", "thread", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	stale := scope
	stale.Epoch = 0
	if _, err := store.RecordDSHDelivery(ctx, stale, "input", "ack", ""); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("stale delivery=%v", err)
	}
	if err := store.EnqueueScheduledObjectCleanupResourceJob(ctx, "job_delivery", scope.WorkspaceUUID, "bucket", "key", "worker_inbound_payload", "input", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if completed, err := store.RecordDSHDelivery(ctx, scope, "input", "ack-original", "job_delivery"); err != nil || completed {
		t.Fatalf("record completed=%v err=%v", completed, err)
	}
	execClaimTest(t, ctx, database, `UPDATE dsh_input_deliveries SET delivered_at=now()-interval '8 days'`)
	other := scope
	other.WorkspaceUUID = uuid.NewV4().String()
	if subject, err := store.DSHDeliveryAckSubject(ctx, other, "input"); err != nil || subject != "" {
		t.Fatalf("tenant leak=%q %v", subject, err)
	}
	if items, err := store.LeaseDSHDeliveries(ctx); err != nil || len(items) != 0 {
		t.Fatalf("ACK before completion=%+v %v", items, err)
	}
	if _, err := execution.StartExecution(ctx, scope, "input"); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.FinishExecution(ctx, scope, "input", "success", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := mapper.Ready(ctx, scope, "input"); err != nil {
		t.Fatal(err)
	}
	first := leaseOneDSHDelivery(t, store)
	if first.CleanupJobUUID == nil {
		t.Fatal("payload cleanup reference missing")
	}
	if items, err := store.LeaseDSHDeliveries(ctx); err != nil || len(items) != 0 {
		t.Fatalf("duplicate lease=%+v %v", items, err)
	}
	if err := store.RetryDSHDelivery(ctx, first); err != nil {
		t.Fatal(err)
	}
	row, _, err := mapper.Find(ctx, scope, "input")
	if err != nil || row.Attempts != 1 || row.Status != "pending" || row.LeaseToken != nil {
		t.Fatalf("retry state=%+v %v", row, err)
	}
	if items, err := store.LeaseDSHDeliveries(ctx); err != nil || len(items) != 0 {
		t.Fatalf("retry skipped backoff=%+v %v", items, err)
	}
	execClaimTest(t, ctx, database, `UPDATE dsh_input_deliveries SET next_attempt_at=now()-interval '1 second'`)
	second := leaseOneDSHDelivery(t, store)
	if first.LeaseToken == second.LeaseToken {
		t.Fatal("lease token reused")
	}
	if err := store.CompleteDSHDelivery(ctx, first); err != nil {
		t.Fatal(err)
	}
	row, _, err = mapper.Find(ctx, scope, "input")
	if err != nil || row.Status != "pending" {
		t.Fatalf("stale completion=%+v %v", row, err)
	}
	execClaimTest(t, ctx, database, `UPDATE dsh_input_deliveries SET lease_until=now()-interval '1 second'`)
	recovered := leaseOneDSHDelivery(t, store)
	if recovered.LeaseToken == second.LeaseToken {
		t.Fatal("crashed lease did not expire")
	}
	if completed, err := store.RecordDSHDelivery(ctx, scope, "input", "ack-redelivered", "job_delivery"); err != nil || !completed {
		t.Fatalf("completed redelivery=%v %v", completed, err)
	}
	if err := store.CompleteDSHDelivery(ctx, recovered); err != nil {
		t.Fatal(err)
	}
	row, _, err = mapper.Find(ctx, scope, "input")
	if err != nil || row.Status != "pending" || row.AckSubject != "ack-redelivered" || row.Attempts != 0 {
		t.Fatalf("new delivery overwritten=%+v %v", row, err)
	}
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET worker_lease_expires_at=now()-interval '1 hour',worker_claim_requested_at=NULL`)
	execClaimTest(t, ctx, database, `UPDATE sessions SET status='idle'`)
	claim := claimTestInput("not-needed")
	claim.WorkerProtocol = "dsr1"
	if _, found, err := store.ClaimNextCodeSession(ctx, claim); err != nil || found {
		t.Fatalf("claimed Brain for pending ACK: found=%v err=%v", found, err)
	}
	current := leaseOneDSHDelivery(t, store)
	wrong := current
	wrong.WorkspaceUUID = other.WorkspaceUUID
	if err := store.CompleteDSHDelivery(ctx, wrong); err != nil {
		t.Fatal(err)
	}
	row, _, err = mapper.Find(ctx, scope, "input")
	if err != nil || row.Status != "pending" {
		t.Fatalf("cross-tenant completion=%+v %v", row, err)
	}
	execClaimTest(t, ctx, database, `CREATE FUNCTION reject_delivery_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected cleanup failure'; END $$`)
	execClaimTest(t, ctx, database, `CREATE TRIGGER reject_delivery_cleanup BEFORE UPDATE ON jobs FOR EACH ROW EXECUTE FUNCTION reject_delivery_cleanup()`)
	if err := store.CompleteDSHDelivery(ctx, current); err == nil {
		t.Fatal("cleanup failure must roll back deletion")
	}
	row, found, err := mapper.Find(ctx, scope, "input")
	if err != nil || !found || row.LeaseToken == nil || *row.LeaseToken != current.LeaseToken {
		t.Fatalf("rollback lost delivery=%+v,%v,%v", row, found, err)
	}
	execClaimTest(t, ctx, database, `DROP TRIGGER reject_delivery_cleanup ON jobs`)
	execClaimTest(t, ctx, database, `DROP FUNCTION reject_delivery_cleanup()`)
	if err := store.CompleteDSHDelivery(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, found, err := mapper.Find(ctx, scope, "input"); err != nil || found {
		t.Fatalf("acknowledged row remains: found=%v err=%v", found, err)
	}
	if err := store.CompleteDSHDelivery(ctx, current); err != nil {
		t.Fatalf("repeated completion=%v", err)
	}
	cleanup, err := store.GetObjectCleanupState(ctx, scope.WorkspaceUUID, "job_delivery")
	if err != nil || cleanup.RunAfter.After(time.Now()) {
		t.Fatalf("cleanup not expedited=%+v %v", cleanup, err)
	}
	if items, err := store.LeaseDSHDeliveries(ctx); err != nil || len(items) != 0 {
		t.Fatalf("ACK repeated=%+v %v", items, err)
	}
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET worker_lease_expires_at=now()+interval '1 hour'`)
	if completed, err := store.RecordDSHDelivery(ctx, scope, "input", "ack-after-delete", ""); err != nil || !completed {
		t.Fatalf("redelivery after deletion=%v,%v", completed, err)
	}
	replay := leaseOneDSHDelivery(t, store)
	if replay.UUID == current.UUID {
		t.Fatal("redelivery reused removed row")
	}
	if err := store.CompleteDSHDelivery(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, found, err := mapper.Find(ctx, scope, "input"); err != nil || !found {
		t.Fatalf("old completion removed new delivery=%v,%v", found, err)
	}
	if err := store.CompleteDSHDelivery(ctx, replay); err != nil {
		t.Fatal(err)
	}
	if _, found, err := mapper.Find(ctx, scope, "input"); err != nil || found {
		t.Fatalf("replayed ACK did not delete=%v,%v", found, err)
	}
	if _, err := store.RecordDSHDelivery(ctx, scope, "input", "retired", ""); err != nil {
		t.Fatal(err)
	}
	execClaimTest(t, ctx, database, `UPDATE sessions SET archived_at=now()`)
	if items, err := store.LeaseDSHDeliveries(ctx); err != nil || len(items) != 0 {
		t.Fatalf("retired delivery leased=%+v %v", items, err)
	}
	row, _, err = mapper.Find(ctx, scope, "input")
	if err != nil || row.Status != "discarded" {
		t.Fatalf("retired delivery=%+v %v", row, err)
	}
	execClaimTest(t, ctx, database, `UPDATE dsh_input_deliveries SET delivered_at=now()-interval '8 days'`)
	if _, err := store.LeaseDSHDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	if _, found, err := mapper.Find(ctx, scope, "input"); err != nil || found {
		t.Fatalf("settled retention failed: found=%v err=%v", found, err)
	}
	if row, found, err := execution.Execution(ctx, scope, "input"); err != nil || !found || row.Status != "completed" {
		t.Fatalf("retention changed business execution: found=%v status=%s err=%v", found, row.Status, err)
	}

}

func leaseOneDSHDelivery(t *testing.T, store *DB) DSHDeliveryLease {
	t.Helper()
	items, err := store.LeaseDSHDeliveries(t.Context())
	if err != nil || len(items) != 1 {
		t.Fatalf("delivery lease count=%d err=%v", len(items), err)
	}
	return items[0]
}
