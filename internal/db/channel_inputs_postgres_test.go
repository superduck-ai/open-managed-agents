//go:build integration

package db

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"uuid"
)

func TestChannelDirectChatPostgres(t *testing.T) {
	store, database := newTranscriptArchiveTestDB(t)
	ctx := t.Context()
	scope := ChannelInputScope{OrganizationUUID: uuid.NewV4().String(), WorkspaceUUID: uuid.NewV4().String(), ChannelUUID: uuid.NewV4().String(), ChannelType: "feishu", AccountID: "app_fixture"}
	const channelID = "chnl_menu_test"
	_, err := database.ExecContext(ctx, `INSERT INTO channels(uuid,external_id,organization_uuid,workspace_uuid,agent_external_id,name,channel_type,mode,binding_status,app_id)
VALUES($1,$2,$3,$4,'agent_fixture','会话菜单','feishu','open','bound',$5)`, scope.ChannelUUID, channelID, scope.OrganizationUUID, scope.WorkspaceUUID, scope.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][3]string{
		{"group", "ou_actor", "oc_group"},
		{"p2p", "ou_other", "oc_other"},
	} {
		input := ChannelInput{
			MessageID: pair[2], ScopeKey: "chat:" + pair[2],
			Payload: fmt.Sprintf(`{"chat_type":%q,"user_id":%q,"chat_id":%q}`, pair[0], pair[1], pair[2]),
		}
		if _, fresh, err := store.ReceiveChannelInput(ctx, scope, input); err != nil || !fresh {
			t.Fatalf("写入测试消息失败: %v %v", fresh, err)
		}
	}
	if got, err := store.FindChannelDirectChat(ctx, scope, channelID, "ou_actor"); got != "" || err != nil {
		t.Fatalf("其他用户的私聊和本人的群聊都不能用于重置: %q %v", got, err)
	}
	legacy := transcriptTestScope()
	legacy.OrganizationUUID, legacy.WorkspaceUUID = scope.OrganizationUUID, scope.WorkspaceUUID
	sessionID := "ses_" + seedTranscriptArchiveSession(t, database, legacy)
	if _, err := database.ExecContext(ctx, `UPDATE sessions SET metadata='{"chat_type":"p2p","app_id":"app_fixture","open_id":"ou_actor"}' WHERE external_id=$1`, sessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChannelSessionBinding(ctx, channelID, "chat:oc_legacy", sessionID, "", 0); err != nil {
		t.Fatal(err)
	}
	if got, err := store.FindChannelDirectChat(ctx, scope, channelID, "ou_actor"); got != "oc_legacy" || err != nil {
		t.Fatalf("无法读取旧版私聊绑定: %q %v", got, err)
	}
	input := ChannelInput{MessageID: "om_direct", ScopeKey: "chat:oc_direct", Payload: `{"chat_type":"p2p","user_id":"ou_actor","chat_id":"oc_direct"}`}
	if _, fresh, err := store.ReceiveChannelInput(ctx, scope, input); err != nil || !fresh {
		t.Fatalf("写入私聊输入失败: %v %v", fresh, err)
	}
	if got, err := store.FindChannelDirectChat(ctx, scope, channelID, "ou_actor"); got != "oc_direct" || err != nil {
		t.Fatalf("应优先使用入站私聊记录: %q %v", got, err)
	}
	scope.WorkspaceUUID = uuid.NewV4().String()
	if got, err := store.FindChannelDirectChat(ctx, scope, channelID, "ou_actor"); got != "" || err != nil {
		t.Fatalf("不能读取其他工作区的私聊: %q %v", got, err)
	}
}

func TestChannelInputsPostgres(t *testing.T) {
	store, database := newTranscriptArchiveTestDB(t)
	scope := ChannelInputScope{OrganizationUUID: uuid.NewV4().String(), WorkspaceUUID: uuid.NewV4().String(), ChannelUUID: uuid.NewV4().String(), ChannelType: "other", AccountID: "app_fixture"}
	if _, err := database.ExecContext(t.Context(), `INSERT INTO channels(uuid,external_id,organization_uuid,workspace_uuid,agent_external_id,name,channel_type,mode,binding_status,app_id) VALUES($1,'chnl_inbox_fixture',$2,$3,'agent_fixture','输入交接','other','open','bound',$4)`, scope.ChannelUUID, scope.OrganizationUUID, scope.WorkspaceUUID, scope.AccountID); err != nil {
		t.Fatal(err)
	}
	receive := func(id string) ChannelInput {
		t.Helper()
		payload, err := json.Marshal(struct {
			Content    string   `json:"content"`
			Resources  []string `json:"resources"`
			MessageIDs []string `json:"message_ids"`
		}{id, []string{"attachment"}, []string{id}})
		if err != nil {
			t.Fatal(err)
		}
		row, fresh, err := store.ReceiveChannelInput(t.Context(), scope, ChannelInput{MessageID: id, ScopeKey: "chat:fixture", Payload: string(payload)})
		if err != nil || !fresh {
			t.Fatalf("原消息未接收: %+v %v %v", row, fresh, err)
		}
		return row
	}
	wait, fresh, err := store.ReceiveChannelInput(t.Context(), scope, ChannelInput{
		MessageID: "waiting", ScopeKey: "chat:fixture", Payload: `{"user_id":"actor","pairing_id":"pair"}`,
	})
	if err != nil || !fresh {
		t.Fatalf("创建历史等待回执：%v %v", fresh, err)
	}
	if _, found, err := store.ClaimChannelInput(t.Context(), scope, wait.UUID, "worker"); err != nil || !found {
		t.Fatalf("无法认领首次消息：%v %v", found, err)
	}
	if err := store.HoldPairingOAuth(t.Context(), scope, wait.UUID, "worker", `{"oauth_provider":"client","oauth_card":"card"}`); err != nil {
		t.Fatal(err)
	}
	card := ChannelPairingResume{MessageID: wait.MessageID, ScopeKey: wait.ScopeKey, ActorID: "other", PairingID: "pair"}
	if ok, err := store.HasWaitingChannelPairingInput(t.Context(), scope, card); err != nil || ok {
		t.Fatalf("其他操作者可以重放等待回执：%v %v", ok, err)
	}
	card.ActorID = "actor"
	if ok, err := store.HasWaitingChannelPairingInput(t.Context(), scope, card); err != nil || !ok {
		t.Fatalf("未找到原消息的等待回执：%v %v", ok, err)
	}
	if id, ok, err := store.ResumeChannelInputAfterPairing(t.Context(), scope, card); err != nil || !ok || id != wait.UUID {
		t.Fatalf("原等待回执未续跑：%s %v %v", id, ok, err)
	}
	if ok, err := store.HasWaitingChannelPairingInput(t.Context(), scope, card); err != nil || ok {
		t.Fatalf("旧确认卡可以重复续跑：%v %v", ok, err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE channel_inputs SET state='handed_off' WHERE uuid=$1`, wait.UUID); err != nil {
		t.Fatal(err)
	}

	a, b := receive("a"), receive("b")
	if _, fresh, err := store.ReceiveChannelInput(t.Context(), scope, ChannelInput{MessageID: "a", ScopeKey: "chat:fixture", Payload: `{"content":"不应覆盖"}`}); err != nil || fresh {
		t.Fatalf("重复回调不能再次接受或覆盖原文: %v %v", fresh, err)
	}
	for _, field := range []string{"组织", "工作区", "渠道", "类型", "应用"} {
		wrong := scope
		switch field {
		case "组织":
			wrong.OrganizationUUID = uuid.NewV4().String()
		case "工作区":
			wrong.WorkspaceUUID = uuid.NewV4().String()
		case "渠道":
			wrong.ChannelUUID = uuid.NewV4().String()
		case "类型":
			wrong.ChannelType = "different"
		case "应用":
			wrong.AccountID = "different"
		}
		if _, found, err := store.ClaimChannelInput(t.Context(), wrong, a.UUID, "outsider"); err != nil || found {
			t.Fatalf("%s不能越界认领: %v %v", field, found, err)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var owners []string
	for i := range 8 {
		owner := fmt.Sprintf("owner_%d", i)
		wg.Go(func() {
			_, found, err := store.ClaimChannelInput(t.Context(), scope, a.UUID, owner)
			if err != nil {
				t.Error(err)
				return
			}
			if found {
				mu.Lock()
				owners = append(owners, owner)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(owners) != 1 {
		t.Fatalf("同一原消息被多实例认领: %v", owners)
	}
	owner := owners[0]
	if _, found, err := store.ClaimChannelInput(t.Context(), scope, b.UUID, owner); err != nil || !found {
		t.Fatalf("第二原消息不能认领: %v %v", found, err)
	}
	batch := ChannelInputBatch{MessageIDs: []string{"a", "b"}, ScopeKey: "chat:fixture", Payload: `{"content":"完整合批","resources":[],"message_ids":["a","b"]}`}
	if _, err := database.ExecContext(t.Context(), `ALTER TABLE channel_inputs ADD CONSTRAINT test_batch_rollback CHECK(NOT(message_id='b' AND state='prepared'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareChannelInputBatch(t.Context(), scope, owner, batch); err == nil {
		t.Fatal("合批保存失败应回滚整批")
	}
	var state string
	if err := database.QueryRowContext(t.Context(), `SELECT state FROM channel_inputs WHERE uuid=$1`, a.UUID).Scan(&state); err != nil || state != "buffering" {
		t.Fatalf("合批失败丢失前面原消息: %s %v", state, err)
	}
	if _, err := database.ExecContext(t.Context(), `ALTER TABLE channel_inputs DROP CONSTRAINT test_batch_rollback`); err != nil {
		t.Fatal(err)
	}
	prepared, err := store.PrepareChannelInputBatch(t.Context(), scope, owner, batch)
	if err != nil || prepared.UUID != b.UUID || prepared.State != "prepared" || !strings.Contains(prepared.Payload, "完整合批") {
		t.Fatalf("合批不完整: %+v %v", prepared, err)
	}
	prepared, err = store.AdvanceChannelInput(t.Context(), scope, b.UUID, owner, ChannelInputAdvance{State: "prepared", SessionExternalID: "session_original", PreparedContent: `{"content":[{"type":"text","text":"已准备"}]}`})
	if err != nil || prepared.PreparedContent == "" {
		t.Fatalf("提交内容未保存: %+v %v", prepared, err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE channel_inputs SET lease_until=now()-interval '1 second' WHERE uuid=$1`, b.UUID); err != nil {
		t.Fatal(err)
	}
	if err := store.RenewChannelInputs(t.Context(), scope, owner); err != nil {
		t.Fatal(err)
	}
	recovered, found, err := store.ClaimChannelInput(t.Context(), scope, b.UUID, "restarted")
	if err != nil || !found || recovered.PreparedContent != prepared.PreparedContent || recovered.SessionExternalID != prepared.SessionExternalID {
		t.Fatalf("重启丢失原批次内容或会话: %+v %v %v", recovered, found, err)
	}
	if _, err := store.AdvanceChannelInput(t.Context(), scope, b.UUID, owner, ChannelInputAdvance{State: "accepted"}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("失权旧实例仍能提交回执: %v", err)
	}
	if _, err := store.AdvanceChannelInput(t.Context(), scope, b.UUID, "restarted", ChannelInputAdvance{State: "accepted", SessionExternalID: "session_original", InputEventID: "original_event"}); err != nil {
		t.Fatal(err)
	}
	var copied int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM channel_inputs WHERE channel_uuid=$1 AND (payload ? 'content' OR payload ? 'resources' OR prepared_content IS NOT NULL)`, scope.ChannelUUID).Scan(&copied); err != nil || copied != 0 {
		t.Fatalf("OMA接受后不应继续复制原输入正文: %d %v", copied, err)
	}
	if err := store.CompleteChannelInput(t.Context(), scope, b.UUID, "restarted"); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListRecoverableChannelInputs(t.Context(), scope)
	if err != nil || len(rows) != 0 {
		t.Fatalf("已交接批次不应再次恢复: %+v %v", rows, err)
	}
	var retained int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM channel_inputs WHERE channel_uuid=$1 AND (payload ? 'content' OR payload ? 'resources' OR prepared_content IS NOT NULL OR state<>'handed_off')`, scope.ChannelUUID).Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("交接完成后仍复制输入正文: %d %v", retained, err)
	}
	command := receive("command")
	if _, found, err := store.ClaimChannelInput(t.Context(), scope, command.UUID, "command_owner"); err != nil || !found {
		t.Fatalf("命令未认领: %v %v", found, err)
	}
	if _, err := store.PrepareChannelInputBatch(t.Context(), scope, "command_owner", ChannelInputBatch{MessageIDs: []string{"command"}, ScopeKey: command.ScopeKey, Payload: command.Payload}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdvanceChannelInput(t.Context(), scope, command.UUID, "command_owner", ChannelInputAdvance{State: "command_sending"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseChannelInput(t.Context(), scope, command.UUID, "command_owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE channel_inputs SET lease_until=now()-interval '1 second' WHERE uuid=$1`, command.UUID); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.ListRecoverableChannelInputs(t.Context(), scope); err != nil || len(rows) != 0 {
		t.Fatalf("未知命令结果不能自动再执行: %+v %v", rows, err)
	}
}

func TestPairingOAuthResumesMatchingInputsOnce(t *testing.T) {
	store, database := newTranscriptArchiveTestDB(t)
	scope := ChannelInputScope{OrganizationUUID: uuid.NewV4().String(), WorkspaceUUID: uuid.NewV4().String(), ChannelUUID: uuid.NewV4().String(), ChannelType: "feishu", AccountID: "app_fixture"}
	if _, err := database.ExecContext(t.Context(), `INSERT INTO channels(uuid,external_id,organization_uuid,workspace_uuid,agent_external_id,name,channel_type,mode,binding_status,app_id) VALUES($1,'chnl_batch',$2,$3,'agent_fixture','配对恢复','feishu','pairing','bound',$4)`, scope.ChannelUUID, scope.OrganizationUUID, scope.WorkspaceUUID, scope.AccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `INSERT INTO channel_pairings(external_id,organization_uuid,workspace_uuid,channel_uuid,channel_external_id,scope_kind,platform_account,remote_id) VALUES('pair',$1,$2,$3,'chnl_batch','direct',$4,'actor')`, scope.OrganizationUUID, scope.WorkspaceUUID, scope.ChannelUUID, scope.AccountID); err != nil {
		t.Fatal(err)
	}
	const receipt = `{"pairing_id":"pair","oauth_card":"om_auth"}`
	var lateInput ChannelInput
	for _, input := range []struct{ id, chat, actor, provider string }{
		{"one", "chat", "actor", "client"},
		{"two", "chat", "actor", "client"},
		{"late", "chat", "actor", "client"},
		{"other_chat", "different", "actor", "client"},
		{"other", "chat", "another", "client"},
		{"old", "chat", "actor", "previous"},
	} {
		row, _, err := store.ReceiveChannelInput(t.Context(), scope, ChannelInput{MessageID: input.id, ScopeKey: "thread:" + input.chat + ":" + input.id, Payload: fmt.Sprintf(`{"chat_id":%q,"user_id":%q,"pairing_id":"pair","oauth_provider":%q}`, input.chat, input.actor, input.provider)})
		if err != nil {
			t.Fatal(err)
		}
		if _, found, err := store.ClaimChannelInput(t.Context(), scope, row.UUID, "worker"); err != nil || !found {
			t.Fatalf("首次认领失败：%v", err)
		}
		if input.id == "late" {
			lateInput = row
			continue
		}
		if err := store.HoldPairingOAuth(t.Context(), scope, row.UUID, "worker", receipt); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ActivateScopePairingByID(t.Context(), ActivateScopePairingParams{
		WorkspaceUUID: scope.WorkspaceUUID, ExternalID: "pair", IdentityUUID: uuid.NewV4().String(), AgentExternalID: "agent_fixture",
		ResumeOAuth: &ChannelOAuthResume{Scope: scope, ActorID: "actor", ChatID: "chat", Provider: "client"},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListRecoverableChannelInputs(t.Context(), scope)
	if err != nil || len(rows) != 2 || rows[0].MessageID != "one" || rows[1].MessageID != "two" {
		t.Fatalf("激活后首次恢复前退出，等待消息不匹配：%+v %v", rows, err)
	}
	for _, row := range rows {
		if row.ScopeKey != "thread:chat:"+row.MessageID || row.State != "received" {
			t.Fatalf("等待消息恢复到了错误的作用域或状态：%+v", row)
		}
		if _, ok, err := store.ClaimChannelInput(t.Context(), scope, row.UUID, "restart"); err != nil || !ok {
			t.Fatalf("恢复的消息无法认领：%v", err)
		}
	}
	if err := store.HoldPairingOAuth(t.Context(), scope, lateInput.UUID, "worker", receipt); err != nil {
		t.Fatal(err)
	}
	late, err := store.ListRecoverableChannelInputs(t.Context(), scope)
	if err != nil || len(late) != 1 || late[0].UUID != lateInput.UUID || late[0].ScopeKey != lateInput.ScopeKey || late[0].State != "received" {
		t.Fatalf("新进程未发现授权后登记的原话题消息：%+v %v", late, err)
	}
	if row, ok, err := store.ClaimChannelInput(t.Context(), scope, lateInput.UUID, "restart"); err != nil || !ok || row.State != "buffering" {
		t.Fatalf("新进程无法认领授权后登记的消息：%+v %v", row, err)
	}
	again, err := store.ResumeOAuthWaiters(t.Context(), scope, ChannelPairingResume{ActorID: "actor", PairingID: "pair"}, "chat", "client")
	if err != nil || len(again) != 0 {
		t.Fatalf("重复回调恢复了已受理的消息：%+v %v", again, err)
	}
}
