package channels

import (
	"encoding/json/v2"
	"errors"
	"strconv"
	"testing"
	"time"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
)

func TestFeishuEventReturnsPersistenceFailureToDispatcher(t *testing.T) {
	failure := errors.New("接收存储不可用")
	worker, _ := testWorker(t, &fakeRuntimeStore{receiveInputErr: failure}, "http://unused")
	adapter := worker.adapter.(*feishuChannel)
	dispatcher := larkevent.NewEventDispatcher("", "")
	dispatcher.OnP2MessageReceiveV1(adapter.receiveEvent)
	payload, err := json.Marshal(map[string]any{
		"schema": "2.0", "header": map[string]string{"event_type": "im.message.receive_v1", "create_time": strconv.FormatInt(time.Now().UnixMilli(), 10)},
		"event": map[string]any{
			"sender":  map[string]any{"sender_id": map[string]string{"open_id": "actor"}, "sender_type": "user"},
			"message": map[string]any{"message_id": "message", "chat_id": "chat", "chat_type": "p2p", "message_type": "text", "create_time": strconv.FormatInt(time.Now().UnixMilli(), 10), "content": `{"text":"不能提前确认"}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Do(t.Context(), payload); !errors.Is(err, failure) {
		t.Fatalf("持久接收失败不能返回成功ACK: %v", err)
	}
}

func TestFeishuDurableEntryRetainsMentionSelfAndStalePolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		chatType string
		actor    string
		mention  bool
		stale    bool
		accepted bool
	}{
		{name: "群消息未提及不入库", chatType: "group", actor: "actor"},
		{name: "机器人自己不入库", chatType: "p2p", actor: "bot"},
		{name: "过期消息不入库", chatType: "p2p", actor: "actor", stale: true},
		{name: "单聊进入持久接收", chatType: "p2p", actor: "actor", accepted: true},
		{name: "群提及进入持久接收", chatType: "group", actor: "actor", mention: true, accepted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := errors.New("已到达持久接收入口")
			worker, fake := testWorker(t, &fakeRuntimeStore{receiveInputErr: failure}, "http://unused")
			fake.identity = &channeltypes.BotIdentity{OpenID: "bot"}
			created := time.Now()
			if test.stale {
				created = created.Add(-time.Hour)
			}
			message := map[string]any{"message_id": "message", "chat_id": "chat", "chat_type": test.chatType, "message_type": "text", "create_time": strconv.FormatInt(created.UnixMilli(), 10), "content": `{"text":"输入"}`}
			if test.mention {
				message["mentions"] = []map[string]any{{"key": "@_user_1", "id": map[string]string{"open_id": "bot"}, "name": "机器人"}}
			}
			payload, err := json.Marshal(map[string]any{"schema": "2.0", "header": map[string]string{"event_type": "im.message.receive_v1", "create_time": strconv.FormatInt(created.UnixMilli(), 10)}, "event": map[string]any{"sender": map[string]any{"sender_id": map[string]string{"open_id": test.actor}, "sender_type": "user"}, "message": message}})
			if err != nil {
				t.Fatal(err)
			}
			dispatcher := larkevent.NewEventDispatcher("", "")
			dispatcher.OnP2MessageReceiveV1(worker.adapter.(*feishuChannel).receiveEvent)
			_, err = dispatcher.Do(t.Context(), payload)
			if test.accepted && !errors.Is(err, failure) || !test.accepted && err != nil {
				t.Fatalf("平台入口策略改变: accepted=%v err=%v", test.accepted, err)
			}
		})
	}
}
