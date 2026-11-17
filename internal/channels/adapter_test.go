package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type independentAdapter struct {
	online      func(bool)
	opens       int
	resumed     []string
	toolDetails []string
	mu          sync.Mutex
	texts       []string
	replies     []string
	statuses    []string
	artifacts   []string
	downloads   []string
	started     chan struct{}
	stopped     chan struct{}
}

func (a *independentAdapter) Start(ctx context.Context) error {
	a.online(true)
	close(a.started)
	<-ctx.Done()
	close(a.stopped)
	return ctx.Err()
}
func (a *independentAdapter) Close(context.Context) error { return nil }
func (a *independentAdapter) ReplyText(_ context.Context, _ *channelMessage, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.texts = append(a.texts, text)
	return nil
}
func (a *independentAdapter) Receipt() string { return "independent_card" }
func (a *independentAdapter) ResumeReply(receipt string) channelReplyStream {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resumed = append(a.resumed, receipt)
	return a
}
func (a *independentAdapter) OpenReply(context.Context, *channelMessage) (channelReplyStream, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.opens++
	return a, nil
}
func (a *independentAdapter) Update(_ context.Context, reply contracts.Reply) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.replies = append(a.replies, reply.Text)
	for _, block := range reply.Blocks {
		if block.Tool != nil {
			a.toolDetails = append(a.toolDetails, block.Tool.Input+"|"+block.Tool.Output)
		}
	}
	a.statuses = append(a.statuses, reply.Status)
	return nil
}
func (a *independentAdapter) DownloadAttachment(_ context.Context, _ string, file channelAttachment, destination io.Writer) (string, error) {
	a.mu.Lock()
	a.downloads = append(a.downloads, file.FileKey)
	a.mu.Unlock()
	_, err := io.WriteString(destination, "channel_attachment")
	return "application/octet-stream", err
}

func (a *independentAdapter) SendMedia(_ context.Context, _ *channelMessage, file omaResource, _ string, data []byte) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.artifacts = append(a.artifacts, file.Filename+":"+string(data))
	return "independent-file-receipt", nil
}

func TestUnknownChannelTypeDoesNotFallBackToFeishu(t *testing.T) {
	supervisor := testRuntimeSupervisor(&fakeRuntimeStore{}, nil, nil, nil)
	worker, err := newChannelWorker(supervisor, db.ChannelRecord{ChannelType: "unknown", AppID: "account"}, "secret")
	appErr, ok := errors.AsType[*apperr.Error](err)
	if !ok || worker != nil || appErr.Kind != apperr.InvalidArgument || appErr.PublicMessage != "不支持的渠道类型：unknown" {
		t.Fatalf("未知类型必须明确拒绝，不能创建飞书实例: worker=%v err=%v", worker, err)
	}
}

