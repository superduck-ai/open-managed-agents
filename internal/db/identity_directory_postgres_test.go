//go:build integration

package db

import (
	json "encoding/json/v2"
	"fmt"
	"testing"
	"uuid"
)

func TestIdentityDirectoryPostgres(t *testing.T) {
	ctx := t.Context()
	store, database := newTranscriptArchiveTestDB(t)
	scope := transcriptTestScope()
	for _, item := range []struct {
		id      string
		enabled bool
	}{
		{"idn_active", true}, {"idn_disabled", false},
	} {
		identity, err := store.CreateBusinessIdentity(ctx, InsertIdentityParams{
			UUID: uuid.NewV4().String(), ExternalID: item.id, OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID,
			Namespace: "test", ExternalSubjectID: item.id, DisplayName: item.id, Enabled: item.enabled,
		})
		if err != nil {
			t.Fatal(err)
		}
		if item.enabled {
			sessionUUID := seedTranscriptArchiveSession(t, database, scope)
			if err := bindSessionIdentityTx(ctx, store.mapperDB, InsertSessionIdentityParams{
				UUID: uuid.NewV4().String(), OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID,
				SessionUUID: sessionUUID, SessionExternalID: "ses_" + sessionUUID, IdentityUUID: identity.UUID,
				AgentExternalID: "agent_archive_test", ScopeKind: "console",
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	params := IdentityDirectoryParams{OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, Sort: "sessions", Limit: 1}
	check := func(expectID string, count, overviewIdentities int, recentDisabled bool) {
		t.Helper()
		raw, err := store.IdentityDirectory(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Data []struct {
				ID      string `json:"id"`
				Active  bool   `json:"active"`
				Enabled bool   `json:"enabled"`
			} `json:"data"`
			Total    int `json:"total_count"`
			Overview struct {
				Identities int `json:"identities"`
				Active     int `json:"active"`
			} `json:"overview"`
		}
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatal(err)
		}
		if result.Total != count || result.Overview.Identities != overviewIdentities {
			t.Fatalf("身份目录统计不匹配：%s", raw)
		}
		if expectID != "" && (len(result.Data) != 1 || result.Data[0].ID != expectID) {
			t.Fatalf("身份目录排序或过滤不匹配：%s", raw)
		}
		if recentDisabled && (result.Overview.Active != 1 || !result.Data[0].Active || result.Data[0].Enabled) {
			t.Fatalf("停用后应保留近期活跃记录：%s", raw)
		}
	}
	check("idn_active", 2, 2, false)
	params.Offset = 1
	check("idn_disabled", 2, 2, false)
	params.Offset, params.AgentID = 0, "agent_archive_test"
	check("idn_active", 1, 2, false)
	params.AgentID, params.Status = "", "disabled"
	check("idn_disabled", 1, 2, false)
	if err := store.SetBusinessIdentityEnabled(ctx, scope.WorkspaceUUID, "idn_active", false); err != nil {
		t.Fatal(err)
	}
	params.Status = "active"
	check("idn_active", 1, 2, true)
	params.OrganizationUUID = uuid.NewV4().String()
	check("", 0, 0, false)
}

func TestIdentityAgentUsagePaginationPostgres(t *testing.T) {
	ctx := t.Context()
	store, database := newTranscriptArchiveTestDB(t)
	scope := transcriptTestScope()
	identity, err := store.CreateBusinessIdentity(ctx, InsertIdentityParams{
		UUID: uuid.NewV4().String(), ExternalID: "idn_many_agents", OrganizationUUID: scope.OrganizationUUID,
		WorkspaceUUID: scope.WorkspaceUUID, Namespace: "test", ExternalSubjectID: "test", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range 101 {
		scope.CodeSessionUUID = uuid.NewV4().String()
		scope.CodeSessionExternalID = "cse_" + scope.CodeSessionUUID
		sessionUUID := seedTranscriptArchiveSession(t, database, scope)
		if err := bindSessionIdentityTx(ctx, store.mapperDB, InsertSessionIdentityParams{
			UUID: uuid.NewV4().String(), OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID,
			SessionUUID: sessionUUID, SessionExternalID: "ses_" + sessionUUID, IdentityUUID: identity.UUID,
			AgentExternalID: fmt.Sprintf("agent_%03d", index), ScopeKind: "console",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, "UPDATE sessions SET updated_at = now()"); err != nil {
		t.Fatal(err)
	}
	first, err := store.ListBusinessIdentityAgents(ctx, scope.WorkspaceUUID, identity.UUID, nil, 100, 0)
	if err != nil || len(first) != 100 || first[99].AgentExternalID != "agent_099" {
		t.Fatalf("第一页应稳定排序并返回 100 条，实际条数 %d，错误 %v", len(first), err)
	}
	second, err := store.ListBusinessIdentityAgents(ctx, scope.WorkspaceUUID, identity.UUID, nil, 100, 100)
	if err != nil || len(second) != 1 || second[0].AgentExternalID != "agent_100" {
		t.Fatalf("第二页应返回第 101 个 Agent，实际 %+v，错误 %v", second, err)
	}
}
