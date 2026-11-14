package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type postFileCaptureStore struct {
	*fakeRuntimeStore
	input db.ChannelInput
}

var errPostFileCaptured = errors.New("已捕获规范化消息")

func (s *postFileCaptureStore) ReceiveChannelInput(_ context.Context, _ db.ChannelInputScope, input db.ChannelInput) (db.ChannelInput, bool, error) {
	s.input = input
	return db.ChannelInput{}, false, errPostFileCaptured
}

func TestFeishuPostAttachmentZoneReachesDurableInput(t *testing.T) {
	for _, kind := range []string{"normal", "localized", "inline duplicate", "file only", "folder"} {
		t.Run(kind, func(t *testing.T) {
			body := map[string]any{"title": "验收", "content": [][]map[string]string{{{"tag": "text", "text": "比较两份附件"}}}, "files": []map[string]any{{"file_key": "first", "file_name": "data.csv", "is_folder": kind == "folder"}, {"file_key": "second", "file_name": "data.csv", "is_folder": false}}}
			if kind == "inline duplicate" {
				body["content"] = [][]map[string]string{{{"tag": "media", "file_key": "first"}}}
			}
			if kind == "file only" {
				body["content"] = [][]map[string]string{}
			}
			if kind == "localized" {
				files := body["files"]
				delete(body, "files")
				body = map[string]any{"zh_cn": body, "files": files}
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			content := string(encoded)
			event := &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: new("actor")}}, Message: &larkim.EventMessage{MessageId: new("message"), ChatId: new("chat"), ChatType: new("p2p"), MessageType: new("post"), Content: &content}}}
			base := &fakeRuntimeStore{}
			worker, _ := testWorker(t, base, "http://unused")
			store := &postFileCaptureStore{fakeRuntimeStore: base}
			worker.supervisor.store = store
			if err := worker.dispatchMessage(t.Context(), normalize.ParseMessage(event)); !errors.Is(err, errPostFileCaptured) {
				t.Fatal(err)
			}
			var message channelMessage
			if err := json.Unmarshal([]byte(store.input.Payload), &message); err != nil {
				t.Fatal(err)
			}
			if len(message.Resources) != 2 || message.Resources[0].FileKey != "first" || message.Resources[1].FileKey != "second" || message.Resources[0].FileName != "data.csv" || message.Resources[1].FileName != "data.csv" {
				t.Fatalf("附件区必须在持久接收前保留原顺序、文件名并去重: %+v", message.Resources)
			}
			if kind == "folder" && message.Resources[0].Type != "folder" {
				t.Fatal("文件夹不能伪装成普通文件")
			}
			if kind == "file only" && message.Content == "[rich text message]" {
				t.Fatal("纯附件不能把SDK占位正文发给模型")
			}
		})
	}
}

func TestMalformedPostFilesCannotBecomePlainTextInput(t *testing.T) {
	body := `{"content":[[{"tag":"text","text":"不能只提交正文"}]],"files":[{"file_key":123}]}`
	event := &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: new("actor")}}, Message: &larkim.EventMessage{MessageId: new("message"), ChatId: new("chat"), ChatType: new("p2p"), MessageType: new("post"), Content: &body}}}
	base := &fakeRuntimeStore{}
	worker, _ := testWorker(t, base, "http://unused")
	store := &postFileCaptureStore{fakeRuntimeStore: base}
	worker.supervisor.store = store
	if err := worker.dispatchMessage(t.Context(), normalize.ParseMessage(event)); err == nil || errors.Is(err, errPostFileCaptured) || store.input.Payload != "" {
		t.Fatalf("附件结构错误不能静默降级为纯正文: err=%v input=%s", err, store.input.Payload)
	}
}
