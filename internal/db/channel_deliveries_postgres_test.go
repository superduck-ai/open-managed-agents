//go:build integration

package db

import (
	"database/sql"
	"encoding/json/v2"
	"errors"
	"sync"
	"testing"
	"time"
)

func testChannelReplyDeliveriesPostgres(t *testing.T, store *DB, database *sql.DB, scope ChannelDeliveryScope, otherWorkspace string) {
	t.Helper()
	ctx := t.Context()
	input := ChannelReplyDelivery{ChannelType: "feishu", AccountID: "app_1", InputEventID: "event_1", SessionExternalID: "session_1", ScopeKey: "chat:chat_1", MessageID: "message_1", ChatID: "chat_1", ChatType: "p2p", ActorID: "actor_1", IncludeThinking: true, IncludeToolCalls: true}
	input.ArtifactDelivery = `{"baseline":{"resource":"file:version"},"planned":false,"files":null}`
	row, err := store.PrepareChannelReplyDelivery(ctx, scope, input)
	if err != nil || row.State != "pending" || row.ChannelType != "feishu" || row.AccountID != "app_1" {
		t.Fatalf("准备回执: %+v %v", row, err)
	}
	changed := input
	changed.ChatID = "不应覆盖"
	row, err = store.PrepareChannelReplyDelivery(ctx, scope, changed)
	if err != nil || row.ChatID != input.ChatID {
		t.Fatalf("重复准备覆盖原回执: %+v %v", row, err)
	}
	wrong := scope
	wrong.WorkspaceUUID = otherWorkspace
	if _, err := store.PrepareChannelReplyDelivery(ctx, wrong, input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨工作区读取回执: %v", err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := []string{}
	for _, owner := range []string{"owner_a", "owner_b"} {
		wg.Go(func() {
			_, ok, err := store.ClaimChannelReplyDelivery(ctx, scope, input.InputEventID, owner)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				winners = append(winners, owner)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(winners) != 1 {
		t.Fatalf("首次发送应只有一个持有者: %v", winners)
	}
	owner := winners[0]
	if err := store.SaveChannelReplyReceipt(ctx, scope, input.InputEventID, owner, "card_1"); err != nil {
		t.Fatal(err)
	}
	for _, denied := range []struct {
		scope ChannelDeliveryScope
		owner string
	}{{wrong, owner}, {scope, "other_owner"}} {
		if err := store.SaveChannelArtifactDelivery(ctx, denied.scope, input.InputEventID, denied.owner, `{"files":[]}`); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("非持有者或跨工作区不得覆盖文件交接: %v", err)
		}
	}
	checkpoint := `{"baseline":{"resource":"file:version"},"planned":true,"files":[{"state":"sent","message_id":"file_message"}]}`
	if err := store.SaveChannelArtifactDelivery(ctx, scope, input.InputEventID, owner, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveChannelArtifactDelivery(ctx, scope, input.InputEventID, owner, `{`); err == nil {
		t.Fatal("非法JSON不能覆盖已有交接")
	}
	saved, err := store.GetChannelReplyDeliveryByReceipt(ctx, scope, "card_1")
	var artifact struct {
		Baseline map[string]string `json:"baseline"`
		Files    []struct {
			MessageID string `json:"message_id"`
		} `json:"files"`
	}
	if err != nil || json.Unmarshal([]byte(saved.ArtifactDelivery), &artifact) != nil || artifact.Baseline["resource"] != "file:version" || len(artifact.Files) != 1 || artifact.Files[0].MessageID != "file_message" {
		t.Fatalf("JSON交接回读不完整: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE channel_reply_deliveries SET created_at=now()-interval '1 day' WHERE channel_external_id=$1 AND input_event_id=$2`, scope.ChannelExternalID, input.InputEventID); err != nil {
		t.Fatal(err)
	}
	anchored, err := store.GetChannelReplyDeliveryByReceipt(ctx, scope, "card_1")
	if err != nil || anchored.InputEventID != input.InputEventID || anchored.SessionExternalID != input.SessionExternalID {
		t.Fatalf("卡片消息必须定位原输入关联: %+v %v", anchored, err)
	}
	if _, err := store.GetChannelReplyDeliveryByReceipt(ctx, wrong, "card_1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("卡片回执查询不能跨工作区: %v", err)
	}
	if _, err := store.GetChannelReplyDeliveryByReceipt(ctx, scope, "forwarded_card"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未交付的消息不能冒充原卡: %v", err)
	}
	deadline, found, err := store.NextChannelReplyRecovery(ctx, scope, "other_instance")
	if err != nil || !found || !deadline.After(time.Now()) {
		t.Fatalf("应读取未到期的交付租约: %v %v %v", deadline, found, err)
	}
	for _, excluded := range []struct {
		scope ChannelDeliveryScope
		owner string
	}{{wrong, "other_instance"}, {scope, owner}} {
		if _, found, err := store.NextChannelReplyRecovery(ctx, excluded.scope, excluded.owner); err != nil || found {
			t.Fatalf("恢复调度必须隔离工作区并排除自身租约: %v %v", found, err)
		}
	}
	if err := store.RenewChannelReplyDelivery(ctx, wrong, input.InputEventID, owner); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("跨工作区续租: %v", err)
	}
	for _, denied := range []struct {
		scope ChannelDeliveryScope
		owner string
	}{{wrong, owner}, {scope, "other_owner"}} {
		if err := store.ReleaseChannelReplyDelivery(ctx, denied.scope, input.InputEventID, denied.owner); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("不允许跨租户或非持有者释放: %v", err)
		}
	}
	if err := store.ReleaseChannelReplyDelivery(ctx, scope, input.InputEventID, owner); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListChannelReplyDeliveries(ctx, scope)
	if err != nil || len(rows) != 1 || rows[0].ReplyMessageID != "card_1" {
		t.Fatalf("恢复列表: %+v %v", rows, err)
	}
	resumed, ok, err := store.ClaimChannelReplyDelivery(ctx, scope, input.InputEventID, "owner_new")
	if err != nil || !ok || resumed.State != "sent" || resumed.ReplyMessageID != "card_1" {
		t.Fatalf("应恢复原卡回执: %+v %v %v", resumed, ok, err)
	}
	if err := store.RenewChannelReplyDelivery(ctx, scope, input.InputEventID, owner); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("旧持有者不应继续更新: %v", err)
	}
	if err := store.ReleaseChannelReplyDelivery(ctx, scope, input.InputEventID, owner); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("旧实例不得释放新实例租约: %v", err)
	}
	if err := store.CompleteChannelReplyDelivery(ctx, scope, input.InputEventID, "owner_new"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveChannelArtifactDelivery(ctx, scope, input.InputEventID, "owner_new", `{}`); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("已完成的文件交接不能被重写: %v", err)
	}
	rows, err = store.ListChannelReplyDeliveries(ctx, scope)
	if err != nil || len(rows) != 0 {
		t.Fatalf("已交付卡片不应恢复: %+v %v", rows, err)
	}
	if err := store.ReleaseChannelReplyDelivery(ctx, scope, input.InputEventID, "owner_new"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("已完成交付不得重新放行: %v", err)
	}
	input.InputEventID = "event_unknown"
	if _, err := store.PrepareChannelReplyDelivery(ctx, scope, input); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ClaimChannelReplyDelivery(ctx, scope, input.InputEventID, "owner_unknown"); err != nil || !ok {
		t.Fatalf("发送许可: %v %v", ok, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE channel_reply_deliveries SET lease_until=now()-interval '1 second' WHERE channel_external_id=$1 AND input_event_id=$2`, scope.ChannelExternalID, input.InputEventID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ClaimChannelReplyDelivery(ctx, scope, input.InputEventID, "owner_retry"); err != nil || ok {
		t.Fatalf("发送结果未知不可重发: %v %v", ok, err)
	}
}
