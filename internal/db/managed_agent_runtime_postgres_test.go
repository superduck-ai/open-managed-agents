package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

type managedAgentRuntimeFixture struct {
	organizationID, workspaceID, apiKeyID, environmentID int64
	environmentExternalID, accountEmail                  string
	session                                              Session
	work                                                 EnvironmentWork
}

func TestCreateManagedAgentRuntimeTxAgainstPostgreSQL(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires config: %v", err)
	}
	database, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	t.Run("failure inactive Work", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "stopped")
		_, err := createManagedAgentRuntimeTx(ctx, tx, fixture.runtimeInput(), noManagedAgentInboundEvents)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("failure mismatched Session Environment identity", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "active")
		input := fixture.runtimeInput()
		input.CodeSession.EnvironmentID++
		_, err := createManagedAgentRuntimeTx(ctx, tx, input, noManagedAgentInboundEvents)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("failure Work points to another Session", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "active")
		if _, err := tx.ExecContext(ctx, `update environment_work set data = '{"type":"session","id":"sess_other"}' where id = $1`, fixture.work.ID); err != nil {
			t.Fatalf("mismatch Work Session: %v", err)
		}
		_, err := createManagedAgentRuntimeTx(ctx, tx, fixture.runtimeInput(), noManagedAgentInboundEvents)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("success publishes a committable runtime", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "active")
		seedPublicSessionEvent(ctx, t, tx, fixture)
		var snapshot []SessionEvent
		result, err := createManagedAgentRuntimeTx(ctx, tx, fixture.runtimeInput(), func(events []SessionEvent) ([]AppendCodeSessionEventInput, error) {
			snapshot = events
			return managedAgentInboundEventsForTest(events), nil
		})
		if err != nil {
			t.Fatalf("create runtime: %v", err)
		}
		if len(snapshot) != 1 || snapshot[0].EventType != "user_message" {
			t.Fatalf("locked final Session event = %#v", snapshot)
		}
		var inbound struct {
			Count    int `db:"count"`
			Minimum  int `db:"minimum"`
			Maximum  int `db:"maximum"`
			Distinct int `db:"distinct"`
		}
		if err := tx.GetContext(ctx, &inbound, `select count(*) count, min(sequence_num) minimum, max(sequence_num) maximum, count(distinct sequence_num) distinct from code_session_inbound_events where code_session_id = $1`, result.CodeSession.ID); err != nil {
			t.Fatalf("inspect inbound sequence: %v", err)
		}
		if inbound.Count != 2 || inbound.Minimum != 1 || inbound.Maximum != 2 || inbound.Distinct != 2 || result.CodeSession.LastInboundSequenceNum != 2 {
			t.Fatalf("inbound sequence = %#v, last = %d", inbound, result.CodeSession.LastInboundSequenceNum)
		}
		var firstSubtype, secondSource string
		var secondPayload json.RawMessage
		if err := tx.QueryRowxContext(ctx, `select event_subtype from code_session_inbound_events where code_session_id = $1 and sequence_num = 1`, result.CodeSession.ID).Scan(&firstSubtype); err != nil {
			t.Fatalf("inspect initialize event: %v", err)
		}
		if err := tx.QueryRowxContext(ctx, `select source, payload from code_session_inbound_events where code_session_id = $1 and sequence_num = 2`, result.CodeSession.ID).Scan(&secondSource, &secondPayload); err != nil {
			t.Fatalf("inspect replay event: %v", err)
		}
		if firstSubtype != "initialize" || secondSource != "public_session" || !strings.Contains(string(secondPayload), "user_message") {
			t.Fatalf("inbound order = (%q, %q, %s)", firstSubtype, secondSource, secondPayload)
		}
		var sessionMetadata json.RawMessage
		if err := tx.GetContext(ctx, &sessionMetadata, `select metadata from sessions where id = $1`, fixture.session.ID); err != nil {
			t.Fatalf("load Session metadata: %v", err)
		}
		if !strings.Contains(string(sessionMetadata), result.CodeSession.ExternalID) || !strings.Contains(string(result.EnvironmentWork.Metadata), "claude_code_local") {
			t.Fatalf("runtime metadata not published: Session=%s Work=%s", sessionMetadata, result.EnvironmentWork.Metadata)
		}
		if result.Credentials.AccountEmail != fixture.accountEmail || result.Credentials.PublicSessionExternalID != fixture.session.ExternalID {
			t.Fatalf("credential context = %#v", result.Credentials)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit runtime: %v", err)
		}
	})
}

func (f managedAgentRuntimeFixture) runtimeInput() CreateManagedAgentRuntimeInput {
	codeSessionExternalID := "cse_" + f.session.ExternalID
	return CreateManagedAgentRuntimeInput{
		CodeSession: CreateCodeSessionInput{
			ExternalID: codeSessionExternalID, OrganizationID: f.organizationID, WorkspaceID: f.workspaceID,
			SessionID: f.session.ID, SessionExternalID: f.session.ExternalID, EnvironmentID: f.environmentID,
			EnvironmentExternalID: f.environmentExternalID, WorkDir: "/home/user", PermissionMode: "bypassPermissions",
			Model: "claude-test", Metadata: json.RawMessage(`{"source":"managed_agents_local"}`),
			OAuthAccessTokenHash: "hash_" + f.session.ExternalID, CreatedAt: f.session.CreatedAt,
		},
		SessionMetadataPatch:        json.RawMessage(`{"claude_code_session_id":"` + codeSessionExternalID + `","runtime":"claude_code_local"}`),
		EnvironmentWorkRuntimePatch: json.RawMessage(`{"runtime":"claude_code_local"}`),
		EnvironmentExternalID:       f.environmentExternalID, WorkExternalID: f.work.ExternalID,
	}
}

