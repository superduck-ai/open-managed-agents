//go:build integration

package db

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type dshLockFixture struct {
	ctx      context.Context
	database *sql.DB
	store    *DB
	scope    DSHScope
	code     CodeSession
}

func newDSHLockFixture(t *testing.T) dshLockFixture {
	t.Helper()
	ctx, database, store := newCodeSessionClaimTestStore(t)
	id := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET current_worker_epoch=7, worker_claimed_by='old-worker', worker_lease_expires_at=now()+interval '1 hour' WHERE external_id=$1`, id)
	code, found, err := store.GetCodeSession(ctx, id)
	if err != nil || !found {
		t.Fatalf("code=%v,%v", found, err)
	}
	scope := DSHScope{CodeSessionID: id, OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, CodeSessionUUID: code.UUID, SessionID: code.SessionExternalID, Epoch: 7}
	if _, err := store.PutDSHTranscript(ctx, scope, "root", nil, []byte(`{"version":4,"id":"root"}`)); err != nil {
		t.Fatal(err)
	}
	return dshLockFixture{ctx, database, store, scope, code}
}

func holdDSHTransaction(t *testing.T, f dshLockFixture, operation func(yourbatis.Executor) error) func() {
	t.Helper()
	ready := make(chan error, 1)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		err := f.store.mapperDB.Transaction(f.ctx, func(executor yourbatis.Executor) error {
			if err := operation(executor); err != nil {
				return err
			}
			ready <- nil
			<-release
			return nil
		})
		if err != nil {
			select {
			case ready <- err:
			default:
			}
		}
		done <- err
	}()
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	finish := func() {
		once.Do(func() {
			close(release)
			if err := <-done; err != nil {
				t.Errorf("held transaction: %v", err)
			}
		})
	}
	t.Cleanup(finish)
	return finish
}

func holdDSHAppend(t *testing.T, f dshLockFixture) func() {
	t.Helper()
	return holdDSHTransaction(t, f, func(executor yourbatis.Executor) error {
		if err := lockDSHWorker(f.ctx, executor, f.scope); err != nil {
			return err
		}
		mapper := NewDSHTranscriptMapper(executor)
		row, found, err := mapper.LockTranscript(f.ctx, f.scope, "root")
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		return appendDSHEvents(f.ctx, mapper, f.scope, row, []DSHTranscriptEvent{{Seq: 0, Payload: []byte(`{"seq":0}`)}}, nil, &DSHTranscriptAppendResult{})
	})
}

func TestDSHLockingPostgresOwnershipChangesWait(t *testing.T) {
	f := newDSHLockFixture(t)
	release := holdDSHAppend(t, f)
	mapper := NewCodeSessionMapper(f.store.mapperDB)
	claims := NewCodeSessionClaimMapper(f.store.mapperDB)
	mutations := map[string]func(context.Context) error{
		"register": func(ctx context.Context) error {
			_, err := mapper.RegisterWorker(ctx, registerCodeSessionWorkerParams{UUID: f.code.UUID, Epoch: 8, WorkerBinding: []byte(`{}`), Now: time.Now(), ExpiresAt: time.Now().Add(time.Minute)})
			return err
		},
		"rotate": func(ctx context.Context) error {
			_, err := mapper.RotateCredentials(ctx, rotateCodeSessionCredentialsParams{OrganizationUUID: f.code.OrganizationUUID, WorkspaceUUID: f.code.WorkspaceUUID, SessionExternalID: f.code.SessionExternalID, CodeSessionExternalID: f.code.ExternalID})
			return err
		},
		"claim": func(ctx context.Context) error {
			_, err := claims.Claim(ctx, claimCodeSessionParams{UUID: f.code.UUID, WorkerID: "new-worker", ClaimExpiresAt: time.Now().Add(time.Minute)})
			return err
		},
		"exhaust": func(ctx context.Context) error {
			_, err := claims.HoldExhausted(ctx, f.code.UUID, time.Now().Add(time.Minute))
			return err
		},
		"permanent failure": func(ctx context.Context) error {
			_, err := claims.FailPermanently(ctx, failCodeSessionClaimParams{ExternalID: f.code.ExternalID, WorkerID: "old-worker", Epoch: 7, HoldUntil: time.Now().Add(time.Minute)})
			return err
		},
		"release": func(ctx context.Context) error {
			_, err := claims.Release(ctx, releaseCodeSessionClaimParams{ExternalID: f.code.ExternalID, WorkerID: "old-worker", Epoch: 7, Kind: CodeSessionClaimReleaseIdle})
			return err
		},
		"terminate session": func(ctx context.Context) error {
			_, err := mapper.TerminateBySession(ctx, f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.SessionUUID)
			return err
		},
		"terminate code session": func(ctx context.Context) error {
			_, err := mapper.TerminateByExternalID(ctx, f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.ExternalID)
			return err
		},
		"terminate launch": func(ctx context.Context) error {
			_, err := mapper.TerminateLaunch(ctx, f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.ExternalID, 7)
			return err
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
			defer cancel()
			if err := mutate(ctx); err == nil || ctx.Err() != context.DeadlineExceeded {
				t.Fatalf("ownership mutation crossed held append: %v, context=%v", err, ctx.Err())
			}
		})
	}
	release()
	epoch, err := mapper.RotateCredentials(f.ctx, rotateCodeSessionCredentialsParams{OrganizationUUID: f.code.OrganizationUUID, WorkspaceUUID: f.code.WorkspaceUUID, SessionExternalID: f.code.SessionExternalID, CodeSessionExternalID: f.code.ExternalID})
	if err != nil || epoch != 8 {
		t.Fatalf("rotate=%d,%v", epoch, err)
	}
	if _, err := f.store.AppendDSHTranscript(f.ctx, f.scope, "root", []DSHTranscriptEvent{{Seq: 1, Payload: []byte(`{"seq":1}`)}}); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("old worker write=%v", err)
	}
	f.scope.Epoch = 8
	if _, err := f.store.RegisterDSHWorker(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AppendDSHTranscript(f.ctx, f.scope, "root", []DSHTranscriptEvent{{Seq: 1, Payload: []byte(`{"seq":1}`)}}); err != nil {
		t.Fatal(err)
	}
}

func TestDSHLockingPostgresAppendIsolationAndHeartbeat(t *testing.T) {
	f := newDSHLockFixture(t)
	parent := "root"
	if _, err := f.store.PutDSHTranscript(f.ctx, f.scope, "other", &parent, []byte(`{"version":4,"id":"other"}`)); err != nil {
		t.Fatal(err)
	}
	release := holdDSHAppend(t, f)
	ctx, cancel := context.WithTimeout(f.ctx, time.Second)
	defer cancel()
	expires, err := f.store.HeartbeatDSHWorker(ctx, f.scope)
	if err != nil || time.Until(expires) < 19*time.Second {
		t.Fatalf("heartbeat blocked by append: %v", err)
	}
	if _, err := f.store.AppendDSHTranscript(ctx, f.scope, "other", []DSHTranscriptEvent{{Seq: 0, Payload: []byte(`{"seq":0}`)}}); err != nil {
		t.Fatalf("different transcript blocked: %v", err)
	}
	if _, err := f.store.PutDSHTranscript(ctx, f.scope, "new-child", &parent, []byte(`{"version":4,"id":"new-child"}`)); err != nil {
		t.Fatalf("header blocked: %v", err)
	}
	sameCtx, sameCancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	defer sameCancel()
	if _, err := f.store.AppendDSHTranscript(sameCtx, f.scope, "root", []DSHTranscriptEvent{{Seq: 1, Payload: []byte(`{"seq":1}`)}}); err == nil || sameCtx.Err() != context.DeadlineExceeded {
		t.Fatalf("same transcript was not serialized: %v", err)
	}
	release()
	if _, err := f.store.AppendDSHTranscript(f.ctx, f.scope, "root", []DSHTranscriptEvent{{Seq: 1, Payload: []byte(`{"seq":1}`)}}); err != nil {
		t.Fatal(err)
	}
}

func TestDSHLockingPostgresHeartbeatClock(t *testing.T) {
	for _, expired := range []bool{true, false} {
		t.Run(map[bool]string{true: "reject expired grace after lock wait", false: "TTL begins after lock acquisition"}[expired], func(t *testing.T) {
			f := newDSHLockFixture(t)
			if expired {
				execClaimTest(t, f.ctx, f.database, `UPDATE code_sessions SET worker_lease_expires_at=clock_timestamp()-interval '9.7 seconds' WHERE uuid=$1`, f.code.UUID)
			}
			release := holdDSHTransaction(t, f, func(executor yourbatis.Executor) error {
				_, _, err := NewDSHTranscriptMapper(executor).LockWorker(f.ctx, f.scope)
				return err
			})
			type heartbeatResult struct {
				expires time.Time
				err     error
			}
			result := make(chan heartbeatResult, 1)
			ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
			defer cancel()
			go func() {
				expires, err := f.store.HeartbeatDSHWorker(ctx, f.scope)
				result <- heartbeatResult{expires, err}
			}()
			select {
			case early := <-result:
				t.Fatalf("heartbeat did not wait for ownership fence: %+v", early)
			case <-time.After(750 * time.Millisecond):
			}
			releasedAt := time.Now()
			release()
			received := <-result
			if expired {
				if !errors.Is(received.err, ErrWorkerLeaseExpired) {
					t.Fatalf("expired lease revived after wait: %v", received.err)
				}
			} else if received.err != nil || received.expires.Before(releasedAt.Add(19750*time.Millisecond)) {
				t.Fatalf("lease TTL consumed by waiting: %v, expires=%v, release=%v", received.err, received.expires, releasedAt)
			}
		})
	}
}

func TestDSHLockingPostgresControlWrites(t *testing.T) {
	f := newDSHLockFixture(t)
	execClaimTest(t, f.ctx, f.database, `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_protocol}','"dsr1"') WHERE uuid=$1`, f.code.UUID)
	now := time.Now().UTC()
	if _, err := f.store.CreateSessionThreadIfAbsent(f.ctx, SessionThread{UUID: "53000000-0000-0000-0011-000000000001", ExternalID: "sthr_lock", OrganizationUUID: f.code.OrganizationUUID, WorkspaceUUID: f.code.WorkspaceUUID, SessionUUID: f.code.SessionUUID, SessionExternalID: f.code.SessionExternalID, AgentSnapshot: []byte(`{}`), Status: "running", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	input := SessionEvent{UUID: "53000000-0000-0000-0012-000000000001", ExternalID: "lock-input", EventType: "user.message", Payload: []byte(`{"id":"lock-input","type":"user.message","content":[]}`), CreatedAt: now}
	if _, err := f.store.AppendSessionEvents(f.ctx, f.scope.WorkspaceUUID, f.scope.SessionID, []SessionEvent{input}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.DeliverDSHInputs(f.ctx, f.scope, []DSHDeliveryUpdate{{ID: input.ExternalID, Status: "processing"}}); err != nil {
		t.Fatal(err)
	}
	release := holdDSHAppend(t, f)
	ctx, cancel := context.WithTimeout(f.ctx, time.Second)
	defer cancel()
	output := SessionEvent{UUID: "53000000-0000-0000-0012-000000000002", ExternalID: "lock-output", EventType: "agent.message", Payload: []byte(`{"type":"agent.message","content":[]}`), CreatedAt: now, ProcessedAt: now}
	result, err := f.store.PublishDSHEvents(ctx, f.scope, []DSHPublicEvent{{ID: output.ExternalID, Payload: output.Payload, Events: []SessionEvent{output}}})
	if err != nil || result.Accepted != 1 {
		t.Fatalf("public output blocked by append: %+v,%v", result, err)
	}
	if _, _, err := f.store.FinishDSHInput(ctx, f.scope, input.ExternalID, DSHFinish{Result: "success"}); err != nil {
		t.Fatalf("finish blocked by append: %v", err)
	}
	release()
	rows, err := NewSandboxLifecycleMapper(f.store.mapperDB).Claim(f.ctx, f.code.UUID, "53000000-0000-0000-0013-000000000001", time.Now())
	if err != nil || rows != 0 {
		t.Fatalf("sandbox claim binding: %d,%v", rows, err)
	}
}
