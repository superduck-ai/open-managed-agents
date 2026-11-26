package db

import (
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestChannelInputMapperScopesAndPayloadBindings(t *testing.T) {
	scope := ChannelInputScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", ChannelUUID: "channel", ChannelType: "other", AccountID: "account"}
	batch := ChannelInputBatch{MessageIDs: []string{"a", "b"}, ScopeKey: "chat", Payload: `{"content":"原消息"}`}
	queries := []yourbatis.BoundSQL{
		buildChannelInputMapperInsert(yourbatis.DialectPostgres, scope, ChannelInput{MessageID: "a", ScopeKey: "chat", Payload: batch.Payload}),
		buildChannelInputMapperClaim(yourbatis.DialectPostgres, scope, "input", "owner"),
		buildChannelInputMapperListRecoverable(yourbatis.DialectPostgres, scope),
		buildChannelInputMapperLockSources(yourbatis.DialectPostgres, scope, "owner", batch),
		buildChannelInputMapperGroupSources(yourbatis.DialectPostgres, scope, "owner", "anchor", batch),
		buildChannelInputMapperPrepare(yourbatis.DialectPostgres, scope, "input", "owner", batch),
		buildChannelInputMapperAdvance(yourbatis.DialectPostgres, scope, "input", "owner", ChannelInputAdvance{State: "prepared", PreparedContent: `{"content":[]}`}),
		buildChannelInputMapperComplete(yourbatis.DialectPostgres, scope, "input", "owner"),
		buildChannelInputMapperRelease(yourbatis.DialectPostgres, scope, "input", "owner"),
		buildChannelInputMapperRenew(yourbatis.DialectPostgres, scope, "owner"),
		buildChannelInputMapperHasPairingWait(yourbatis.DialectPostgres, scope, ChannelPairingResume{MessageID: "msg", ActorID: "actor", PairingID: "pair", ScopeKey: "key"}),
		buildChannelInputMapperResumeAfterPairing(yourbatis.DialectPostgres, scope, ChannelPairingResume{MessageID: "msg", ActorID: "actor", PairingID: "pair", ScopeKey: "key"}),
	}
	for _, query := range queries {
		for _, name := range []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.ChannelUUID", "scope.ChannelType", "scope.AccountID"} {
			found := false
			for _, arg := range query.Args {
				if arg.Name == name {
					found = true
				}
			}
			if !found {
				t.Fatalf("SQL缺少渠道租户范围%s: %s", name, query.SQL)
			}
		}
		for _, arg := range query.Args {
			payload := arg.Name == "input.Payload" || arg.Name == "batch.Payload" || arg.Name == "change.PreparedContent"
			if arg.Sensitive != payload {
				t.Fatalf("输入正文敏感标记错误: %s %v", arg.Name, arg.Sensitive)
			}
		}
	}
	assertMapperSQLContains(t, queries[0], "ON CONFLICT(channel_uuid,channel_type,account_id,message_id) DO NOTHING")
	assertMapperSQLContains(t, queries[3], "ORDER BY i.created_at,i.uuid FOR UPDATE")
	assertMapperSQLContains(t, queries[7], "payload=payload-'content'-'resources'")
	for _, query := range queries[10:] {
		for _, part := range []string{"i.state='pairing_wait'", "i.message_id=$9", "i.scope_key=$6", "i.payload->>'user_id'=$7", "i.payload->>'pairing_id'=$8"} {
			assertMapperSQLContains(t, query, part)
		}
	}
	if strings.Contains(queries[2].SQL, "command_sending") {
		t.Fatal("命令交接未知时不能自动再次执行")
	}
}

func TestChannelInputMapperFindsDirectChatsForOneActor(t *testing.T) {
	scope := ChannelInputScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", ChannelUUID: "channel", ChannelType: "feishu", AccountID: "app"}
	query := buildChannelInputMapperFindDirectChat(yourbatis.DialectPostgres, scope, "chnl_1", "ou_1")
	for _, part := range []string{
		"i.organization_uuid=$1 AND i.workspace_uuid=$2",
		"i.channel_uuid=$3 AND i.channel_type=$4 AND i.account_id=$5",
		"i.payload->>'chat_type'='p2p' AND i.payload->>'user_id'=$6",
		"s.workspace_uuid=$7", "b.channel_external_id=$8 AND s.organization_uuid=$9",
		"s.metadata->>'app_id'=$10 AND s.metadata->>'chat_type'='p2p'",
		"COALESCE(s.metadata->>'actor_id',s.metadata->>'open_id')=$11",
	} {
		assertMapperSQLContains(t, query, part)
	}
}
