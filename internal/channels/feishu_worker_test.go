package channels

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type fakeRuntimeStore struct {
	mu       sync.Mutex
	record   db.ChannelRecord
	bindings map[string]string
	deleted  []string
	env      string
}

func (f *fakeRuntimeStore) GetChannelRecord(context.Context, string, string, string, string) (db.ChannelRecord, error) {
	return f.record, nil
}

func (f *fakeRuntimeStore) GetChannelSessionBinding(_ context.Context, _ string, scopeKey string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.bindings[scopeKey]
	if !ok {
		return "", db.ErrNotFound
	}
	return value, nil
}

func (f *fakeRuntimeStore) SetChannelSessionBinding(_ context.Context, _ string, scopeKey, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bindings[scopeKey] = sessionID
	return nil
}

func (f *fakeRuntimeStore) DeleteChannelSessionBinding(_ context.Context, _ string, scopeKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, scopeKey)
	delete(f.bindings, scopeKey)
	return nil
}

func (f *fakeRuntimeStore) GetChannelAgentEnvironment(context.Context, string, string, string) (string, error) {
	return f.env, nil
}

func (f *fakeRuntimeStore) ListChannelPairingRecords(context.Context, string) ([]db.ChannelPairingRecord, error) {
	return nil, nil
}

func (f *fakeRuntimeStore) CountChannelPairingRecords(context.Context, string) (int, error) {
	return 0, nil
}

func (f *fakeRuntimeStore) InsertChannelPairingRecord(context.Context, string, db.ChannelPairingInput) error {
	return nil
}

func (f *fakeRuntimeStore) EnsureChannelWorkspaceCredential(context.Context, platform.CreateConsoleAPIKeyInput, *secrets.Service) (string, error) {
	return "key", nil
}

type fakeChannel struct {
	mu        sync.Mutex
	sends     []string
	inputs    []*channeltypes.SendInput
	downloads map[string][]byte
	startErr  error
}

func (f *fakeChannel) Send(_ context.Context, input *channeltypes.SendInput) (*channeltypes.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, input.Text+input.Markdown)
	f.inputs = append(f.inputs, input)
	return &channeltypes.SendResult{MessageID: "om_1"}, nil
}

func (f *fakeChannel) OnMessage(func(context.Context, *channeltypes.NormalizedMessage) error) {}
func (f *fakeChannel) OnReaction(func(context.Context, *channeltypes.ReactionEvent) error)    { return }
func (f *fakeChannel) OnComment(func(context.Context, *channeltypes.CommentEvent) error)      { return }
func (f *fakeChannel) OnBotAdded(func(context.Context, *channeltypes.BotAddedEvent) error)    { return }
func (f *fakeChannel) OnCardAction(func(context.Context, *channeltypes.CardActionEvent) error) {
}
func (f *fakeChannel) OnReject(func(context.Context, *channeltypes.RejectEvent) error) {}
func (f *fakeChannel) DownloadFile(_ context.Context, fileKey, _ string) ([]byte, error) {
	return f.downloads[fileKey], nil
}
func (f *fakeChannel) OnReady(func())                         {}
func (f *fakeChannel) OnError(func(error))                    {}
func (f *fakeChannel) OnReconnecting(func())                  {}
func (f *fakeChannel) OnReconnected(func())                   {}
func (f *fakeChannel) OnDisconnected(func())                  {}
func (f *fakeChannel) Start(context.Context) error            { return f.startErr }
func (f *fakeChannel) UpdatePolicy(channeltypes.PolicyConfig) {}
func (f *fakeChannel) GetPolicy() channeltypes.PolicyConfig {
	return channeltypes.PolicyConfig{}
}
func (f *fakeChannel) GetBotIdentity(context.Context) *channeltypes.BotIdentity {
	return &channeltypes.BotIdentity{}
}
func (f *fakeChannel) Stop(context.Context) error { return nil }

func (f *fakeChannel) Stream(context.Context, *channeltypes.SendInput) (channeltypes.StreamController, error) {
	return fakeStream{}, nil
}

func (f *fakeChannel) text() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.sends...)
}

