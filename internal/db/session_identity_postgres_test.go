//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superduck-ai/yourbatis"
)

func TestCreateSessionIdentityIsAtomicPostgres(t *testing.T) {
	t.Run("控制台", func(t *testing.T) {
		testCreateSessionIdentityIsAtomic(t, InsertSessionIdentityParams{ScopeKind: "console", ScopeKey: "console"})
	})
	t.Run("渠道", func(t *testing.T) {
		testCreateSessionIdentityIsAtomic(t, InsertSessionIdentityParams{
			ScopeKind: "direct", ScopeKey: "p2p:actor", ChannelExternalID: "chnl_atomic", BindingRevision: 2,
		})
	})
}

func testCreateSessionIdentityIsAtomic(t *testing.T, bindingParams InsertSessionIdentityParams) {
	t.Helper()
	ctx, database, store := newCodeSessionClaimTestStore(t)
	fixture := devIdentityFixture()
	if err := store.CreateDevIdentity(ctx, fixture); err != nil {
		t.Fatal(err)
	}
	org, workspace := fixture.OrganizationUUID, fixture.Workspaces[0].UUID
	identity, err := store.CreateBusinessIdentity(ctx, InsertIdentityParams{
		UUID: uuid.NewV4().String(), ExternalID: "idn_console_atomic", OrganizationUUID: org, WorkspaceUUID: workspace,
		ExternalSubjectID: "console", DisplayName: "控制台测试身份", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	empty := json.RawMessage("{}")
	bindingParams.IdentityUUID = identity.UUID
	input := CreateSessionInput{
		IdentityBinding: &bindingParams,
		Session: Session{
			UUID: uuid.NewV4().String(), ExternalID: "sesn_console_atomic", OrganizationUUID: org, WorkspaceUUID: workspace,
			EnvironmentUUID: uuid.NewV4().String(), EnvironmentExternalID: "env_console_atomic", AgentUUID: uuid.NewV4().String(),
			AgentExternalID: "agent_console_atomic", AgentVersion: 1, AgentSnapshot: empty,
			Status: "idle", Metadata: empty, Budget: empty, Usage: empty, Stats: empty, OutcomeEvaluations: json.RawMessage("[]"), CreatedAt: now,
		},
		Thread: SessionThread{
			UUID: uuid.NewV4().String(), ExternalID: "sthr_console_atomic", OrganizationUUID: org, WorkspaceUUID: workspace,
			AgentSnapshot: empty, Status: "idle", Usage: empty, Stats: empty, CreatedAt: now,
		},
		Work: EnvironmentWork{
			UUID: uuid.NewV4().String(), ExternalID: "work_console_atomic", OrganizationUUID: org, WorkspaceUUID: workspace,
			EnvironmentExternalID: "env_console_atomic", Metadata: empty, State: "queued", CreatedAt: now,
		},
	}
	input.Work.EnvironmentUUID = input.Session.EnvironmentUUID

	if err := store.SetBusinessIdentityEnabled(ctx, workspace, identity.ExternalID, false); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := store.CreateSessionWithEvents(ctx, input, nil, nil); !errors.Is(err, ErrConsoleIdentityDisabled) {
		t.Fatalf("停用身份拒绝创建会话：%v", err)
	}
	var sessions, work int
	err = database.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM sessions WHERE uuid = $1),
		(SELECT count(*) FROM environment_work WHERE session_uuid = $1)`, input.Session.UUID).Scan(&sessions, &work)
	if err != nil || sessions != 0 || work != 0 {
		t.Fatalf("事务失败后仍有会话或任务：会话=%d，任务=%d，错误=%v", sessions, work, err)
	}

	if err := store.SetBusinessIdentityEnabled(ctx, workspace, identity.ExternalID, true); err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("结束锁定测试，回滚临时会话")
	err = store.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if _, _, _, _, err := insertSessionTx(ctx, executor, input); err != nil {
			return err
		}
		var locked string
		err := database.QueryRowContext(ctx, `SELECT uuid::text FROM identities WHERE workspace_uuid=$1 AND uuid=$2 FOR NO KEY UPDATE NOWAIT`, workspace, identity.UUID).Scan(&locked)
		if pgError, ok := errors.AsType[*pgconn.PgError](err); !ok || pgError.Code != "55P03" {
			t.Errorf("会话创建后停用身份应等待事务提交：%v", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := store.CreateSessionWithEvents(ctx, input, nil, nil); err != nil {
		t.Fatal(err)
	}
	binding, err := store.FindSessionIdentity(ctx, workspace, input.Session.ExternalID)
	if err != nil || binding.IdentityUUID != identity.UUID || binding.ScopeKind != bindingParams.ScopeKind ||
		binding.ScopeKey != bindingParams.ScopeKey || binding.ChannelExternalID != bindingParams.ChannelExternalID ||
		binding.BindingRevision != bindingParams.BindingRevision {
		t.Fatalf("会话和任务提交后应已带身份绑定：%+v，错误=%v", binding, err)
	}
}
