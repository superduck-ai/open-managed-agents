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

// managedAgentRuntimeFixture 是启动事务所需的最小租户与 Session 上下文。
type managedAgentRuntimeFixture struct {
	organizationID        int64
	workspaceID           int64
	apiKeyID              int64
	environmentID         int64
	environmentExternalID string
	accountEmail          string
	session               Session
	work                  EnvironmentWork
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

	t.Run("failure inactive Work rejects the launch", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "stopped")
		_, err := createManagedAgentRuntimeTx(ctx, tx, fixture.runtimeInput(), noManagedAgentInboundEvents)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("createManagedAgentRuntimeTx() error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("failure terminated Session rejects the launch", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "active")
		if _, err := tx.ExecContext(ctx, `
			update sessions set status = 'terminated' where id = $1
		`, fixture.session.ID); err != nil {
			t.Fatalf("terminate seeded session: %v", err)
		}
		_, err := createManagedAgentRuntimeTx(ctx, tx, fixture.runtimeInput(), noManagedAgentInboundEvents)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("createManagedAgentRuntimeTx() error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("failure mismatched Session identity rejects the launch", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "active")
		input := fixture.runtimeInput()
		input.CodeSession.SessionID++

		_, err := createManagedAgentRuntimeTx(ctx, tx, input, noManagedAgentInboundEvents)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("createManagedAgentRuntimeTx() error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("failure mismatched Environment identity rejects the launch", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "active")
		input := fixture.runtimeInput()
		input.CodeSession.EnvironmentID++

		_, err := createManagedAgentRuntimeTx(ctx, tx, input, noManagedAgentInboundEvents)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("createManagedAgentRuntimeTx() error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("failure mismatched Work identity rejects the launch", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "active")
		if _, err := tx.ExecContext(ctx, `
			update environment_work set organization_id = organization_id + 1 where id = $1
		`, fixture.work.ID); err != nil {
			t.Fatalf("mismatch seeded work identity: %v", err)
		}

		_, err := createManagedAgentRuntimeTx(ctx, tx, fixture.runtimeInput(), noManagedAgentInboundEvents)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("createManagedAgentRuntimeTx() error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("failure Work for another Session rejects the launch", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "active")
		if _, err := tx.ExecContext(ctx, `
			update environment_work set data = '{"type":"session","id":"sess_other"}' where id = $1
		`, fixture.work.ID); err != nil {
			t.Fatalf("mismatch seeded work Session: %v", err)
		}

		_, err := createManagedAgentRuntimeTx(ctx, tx, fixture.runtimeInput(), noManagedAgentInboundEvents)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("createManagedAgentRuntimeTx() error = %v, want ErrInvalidState", err)
		}
	})

	t.Run("success binds named parameters and scans every row", func(t *testing.T) {
		tx := beginManagedAgentRuntimeTx(ctx, t, database)
		fixture := seedManagedAgentRuntimeFixture(ctx, t, tx, "active")
		seedPublicSessionEvent(ctx, t, tx, fixture)

		var snapshot []SessionEvent
		result, err := createManagedAgentRuntimeTx(
			ctx,
			tx,
			fixture.runtimeInput(),
			func(events []SessionEvent) ([]AppendCodeSessionEventInput, error) {
				snapshot = events
				return managedAgentInboundEventsForTest(events), nil
			},
		)
		if err != nil {
			t.Fatalf("createManagedAgentRuntimeTx() error = %v", err)
		}

		if len(snapshot) != 1 || snapshot[0].EventType != "user_message" {
			t.Fatalf("locked event snapshot = %#v, want one user_message", snapshot)
		}
		if string(snapshot[0].Payload) != `{"kind": "user_message"}` {
			t.Fatalf("locked event payload = %s, want the seeded JSONB document", snapshot[0].Payload)
		}
		assertManagedAgentCodeSession(t, fixture, result.CodeSession)
		assertManagedAgentWorkMetadata(t, result.EnvironmentWork)
		assertManagedAgentCredentials(t, fixture, result.Credentials)

		session, err := getSessionSQLX(
			ctx,
			tx,
			getSessionQuery,
			sessionLookupArguments(fixture.workspaceID, fixture.session.ExternalID),
		)
		if err != nil {
			t.Fatalf("reload patched session: %v", err)
		}
		if jsonFieldForTest(t, session.Metadata, "claude_code_session_id") != `"`+result.CodeSession.ExternalID+`"` {
			t.Fatalf("session metadata = %s, want the published Code Session ID", session.Metadata)
		}

		var queued int
		if err := tx.GetContext(ctx, &queued, `
			select count(*) from code_session_inbound_events where code_session_id = $1
		`, result.CodeSession.ID); err != nil {
			t.Fatalf("count inbound events: %v", err)
		}
		if queued != 2 || result.CodeSession.LastInboundSequenceNum != 2 {
			t.Fatalf("queued = %d, last inbound sequence = %d, want 2 and 2", queued, result.CodeSession.LastInboundSequenceNum)
		}
	})
}

func assertManagedAgentCodeSession(t *testing.T, fixture managedAgentRuntimeFixture, codeSession CodeSession) {
	t.Helper()
	if codeSession.ID == 0 || codeSession.UUID == "" {
		t.Fatalf("code session identity = (%d, %q), want generated id and uuid", codeSession.ID, codeSession.UUID)
	}
	if codeSession.SessionExternalID != fixture.session.ExternalID || codeSession.EnvironmentID != fixture.environmentID {
		t.Fatalf("code session scope = %#v, want the seeded session and environment", codeSession)
	}
	if jsonFieldForTest(t, codeSession.Metadata, "source") != `"managed_agents_local"` {
		t.Fatalf("code session metadata = %s, want the JSONB document written by CAST", codeSession.Metadata)
	}
	// worker_external_metadata 在数据库里是 null，行映射必须补成空对象。
	if string(codeSession.WorkerExternalMetadata) != `{}` {
		t.Fatalf("worker external metadata = %s, want {}", codeSession.WorkerExternalMetadata)
	}
	if codeSession.WorkerTokenSessionID != nil || codeSession.WorkerLeaseExpiresAt != nil {
		t.Fatalf("nullable worker columns = (%v, %v), want nil", codeSession.WorkerTokenSessionID, codeSession.WorkerLeaseExpiresAt)
	}
}

func assertManagedAgentWorkMetadata(t *testing.T, work EnvironmentWork) {
	t.Helper()
	if jsonFieldForTest(t, work.Metadata, "managed_agent_skills_mount") != "" {
		t.Fatalf("work metadata = %s, want no legacy skill mount", work.Metadata)
	}
	if jsonFieldForTest(t, work.Metadata, "runtime") != `"claude_code_local"` {
		t.Fatalf("work runtime patch = %s, want the merged runtime document", work.Metadata)
	}
}

// jsonFieldForTest 按字段比较 JSONB 往返结果，避免断言依赖 PostgreSQL 的空白规范化。
func jsonFieldForTest(t *testing.T, document json.RawMessage, field string) string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(document, &fields); err != nil {
		t.Fatalf("decode %s: %v", document, err)
	}
	return string(fields[field])
}

func assertManagedAgentCredentials(t *testing.T, fixture managedAgentRuntimeFixture, credentials CodeSessionCredentialContext) {
	t.Helper()
	if credentials.AccountEmail != fixture.accountEmail {
		t.Fatalf("account email = %q, want %q", credentials.AccountEmail, fixture.accountEmail)
	}
	if credentials.PublicSessionExternalID != fixture.session.ExternalID {
		t.Fatalf("public session = %q, want %q", credentials.PublicSessionExternalID, fixture.session.ExternalID)
	}
	if credentials.AgentVersion != fixture.session.AgentVersion {
		t.Fatalf("agent version = %d, want %d", credentials.AgentVersion, fixture.session.AgentVersion)
	}
	// organization/workspace UUID 走 CAST(uuid AS text)，空值说明列别名与行映射不匹配。
	if credentials.OrganizationUUID == "" || credentials.WorkspaceUUID == "" {
		t.Fatalf("tenant uuids = (%q, %q), want scanned text casts", credentials.OrganizationUUID, credentials.WorkspaceUUID)
	}
}

func (f managedAgentRuntimeFixture) runtimeInput() CreateManagedAgentRuntimeInput {
	return CreateManagedAgentRuntimeInput{
		CodeSession: CreateCodeSessionInput{
			ExternalID:            "cse_" + f.session.ExternalID,
			OrganizationID:        f.organizationID,
			WorkspaceID:           f.workspaceID,
			SessionID:             f.session.ID,
			SessionExternalID:     f.session.ExternalID,
			EnvironmentID:         f.environmentID,
			EnvironmentExternalID: f.environmentExternalID,
			WorkDir:               "/home/user",
			PermissionMode:        "bypassPermissions",
			Model:                 "claude-test",
			Metadata:              json.RawMessage(`{"source":"managed_agents_local"}`),
			OAuthAccessTokenHash:  "hash_" + f.session.ExternalID,
			CreatedAt:             f.session.CreatedAt,
		},
		SessionMetadataPatch:        json.RawMessage(`{"claude_code_session_id":"cse_` + f.session.ExternalID + `","runtime":"claude_code_local"}`),
		EnvironmentWorkRuntimePatch: json.RawMessage(`{"runtime":"claude_code_local"}`),
		EnvironmentExternalID:       f.environmentExternalID,
		WorkExternalID:              f.work.ExternalID,
	}
}

func noManagedAgentInboundEvents([]SessionEvent) ([]AppendCodeSessionEventInput, error) {
	return nil, nil
}

func managedAgentInboundEventsForTest(events []SessionEvent) []AppendCodeSessionEventInput {
	inputs := []AppendCodeSessionEventInput{{
		ExternalID:     "csev_initialize",
		EventType:      "control_request",
		EventSubtype:   "initialize",
		Payload:        json.RawMessage(`{"subtype":"initialize"}`),
		PayloadHash:    "hash_initialize",
		IdempotencyKey: "idem_initialize",
		DeliveryStatus: "queued",
		Source:         "internal",
	}}
	for index, event := range events {
		inputs = append(inputs, AppendCodeSessionEventInput{
			ExternalID:     "csev_replay_" + event.ExternalID,
			EventType:      "user_message",
			Payload:        event.Payload,
			PayloadHash:    "hash_replay",
			IdempotencyKey: "idem_replay",
			DeliveryStatus: "queued",
			Source:         "public_session",
			CreatedAt:      event.CreatedAt.Add(time.Duration(index) * time.Millisecond),
		})
	}
	return inputs
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

// seedManagedAgentRuntimeFixture 在事务内建立完整租户链路，并复用生产创建路径
// insertSessionSQLXTx 写入 Session、thread、filesystem 与 Work，避免 fixture 与
// 真实 Session 形状漂移。整个事务在测试结束时回滚。
func seedManagedAgentRuntimeFixture(ctx context.Context, t *testing.T, tx *sqlx.Tx, workState string) managedAgentRuntimeFixture {
	t.Helper()
	suffix := managedAgentTestSuffix()
	fixture := managedAgentRuntimeFixture{
		environmentExternalID: "env_" + suffix,
		accountEmail:          "owner_" + suffix + "@example.com",
	}
	if err := tx.GetContext(ctx, &fixture.organizationID, `
		insert into organizations (external_id, name) values ($1, $1) returning id
	`, "org_"+suffix); err != nil {
		t.Fatalf("seed organization: %v", err)
	}
	if err := tx.GetContext(ctx, &fixture.workspaceID, `
		insert into workspaces (external_id, organization_id, name) values ($1, $2, $1) returning id
	`, "wrkspc_"+suffix, fixture.organizationID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	var userID int64
	if err := tx.GetContext(ctx, &userID, `
		insert into users (external_id, organization_id, email, name, role)
		values ($1, $2, $3, 'Owner', 'user')
		returning id
	`, "user_"+suffix, fixture.organizationID, fixture.accountEmail); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tx.GetContext(ctx, &fixture.apiKeyID, `
		insert into api_keys (external_id, workspace_id, key_hash, created_by_user_id)
		values ($1, $2, $1, $3)
		returning id
	`, "apikey_"+suffix, fixture.workspaceID, userID); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	if err := tx.GetContext(ctx, &fixture.environmentID, `
		insert into environments (external_id, organization_id, workspace_id, created_by_api_key_id, name)
		values ($1, $2, $3, $4, $1)
		returning id
	`, fixture.environmentExternalID, fixture.organizationID, fixture.workspaceID, fixture.apiKeyID); err != nil {
		t.Fatalf("seed environment: %v", err)
	}

	session, _, _, work, err := insertSessionSQLXTx(ctx, tx, fixture.createSessionInput(suffix))
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		update environment_work set state = $2 where id = $1
	`, work.ID, workState); err != nil {
		t.Fatalf("seed work state: %v", err)
	}
	fixture.session = session
	fixture.work = work
	return fixture
}

func (f managedAgentRuntimeFixture) createSessionInput(suffix string) CreateSessionInput {
	now := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	agentSnapshot := json.RawMessage(`{"name":"managed-agent"}`)
	return CreateSessionInput{
		Session: Session{
			UUID:                  managedAgentTestUUID(suffix, 1),
			ExternalID:            "sess_" + suffix,
			OrganizationID:        f.organizationID,
			WorkspaceID:           f.workspaceID,
			CreatedByAPIKeyID:     f.apiKeyID,
			EnvironmentID:         f.environmentID,
			EnvironmentExternalID: f.environmentExternalID,
			AgentID:               1,
			AgentExternalID:       "agent_" + suffix,
			AgentVersion:          3,
			AgentSnapshot:         agentSnapshot,
			Metadata:              json.RawMessage(`{}`),
			VaultIDs:              json.RawMessage(`[]`),
			Status:                "idle",
			Usage:                 json.RawMessage(`{}`),
			Stats:                 json.RawMessage(`{}`),
			OutcomeEvaluations:    json.RawMessage(`[]`),
			CreatedAt:             now,
		},
		Thread: SessionThread{
			UUID:           managedAgentTestUUID(suffix, 2),
			ExternalID:     "thread_" + suffix,
			OrganizationID: f.organizationID,
			WorkspaceID:    f.workspaceID,
			AgentSnapshot:  agentSnapshot,
			Status:         "idle",
			Usage:          json.RawMessage(`{}`),
			Stats:          json.RawMessage(`{}`),
			CreatedAt:      now,
		},
		Work: EnvironmentWork{
			UUID:                  managedAgentTestUUID(suffix, 3),
			ExternalID:            "work_" + suffix,
			OrganizationID:        f.organizationID,
			WorkspaceID:           f.workspaceID,
			EnvironmentID:         f.environmentID,
			EnvironmentExternalID: f.environmentExternalID,
			Data:                  json.RawMessage(`{"type":"session","id":"sess_` + suffix + `"}`),
			Metadata:              json.RawMessage(`{}`),
			State:                 "queued",
			CreatedAt:             now,
		},
	}
}

func seedPublicSessionEvent(ctx context.Context, t *testing.T, tx *sqlx.Tx, fixture managedAgentRuntimeFixture) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		insert into session_events (
			external_id, organization_id, workspace_id, session_id, session_external_id,
			event_type, payload, processed_at, created_at
		)
		select $1, $2, $3, $4, $5, 'user_message', CAST($6 AS jsonb), now(), now()
	`,
		"sessev_"+fixture.session.ExternalID,
		fixture.organizationID,
		fixture.workspaceID,
		fixture.session.ID,
		fixture.session.ExternalID,
		`{"kind": "user_message"}`,
	); err != nil {
		t.Fatalf("seed session event: %v", err)
	}
}

func managedAgentTestSuffix() string {
	return strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "")
}

func managedAgentTestUUID(suffix string, slot int) string {
	digits := suffix
	if len(digits) > 11 {
		digits = digits[len(digits)-11:]
	}
	return "00000000-0000-4000-8000-" + string(rune('0'+slot)) + digits
}
