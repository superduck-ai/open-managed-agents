//go:build integration

package db

import (
	"errors"
	"testing"
	"time"
	"uuid"
)

func TestDSHSandboxRenewalUsesPublicActivityPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	id := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_protocol}','"dsr1"'),worker_status='idle' WHERE external_id=$1`, id)
	code, _, err := store.GetCodeSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetRenewableEnvironmentSandboxForCodeSession(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("early session without sandbox", err)
	}
	work, err := store.CreateEnvironmentWork(ctx, EnvironmentWork{UUID: uuid.NewV4().String(), ExternalID: "work_dsr_renew", OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, EnvironmentUUID: code.EnvironmentUUID, EnvironmentExternalID: code.EnvironmentExternalID, SessionUUID: code.SessionUUID, SessionExternalID: code.SessionExternalID, State: "active", Metadata: []byte(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	providerID := "provider-owned-test"
	sandbox, err := store.CreateEnvironmentSandbox(ctx, EnvironmentSandbox{UUID: uuid.NewV4().String(), ExternalID: "sbx_dsr_renew", OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, EnvironmentUUID: code.EnvironmentUUID, EnvironmentExternalID: code.EnvironmentExternalID, WorkUUID: &work.UUID, WorkExternalID: &work.ExternalID, Provider: "e2b", Template: "test", ProviderSandboxID: &providerID, State: "running", Metadata: []byte(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"idle", "terminated", "running"} {
		execClaimTest(t, ctx, database, `UPDATE sessions SET status=$1 WHERE uuid=$2`, status, code.SessionUUID)
		found, err := store.GetRenewableEnvironmentSandboxForCodeSession(ctx, id)
		if status != "running" {
			if !errors.Is(err, ErrNotFound) {
				t.Fatal("inactive DSR session renewed", status, err)
			}
		} else if err != nil || found.UUID != sandbox.UUID {
			t.Fatal("public running DSR with legacy idle not renewed", found, err)
		}
	}
	execClaimTest(t, ctx, database, `UPDATE sessions SET status='idle' WHERE uuid=$1`, code.SessionUUID)
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET metadata='{}',worker_status='running' WHERE external_id=$1`, id)
	if found, err := store.GetRenewableEnvironmentSandboxForCodeSession(ctx, id); err != nil || found.UUID != sandbox.UUID {
		t.Fatal("legacy worker activity changed", err)
	}
}