func TestIndependentAdapterUsesSharedConversationAndFiles(t *testing.T) {
	var mu sync.Mutex
	sequence := 0
	writes := 0
	var events []omaEvent
	var resources []omaResource
	var metadata []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			var body struct {
				Metadata map[string]string `json:"metadata"`
			}
			if err := json.UnmarshalRead(r.Body, &body); err != nil {
				t.Error(err)
			}
			metadata = append(metadata, body.Metadata)
			_ = json.MarshalWrite(w, map[string]string{"id": "sesn_other"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			var body struct {
				Events []struct {
					Type    string            `json:"type"`
					Content []omaContentBlock `json:"content"`
				} `json:"events"`
			}
			if err := json.UnmarshalRead(r.Body, &body); err != nil {
				t.Error(err)
			}
			for _, event := range body.Events {
				if event.Type != "user.message" {
					continue
				}
				sequence++
				id := fmt.Sprintf("input_%d", sequence)
				events = append(events, omaEvent{ID: id, Type: "user.message"})
				text, _ := json.Marshal("来自其他渠道：" + event.Content[0].Text)
				events = append(events, omaEvent{ID: fmt.Sprintf("answer_%d", sequence), Type: "agent.message", Content: text}, omaEvent{ID: fmt.Sprintf("done_%d", sequence), Type: "session.status_idle", StopReason: []byte(`"end_turn"`)})
				if sequence == 1 {
					resources = append(resources, omaResource{ID: "output", Type: "file", FileID: "file_output", Filename: "result.csv", MimeType: "text/csv", MountPath: "/mnt/user-data/outputs/result.csv"})
				}
				_ = json.MarshalWrite(w, map[string]any{"data": []map[string]string{{"id": id}}})
				return
			}
			_, _ = w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/resources"):
			_ = json.MarshalWrite(w, map[string]any{"data": resources})
		case strings.HasSuffix(r.URL.Path, "/events"):
			_ = json.MarshalWrite(w, map[string]any{"data": events})
		case r.URL.Path == "/v1/files/file_output/content":
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte("a,b\n1,2\n"))
		case r.URL.Path == "/v1/files/file_output":
			_ = json.MarshalWrite(w, omaFile{ID: "file_output", Filename: "result.csv", MimeType: "text/csv", Size: 8})
		default:
			_, _ = w.Write([]byte(`{"id":"sesn_other","status":"idle","agent":{"id":"agent"},"environment_id":"environment"}`))
		}
	}))
	t.Cleanup(server.Close)
	record := db.ChannelRecord{ExternalID: "other_channel", OrganizationUUID: "org", WorkspaceUUID: "workspace", AgentExternalID: "agent", ChannelType: "test-channel", AppID: "account", Enabled: true, BindingStatus: "bound", Mode: "open"}
	store := &fakeRuntimeStore{record: record, bindings: map[string]string{}, env: "environment"}
	supervisor := testRuntimeSupervisor(store, nil, newOMAClient(server.URL), nil)
	supervisor.putAttachment = func(_ context.Context, _, _, attachmentPath, _ string, body io.Reader) (db.FileRecord, error) {
		data, err := io.ReadAll(body)
		if err != nil || string(data) != "channel_attachment" {
			t.Errorf("原生附件内容错误: %q %v", data, err)
		}
		mu.Lock()
		defer mu.Unlock()
		writes++
		return db.FileRecord{ExternalID: "file_mounted"}, err
	}
	supervisor.subscribeEvents = testEventSubscriber(t, func() []omaEvent { mu.Lock(); defer mu.Unlock(); return append([]omaEvent(nil), events...) })
	adapter := &independentAdapter{started: make(chan struct{}), stopped: make(chan struct{})}
	supervisor.adapters[record.ChannelType] = func(config contracts.AdapterConfig) (channelAdapter, error) {
		adapter.online = config.Online
		return adapter, nil
	}
	worker, err := newChannelWorker(supervisor, record, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	worker.start()
	<-adapter.started
	defer func() { worker.close(); <-adapter.stopped }()
	for _, message := range []*channelMessage{
		{MessageID: "other-message-1", ChatID: "other-room", ChatType: "p2p", UserID: "other-user", Content: "第一轮"},
		{MessageID: "other-message-2", ChatID: "other-room", ChatType: "p2p", UserID: "other-user", Content: "读取附件", Resources: []channelAttachment{{Type: "file", FileKey: "input-key", FileName: "input.txt"}}},
		{MessageID: "other-command", ChatID: "other-room", ChatType: "p2p", UserID: "other-user", Content: "/help"},
	} {
		scope, err := worker.messageScopeKey(t.Context(), message)
		if err != nil {
			t.Fatal(err)
		}
		if err := worker.handleMessage(t.Context(), message, scope); err != nil {
			t.Fatal(err)
		}
		waitForScope(t, worker, scope)
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	if len(metadata) != 1 || metadata[0]["source"] != "test-channel" || metadata[0]["actor_id"] != "other-user" || metadata[0]["open_id"] != "" {
		t.Fatalf("跨渠道会话应保留自有来源身份并复用绑定: %v", metadata)
	}
	if sequence != 2 || !strings.Contains(strings.Join(adapter.replies, ""), "来自其他渠道：读取附件") || len(adapter.texts) != 1 {
		t.Fatalf("其他适配器必须复用普通对话和本地命令: runs=%d replies=%v notices=%v", sequence, adapter.replies, adapter.texts)
	}
	if writes != 1 || len(adapter.downloads) != 1 || len(adapter.artifacts) != 1 || !strings.Contains(adapter.artifacts[0], "result.csv:a,b") {
		t.Fatalf("其他适配器必须复用附件挂载、产物基线与交付: writes=%d downloads=%v artifacts=%v", writes, adapter.downloads, adapter.artifacts)
	}
}

func (*independentAdapter) OAuthProvider() vaults.EnvOAuthProviderSnapshot {
	return vaults.EnvOAuthProviderSnapshot{}
}
func (*independentAdapter) OAuthUserID(context.Context, string) (string, error) {
	return "", errors.New("测试渠道未启用个人授权")
}
