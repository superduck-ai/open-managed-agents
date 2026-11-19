//go:build integration

package db

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"uuid"
)

func TestIdentityChannelBindingsPostgres(t *testing.T) {
	store, database := newTranscriptArchiveTestDB(t)
	ctx := t.Context()
	scope := transcriptTestScope()
	identity := uuid.NewV4().String()
	other := uuid.NewV4().String()
	const agent = "agent_identity_channels"
	fixed := uuid.NewV4().String()
	paired := uuid.NewV4().String()
	removed := uuid.NewV4().String()
	seedChannel := func(channelUUID, id, binding string) {
		t.Helper()
		if _, err := database.ExecContext(ctx, `INSERT INTO channels
			(uuid,external_id,organization_uuid,workspace_uuid,agent_external_id,name,channel_type,mode,binding_status,app_id)
			VALUES($1,$2,$3,$4,$5,$2,'feishu','open',$6,$2)`, channelUUID, id, scope.OrganizationUUID, scope.WorkspaceUUID, agent, binding); err != nil {
			t.Fatal(err)
		}
	}
	seedChannel(fixed, "chnl_fixed", "unbound")
	seedChannel(paired, "chnl_paired", "bound")
	seedChannel(removed, "chnl_stale", "bound")
	if _, err := database.ExecContext(ctx, `UPDATE channels SET identity_resolution_mode='fixed', identity_uuid=$1 WHERE uuid=$2`, identity, fixed); err != nil {
		t.Fatal(err)
	}
	seedIdentityChannelPairing(t, ctx, database, scope, paired, "chnl_paired", "chnl_paired", identity, agent, "one", "active")
	seedIdentityChannelPairing(t, ctx, database, scope, paired, "chnl_paired", "chnl_paired", identity, agent, "two", "active")
	seedIdentityChannelPairing(t, ctx, database, scope, removed, "chnl_stale", "previous-app", identity, agent, "old-app", "active")
	seedIdentityChannelPairing(t, ctx, database, scope, removed, "chnl_stale", "chnl_stale", identity, agent, "old-scope", "unbound")
	seedIdentityChannelPairing(t, ctx, database, scope, paired, "chnl_paired", "chnl_paired", other, agent, "other-user", "active")

	rows, err := store.ListIdentityChannels(ctx, scope.WorkspaceUUID, identity)
	want := []IdentityChannelBindingRecord{
		{AgentExternalID: agent, ChannelExternalID: "chnl_fixed", ChannelType: "feishu", Name: "chnl_fixed", BindingStatus: "unbound"},
		{AgentExternalID: agent, ChannelExternalID: "chnl_paired", ChannelType: "feishu", Name: "chnl_paired", BindingStatus: "bound"},
	}
	if err != nil || !slices.Equal(rows, want) {
		t.Fatalf("渠道关联不匹配：%+v %v", rows, err)
	}
	rows, err = store.ListIdentityChannels(ctx, uuid.NewV4().String(), identity)
	if err != nil || len(rows) != 0 {
		t.Fatalf("工作区范围未隔离：%+v %v", rows, err)
	}
}

func seedIdentityChannelPairing(t *testing.T, ctx context.Context, database *sql.DB, scope TranscriptScope,
	channelUUID, channelID, appID, identity, agent, remote, status string,
) {
	t.Helper()
	_, err := database.ExecContext(ctx, `INSERT INTO channel_pairings
		(external_id,organization_uuid,workspace_uuid,channel_uuid,channel_external_id,scope_kind,platform_account,remote_id,status,identity_uuid,agent_external_id)
		VALUES($1,$2,$3,$4,$5,'direct',$6,$1,$7,$8,$9)`, remote, scope.OrganizationUUID, scope.WorkspaceUUID,
		channelUUID, channelID, appID, status, identity, agent)
	if err != nil {
		t.Fatal(err)
	}
}