func (f *fakeChannel) sentInputs() []*channeltypes.SendInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*channeltypes.SendInput{}, f.inputs...)
}

type fakeStream struct{}

func (fakeStream) Append(context.Context, string) error     { return nil }
func (fakeStream) UpdateCard(context.Context, string) error { return nil }
func (fakeStream) Flush(context.Context) error              { return nil }
func (fakeStream) Close(context.Context) error              { return nil }

func testWorker(t *testing.T, store *fakeRuntimeStore, baseURL string) (*channelWorker, *fakeChannel) {
	t.Helper()
	if store.bindings == nil {
		store.bindings = map[string]string{}
	}
	store.record = db.ChannelRecord{
		ExternalID:       "chnl_1",
		OrganizationUUID: "org_1",
		WorkspaceUUID:    "ws_1",
		AgentExternalID:  "agent_1",
		AppID:            "app_1",
		Enabled:          true,
		BindingStatus:    "bound",
		Mode:             "open",
	}
	store.env = "env_1"
	fake := &fakeChannel{}
	worker := &channelWorker{
		supervisor: newRuntimeSupervisor(store, nil, newOMAClient(baseURL), nil),
		record:     store.record,
		channel:    fake,
		replies:    make(map[string][]*channelReplyTask),
		asked:      make(map[string]struct{}),
		artifacts:  make(map[string]struct{}),
		ctx:        context.Background(),
	}
	return worker, fake
}

