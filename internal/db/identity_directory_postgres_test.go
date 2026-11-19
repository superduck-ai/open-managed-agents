//go:build integration

package db

import (
	json "encoding/json/v2"
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
	check := func(expectID string, count, overviewIdentities int) {
		t.Helper()
		raw, err := store.IdentityDirectory(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Total    int `json:"total_count"`
			Overview struct {
				Identities int `json:"identities"`
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
	}
	check("idn_active", 2, 2)
	params.Offset = 1
	check("idn_disabled", 2, 2)
	params.Offset, params.AgentID = 0, "agent_archive_test"
	check("idn_active", 1, 2)
	params.AgentID, params.Status = "", "disabled"
	check("idn_disabled", 1, 2)
	params.OrganizationUUID = uuid.NewV4().String()
	check("", 0, 0)
}