func noManagedAgentInboundEvents([]SessionEvent) ([]AppendCodeSessionEventInput, error) {
	return nil, nil
}

func managedAgentInboundEventsForTest(events []SessionEvent) []AppendCodeSessionEventInput {
	return []AppendCodeSessionEventInput{
		{ExternalID: "csev_initialize_" + events[0].ExternalID, EventType: "control_request", EventSubtype: "initialize", Payload: json.RawMessage(`{"subtype":"initialize"}`), PayloadHash: "hash_initialize", IdempotencyKey: "idem_initialize", DeliveryStatus: "queued", Source: "internal"},
		{ExternalID: "csev_replay_" + events[0].ExternalID, EventType: "user_message", Payload: events[0].Payload, PayloadHash: "hash_replay", IdempotencyKey: "idem_replay", DeliveryStatus: "queued", Source: "public_session", CreatedAt: events[0].CreatedAt},
	}
}

func beginManagedAgentRuntimeTx(ctx context.Context, t *testing.T, database *DB) *sqlx.Tx {
	t.Helper()
	tx, err := database.sql.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func seedManagedAgentRuntimeFixture(ctx context.Context, t *testing.T, tx *sqlx.Tx, workState string) managedAgentRuntimeFixture {
	t.Helper()
	suffix := strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")
	f := managedAgentRuntimeFixture{environmentExternalID: "env_" + suffix, accountEmail: "owner_" + suffix + "@example.com"}
	var tenant struct {
		OrganizationID int64 `db:"organization_id"`
		WorkspaceID    int64 `db:"workspace_id"`
		APIKeyID       int64 `db:"api_key_id"`
		EnvironmentID  int64 `db:"environment_id"`
	}
	err := tx.GetContext(ctx, &tenant, `
		with organization as (insert into organizations (external_id, name) values ($1, $1) returning id),
		workspace as (insert into workspaces (external_id, organization_id, name) select $2, id, $2 from organization returning id, organization_id),
		app_user as (insert into users (external_id, organization_id, email, name, role) select $3, organization_id, $4, 'Owner', 'user' from workspace returning id),
		api_key as (insert into api_keys (external_id, workspace_id, key_hash, created_by_user_id) select $5, workspace.id, $5, app_user.id from workspace, app_user returning id, workspace_id),
		environment as (insert into environments (external_id, organization_id, workspace_id, created_by_api_key_id, name) select $6, organization.id, workspace.id, api_key.id, $6 from organization, workspace, api_key returning id)
		select organization.id organization_id, workspace.id workspace_id, api_key.id api_key_id, environment.id environment_id from organization, workspace, api_key, environment`,
		"org_"+suffix, "wrkspc_"+suffix, "user_"+suffix, f.accountEmail, "apikey_"+suffix, f.environmentExternalID)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	f.organizationID, f.workspaceID, f.apiKeyID, f.environmentID = tenant.OrganizationID, tenant.WorkspaceID, tenant.APIKeyID, tenant.EnvironmentID
	now := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	base := Session{UUID: managedAgentTestUUID(suffix, 1), ExternalID: "sess_" + suffix, OrganizationID: f.organizationID, WorkspaceID: f.workspaceID, CreatedByAPIKeyID: f.apiKeyID, EnvironmentID: f.environmentID, EnvironmentExternalID: f.environmentExternalID, AgentID: 1, AgentExternalID: "agent_" + suffix, AgentVersion: 3, AgentSnapshot: json.RawMessage(`{"name":"managed-agent"}`), Metadata: json.RawMessage(`{}`), VaultIDs: json.RawMessage(`[]`), Status: "idle", Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), OutcomeEvaluations: json.RawMessage(`[]`), CreatedAt: now}
	input := CreateSessionInput{Session: base, Thread: SessionThread{UUID: managedAgentTestUUID(suffix, 2), ExternalID: "thread_" + suffix, OrganizationID: f.organizationID, WorkspaceID: f.workspaceID, AgentSnapshot: base.AgentSnapshot, Status: "idle", Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), CreatedAt: now}, Work: EnvironmentWork{UUID: managedAgentTestUUID(suffix, 3), ExternalID: "work_" + suffix, OrganizationID: f.organizationID, WorkspaceID: f.workspaceID, EnvironmentID: f.environmentID, EnvironmentExternalID: f.environmentExternalID, Data: json.RawMessage(`{"type":"session","id":"` + base.ExternalID + `"}`), Metadata: json.RawMessage(`{}`), State: "queued", CreatedAt: now}}
	f.session, _, _, f.work, err = insertSessionSQLXTx(ctx, tx, input)
	if err != nil {
		t.Fatalf("seed Session: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `update environment_work set state = $2 where id = $1`, f.work.ID, workState); err != nil {
		t.Fatalf("seed Work state: %v", err)
	}
	return f
}

func seedPublicSessionEvent(ctx context.Context, t *testing.T, tx *sqlx.Tx, f managedAgentRuntimeFixture) {
	t.Helper()
	_, err := tx.ExecContext(ctx, `insert into session_events (external_id, organization_id, workspace_id, session_id, session_external_id, event_type, payload, processed_at, created_at) values ($1, $2, $3, $4, $5, 'user_message', '{"kind":"user_message"}', now(), now())`, "sessev_"+f.session.ExternalID, f.organizationID, f.workspaceID, f.session.ID, f.session.ExternalID)
	if err != nil {
		t.Fatalf("seed Session event: %v", err)
	}
}

func managedAgentTestUUID(suffix string, slot int) string {
	return "00000000-0000-4000-8000-" + string(rune('0'+slot)) + suffix[len(suffix)-11:]
}
