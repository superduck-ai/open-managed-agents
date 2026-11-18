package channels

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChannelInputRecoversAcceptedEventWithoutResubmission(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"响应丢失", http.StatusServiceUnavailable, `暂时不可用`},
		{"回执损坏", http.StatusOK, `{`},
		{"缺少事件编号", http.StatusOK, `{"data":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			expected := channelInputMetadata{ChannelID: "channel", Type: "other", AccountID: "account", MessageID: "message", MessageIDs: []string{"earlier", "message"}, ChatID: "chat", ActorID: "actor"}
			posts, gets := 0, 0
			var accepted channelInputMetadata
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					posts++
					if r.Header.Get("Idempotency-Key") == "" || r.Header.Get("Idempotency-Key") != expected.requestKey() {
						t.Error("渠道输入缺少稳定幂等请求标识")
					}
					var body struct {
						Events []struct {
							Metadata channelInputMetadata `json:"metadata"`
						} `json:"events"`
					}
					if err := json.UnmarshalRead(r.Body, &body); err != nil {
						t.Error(err)
						return
					}
					if len(body.Events) != 1 {
						t.Error("一次请求只应提交一个输入")
						return
					}
					accepted = body.Events[0].Metadata
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte(test.body))
					return
				}
				gets++
				if err := json.MarshalWrite(w, struct {
					Data []omaEvent `json:"data"`
				}{[]omaEvent{{ID: "accepted", Type: "user.message", Metadata: metadataJSON(t, accepted)}}}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			ids, err := newOMAClient(server.URL).sendContent(t.Context(), "key", "session", []map[string]any{{"type": "text", "text": "原始输入"}}, &expected)
			if err != nil || len(ids) != 1 || ids[0] != "accepted" || posts != 1 || gets != 1 || !accepted.matches(expected) {
				t.Fatalf("应只核对OMA原记录，不重发: ids=%v err=%v posts=%d gets=%d metadata=%+v", ids, err, posts, gets, accepted)
			}
		})
	}
}

func TestChannelInputRequestKeyScopes(t *testing.T) {
	base := channelInputMetadata{ChannelID: "channel", Type: "other", AccountID: "account", MessageID: "message"}
	for _, field := range []string{"渠道", "类型", "账号", "消息"} {
		other := base
		switch field {
		case "渠道":
			other.ChannelID = "different"
		case "类型":
			other.Type = "different"
		case "账号":
			other.AccountID = "different"
		case "消息":
			other.MessageID = "different"
		}
		if other.requestKey() == base.requestKey() {
			t.Fatalf("%s改变后不能共用幂等标识", field)
		}
	}
	if len(base.requestKey()) > 128 {
		t.Fatal("请求标识必须符合接口上限")
	}
}

func TestChannelInputRequiresExactAssociation(t *testing.T) {
	for _, field := range []string{"渠道", "类型", "账号", "消息", "来源缺失", "来源不全", "来源顺序", "来源内容", "聊天", "操作者", "非用户事件", "无记录", "重复记录", "其他格式的元数据"} {
		t.Run(field, func(t *testing.T) {
			expected := channelInputMetadata{ChannelID: "channel", Type: "feishu", AccountID: "account", MessageID: "message", MessageIDs: []string{"earlier", "message"}, ChatID: "chat", ActorID: "actor"}
			actual := expected
			switch field {
			case "渠道":
				actual.ChannelID = "other"
			case "类型":
				actual.Type = "other"
			case "账号":
				actual.AccountID = "other"
			case "消息":
				actual.MessageID = "other"
			case "来源缺失":
				actual.MessageIDs = nil
			case "来源不全":
				actual.MessageIDs = []string{"message"}
			case "来源顺序":
				actual.MessageIDs = []string{"message", "earlier"}
			case "来源内容":
				actual.MessageIDs = []string{"other", "message"}
			case "聊天":
				actual.ChatID = "other"
			case "操作者":
				actual.ActorID = "other"
			}
			events := []omaEvent{{ID: "accepted", Type: "user.message", Metadata: metadataJSON(t, actual)}}
			if field == "其他格式的元数据" {
				events[0].Metadata = []byte(`"其他客户端的元数据"`)
			}
			if field == "非用户事件" {
				events[0].Type = "agent.message"
			}
			if field == "无记录" {
				events = nil
			}
			if field == "重复记录" {
				events = append(events, omaEvent{ID: "second", Type: "user.message", Metadata: metadataJSON(t, actual)})
			}
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
					http.Error(w, "未知", http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.MarshalWrite(w, struct {
					Data []omaEvent `json:"data"`
				}{events}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			ids, err := newOMAClient(server.URL).sendContent(t.Context(), "key", "session", []map[string]any{{"type": "text", "text": "相同正文不能代替消息标识"}}, &expected)
			if !errors.Is(err, errChannelInputUnconfirmed) || len(ids) != 0 || posts != 1 {
				t.Fatalf("关联不确定时不应认领或重发: ids=%v err=%v posts=%d", ids, err, posts)
			}
		})
	}
}

func metadataJSON(t *testing.T, value channelInputMetadata) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