func omaTestServer(t *testing.T, sendDelay time.Duration) (*httptest.Server, *atomic.Int64, *[]string) {
	t.Helper()
	var creates atomic.Int64
	var mu sync.Mutex
	bodies := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			creates.Add(1)
			_, _ = w.Write([]byte(`{"id":"sesn_1"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			_, _ = w.Write([]byte(`{"data":[{"id":"evt_user_1","type":"user.message"},{"id":"evt_1","type":"session.status_idle","stop_reason":{"type":"end_turn"}}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			mu.Lock()
			bodies = append(bodies, string(body))
			mu.Unlock()
			time.Sleep(sendDelay)
			_, _ = w.Write([]byte(`{"data":[{"id":"evt_user_1","type":"user.message"}]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return server, &creates, &bodies
}

func TestHandleMessageReturnsBeforeReplyFinishes(t *testing.T) {
	server, _, bodies := omaTestServer(t, 400*time.Millisecond)
	store := &fakeRuntimeStore{}
	worker, _ := testWorker(t, store, server.URL)
	msg := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", ChatType: "group", UserID: "ou_1", Content: "你好"}
	started := time.Now()
	if err := worker.handleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handleMessage: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("handleMessage 阻塞了 %v，应该快速返回", elapsed)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(*bodies) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("回复任务没有继续执行")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestThreadScopesUseSeparateSessions(t *testing.T) {
	server, creates, _ := omaTestServer(t, 0)
	store := &fakeRuntimeStore{}
	worker, _ := testWorker(t, store, server.URL)
	first := &channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1", Content: "a", RawEvent: map[string]any{"thread_id": "omt_1"}}
	second := &channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1", Content: "b", RawEvent: map[string]any{"thread_id": "omt_2"}}
	if _, err := worker.sessionFor(context.Background(), first, channelScopeKey(first)); err != nil {
		t.Fatalf("sessionFor first: %v", err)
	}
	if _, err := worker.sessionFor(context.Background(), second, channelScopeKey(second)); err != nil {
		t.Fatalf("sessionFor second: %v", err)
	}
	if creates.Load() != 2 {
		t.Fatalf("两个话题应各自建会话，实际 %d", creates.Load())
	}
	if _, ok := store.bindings["thread:oc_1:omt_1"]; !ok {
		t.Fatalf("缺少话题 1 绑定: %v", store.bindings)
	}
	if _, ok := store.bindings["thread:oc_1:omt_2"]; !ok {
		t.Fatalf("缺少话题 2 绑定: %v", store.bindings)
	}
}

func TestNewCancelsReplyAndResetsBinding(t *testing.T) {
	server, _, bodies := omaTestServer(t, 0)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, fake := testWorker(t, store, server.URL)
	task := worker.startReplyTask(context.Background(), "chat:oc_1", "sesn_1")
	worker.runCommand(context.Background(), "chat:oc_1", &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1"}, "/new")
	if task.ctx.Err() == nil {
		t.Fatal("/new 应取消旧回复任务")
	}
	if len(store.deleted) != 1 || store.deleted[0] != "chat:oc_1" {
		t.Fatalf("应删除会话绑定，实际 %v", store.deleted)
	}
	if len(fake.text()) == 0 {
		t.Fatal("/new 应有提示回复")
	}
	if !strings.Contains(strings.Join(*bodies, ""), "user.interrupt") {
		t.Fatalf("/new 应中断旧会话，实际 %v", *bodies)
	}
}

func TestStopInterruptsMappedSession(t *testing.T) {
	server, _, bodies := omaTestServer(t, 0)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, server.URL)
	worker.runCommand(context.Background(), "chat:oc_1", &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1"}, "/stop")
	if !strings.Contains(strings.Join(*bodies, ""), "user.interrupt") {
		t.Fatalf("/stop 应发送 user.interrupt，实际 %v", *bodies)
	}
	if len(store.deleted) != 0 {
		t.Fatal("/stop 不应删除会话绑定")
	}
}

func TestAnsweredToolsDetectsRepeats(t *testing.T) {
	events := []omaEvent{
		{ID: "evt_1", Type: "user.tool_confirmation", ToolUseID: "tool_1"},
		{ID: "evt_2", Type: "user.custom_tool_result", CustomToolUseID: "tool_2"},
	}
	answered := answeredToolIDs(events)
	if _, ok := answered["tool_1"]; !ok {
		t.Fatal("应识别已审批的工具")
	}
	if _, ok := answered["tool_2"]; !ok {
		t.Fatal("应识别已回答的问卷")
	}
	if _, ok := answered["tool_3"]; ok {
		t.Fatal("未处理的工具不应被标记")
	}
}

func TestBuildAnswersSupportsMultiSelectAndCustom(t *testing.T) {
	answers := buildAnswers(channelActionValue{}, map[string]any{
		"q1":        []any{"main", "dev"},
		"q1_custom": "",
		"q2":        []any{},
		"q2_custom": "自定义",
	})
	encoded, err := json.Marshal(answers)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"id":"q1"`) || !strings.Contains(text, `"main"`) || !strings.Contains(text, `"custom":"自定义"`) {
		t.Fatalf("问卷答案不完整: %s", text)
	}
}

func TestFeishuWSClientRegistersEventDispatcher(t *testing.T) {
	client := newFeishuWSClient("cli_test", "secret")
	if client.EventHandler() == nil {
		t.Fatal("飞书长连接必须配置事件分发器，否则 OnMessage / OnCardAction 注册的处理器永远不会被调用")
	}
}

func TestFailedStartReleasesWorkerForRetry(t *testing.T) {
	store := &fakeRuntimeStore{}
	supervisor := newRuntimeSupervisor(store, nil, nil, nil)
	worker := &channelWorker{
		supervisor: supervisor,
		record:     db.ChannelRecord{ExternalID: "chnl_1"},
		channel:    &fakeChannel{startErr: errors.New("websocket dial failed")},
		replies:    make(map[string][]*channelReplyTask),
		asked:      make(map[string]struct{}),
	}
	supervisor.workers["chnl_1"] = worker
	worker.start()
	deadline := time.Now().Add(2 * time.Second)
	for supervisor.running("chnl_1") {
		if time.Now().After(deadline) {
			t.Fatal("长连接启动失败后应释放 worker，让对账重新拉起")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestScopeMatchesChat(t *testing.T) {
	if !scopeMatchesChat("chat:oc_1", "oc_1") {
		t.Fatal("群聊作用域应匹配")
	}
	if !scopeMatchesChat("thread:oc_1:omt_1", "oc_1") {
		t.Fatal("话题作用域应匹配")
	}
	if scopeMatchesChat("thread:oc_2:omt_1", "oc_1") {
		t.Fatal("其它群聊的话题不应匹配")
	}
}
