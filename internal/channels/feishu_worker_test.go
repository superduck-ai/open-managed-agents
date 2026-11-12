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
	pairings []db.ChannelPairingRecord
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

func (f *fakeRuntimeStore) binding(scopeKey string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bindings[scopeKey]
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
	return f.pairings, nil
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
	appends   []string
	downloads map[string][]byte
	startErr  error
	sendErr   error
	attempts  int
}

func (f *fakeChannel) Send(_ context.Context, input *channeltypes.SendInput) (*channeltypes.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	f.sends = append(f.sends, input.Text+input.Markdown)
	f.inputs = append(f.inputs, input)
	return &channeltypes.SendResult{MessageID: "om_1"}, nil
}

func (f *fakeChannel) setSendErr(err error) {
	f.mu.Lock()
	f.sendErr = err
	f.mu.Unlock()
}

func (f *fakeChannel) sendAttempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

func (f *fakeChannel) cardInputs() []*channeltypes.SendInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	cards := make([]*channeltypes.SendInput, 0, len(f.inputs))
	for _, input := range f.inputs {
		if input.Card != "" {
			cards = append(cards, input)
		}
	}
	return cards
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
	return fakeStream{channel: f}, nil
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

type fakeStream struct {
	channel *fakeChannel
}

func (s fakeStream) Append(_ context.Context, text string) error {
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	s.channel.appends = append(s.channel.appends, text)
	return nil
}

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
		scopes:     make(map[string]*scopeQueue),
		replies:    make(map[string][]*channelReplyTask),
		asked:      make(map[string]*askedCard),
		artifacts:  make(map[string]struct{}),
		tails:      make(map[string]string),
		ctx:        context.Background(),
	}
	return worker, fake
}

type omaRecorder struct {
	mu     sync.Mutex
	bodies []string
}

func (r *omaRecorder) record(body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bodies = append(r.bodies, body)
}

func (r *omaRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.bodies...)
}

func omaTestServer(t *testing.T, sendDelay time.Duration) (*httptest.Server, *atomic.Int64, *omaRecorder) {
	t.Helper()
	var creates atomic.Int64
	var sent atomic.Int64
	recorder := &omaRecorder{}
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
			recorder.record(string(body))
			if sent.Add(1) == 1 {
				time.Sleep(sendDelay)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"evt_user_1","type":"user.message"}]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return server, &creates, recorder
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
		if len(bodies.snapshot()) > 0 {
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
	if !strings.Contains(strings.Join(bodies.snapshot(), ""), "user.interrupt") {
		t.Fatalf("/new 应中断旧会话，实际 %v", bodies.snapshot())
	}
}

func TestStopInterruptsMappedSession(t *testing.T) {
	server, _, bodies := omaTestServer(t, 0)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, server.URL)
	worker.runCommand(context.Background(), "chat:oc_1", &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1"}, "/stop")
	if !strings.Contains(strings.Join(bodies.snapshot(), ""), "user.interrupt") {
		t.Fatalf("/stop 应发送 user.interrupt，实际 %v", bodies.snapshot())
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
		asked:      make(map[string]*askedCard),
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

func sentTexts(bodies *omaRecorder) []string {
	texts := make([]string, 0)
	for _, body := range bodies.snapshot() {
		if caret := strings.Index(body, `"text":"`); caret >= 0 {
			rest := body[caret+len(`"text":"`):]
			if end := strings.Index(rest, `"`); end >= 0 {
				texts = append(texts, rest[:end])
			}
		}
	}
	return texts
}

func TestScopeSerializesInboundMessages(t *testing.T) {
	server, _, bodies := omaTestServer(t, 300*time.Millisecond)
	store := &fakeRuntimeStore{}
	worker, _ := testWorker(t, store, server.URL)
	first := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", ChatType: "group", UserID: "ou_1", Content: "先发的"}
	second := &channeltypes.NormalizedMessage{MessageID: "om_2", ChatID: "oc_1", ChatType: "group", UserID: "ou_1", Content: "后发的"}
	if err := worker.handleMessage(context.Background(), first); err != nil {
		t.Fatalf("handleMessage first: %v", err)
	}
	if err := worker.handleMessage(context.Background(), second); err != nil {
		t.Fatalf("handleMessage second: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(sentTexts(bodies)) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("两条消息都应提交，实际 %v", sentTexts(bodies))
		}
		time.Sleep(10 * time.Millisecond)
	}
	texts := sentTexts(bodies)
	if texts[0] != "先发的" || texts[1] != "后发的" {
		t.Fatalf("同一会话必须按到达顺序提交，实际 %v", texts)
	}
}

func TestNewRunsBeforeFollowingMessage(t *testing.T) {
	server, creates, _ := omaTestServer(t, 0)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, server.URL)
	reset := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", ChatType: "group", UserID: "ou_1", Content: "/new"}
	next := &channeltypes.NormalizedMessage{MessageID: "om_2", ChatID: "oc_1", ChatType: "group", UserID: "ou_1", Content: "新会话第一句"}
	if err := worker.handleMessage(context.Background(), reset); err != nil {
		t.Fatalf("handleMessage reset: %v", err)
	}
	if err := worker.handleMessage(context.Background(), next); err != nil {
		t.Fatalf("handleMessage next: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for creates.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("/new 之后的消息必须新建会话，实际创建 %d 次", creates.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "chat:oc_1" {
		t.Fatalf("/new 应先删除旧绑定，实际 %v", store.deleted)
	}
	if value := store.binding("chat:oc_1"); value != "sesn_1" {
		t.Fatalf("新消息应重新绑定会话，实际 %q", value)
	}
}

func TestCardActionRequiresCurrentPairing(t *testing.T) {
	server, _, bodies := omaTestServer(t, 0)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, fake := testWorker(t, store, server.URL)
	worker.record.Mode = "pairing"
	value := channelActionValue{
		Kind:      "confirm",
		ChannelID: "chnl_1",
		ScopeKey:  "chat:oc_1",
		SessionID: "sesn_1",
		ToolUseID: "tool_1",
		Result:    "allow",
		OpenID:    "ou_1",
		ChatType:  "group",
	}
	action := &channeltypes.CardActionEvent{ChatID: "oc_1", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: map[string]any{
		"kind":        value.Kind,
		"channel_id":  value.ChannelID,
		"scope_key":   value.ScopeKey,
		"session_id":  value.SessionID,
		"tool_use_id": value.ToolUseID,
		"result":      value.Result,
		"open_id":     value.OpenID,
		"chat_type":   value.ChatType,
	}}}
	if err := worker.handleCardAction(context.Background(), action); err == nil {
		t.Fatal("配对撤销后旧卡片必须被拒绝")
	}
	if len(bodies.snapshot()) != 0 {
		t.Fatalf("被拒绝的卡片不应提交任何事件，实际 %v", bodies.snapshot())
	}
	if !strings.Contains(strings.Join(fake.text(), ""), "未获授权") {
		t.Fatalf("应提示授权失效，实际 %v", fake.text())
	}
}

func TestCardActionResumesReplyListener(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			_, _ = w.Write([]byte(`{"data":[{"id":"evt_1","type":"agent.custom_tool_use","custom_tool_use_id":"tool_1"}]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, server.URL)
	action := &channeltypes.CardActionEvent{ChatID: "oc_1", MessageID: "om_card", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: map[string]any{
		"kind":        "answer",
		"channel_id":  "chnl_1",
		"scope_key":   "chat:oc_1",
		"session_id":  "sesn_1",
		"tool_use_id": "tool_1",
		"open_id":     "ou_1",
		"chat_type":   "group",
	}}}
	if err := worker.handleCardAction(context.Background(), action); err != nil {
		t.Fatalf("handleCardAction: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		worker.mu.Lock()
		tasks := append([]*channelReplyTask{}, worker.replies["chat:oc_1"]...)
		worker.mu.Unlock()
		if len(tasks) == 1 && tasks[0].listener {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("重启后卡片操作应重建回复监听，实际 %d 个任务", len(tasks))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAskCardRetriesAfterSendFailure(t *testing.T) {
	server, _, _ := omaTestServer(t, 0)
	store := &fakeRuntimeStore{}
	worker, fake := testWorker(t, store, server.URL)
	fake.sendErr = errors.New("feishu unavailable")
	task := worker.startReplyTask(context.Background(), "chat:oc_1", "sesn_1")
	event := omaEvent{ID: "evt_ask", Type: "agent.tool_use", Name: "Bash", Permission: "ask"}
	if err := worker.askIfNeeded(context.Background(), &channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1"}, task, event, nil); err == nil {
		t.Fatal("发送失败应返回错误")
	}
	key := "confirm:sesn_1:evt_ask"
	if err := worker.askIfNeeded(context.Background(), &channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1"}, task, event, nil); err != nil {
		t.Fatalf("退避期间应静默跳过: %v", err)
	}
	if fake.sendAttempts() != 1 {
		t.Fatalf("退避期间只应尝试一次，实际 %d", fake.sendAttempts())
	}
	worker.mu.Lock()
	worker.asked[key].nextTry = time.Time{}
	worker.mu.Unlock()
	fake.setSendErr(nil)
	if err := worker.askIfNeeded(context.Background(), &channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1"}, task, event, nil); err != nil {
		t.Fatalf("恢复后应重发成功: %v", err)
	}
	if len(fake.cardInputs()) != 1 || fake.sendAttempts() != 2 {
		t.Fatalf("发送失败后应重试，实际发送 %d 次、卡片 %d 张", fake.sendAttempts(), len(fake.cardInputs()))
	}
	worker.mu.Lock()
	state := worker.asked[key]
	worker.mu.Unlock()
	if state == nil || !state.sent {
		t.Fatalf("成功后应标记已发送: %+v", state)
	}
}

func TestBaselineFailureBlocksArtifacts(t *testing.T) {
	var listing atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/resources"):
			listing.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			_, _ = w.Write([]byte(`{"data":[{"id":"evt_user_1","type":"user.message"},{"id":"evt_1","type":"session.status_idle","stop_reason":{"type":"end_turn"}}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			_, _ = w.Write([]byte(`{"data":[{"id":"evt_user_1","type":"user.message"}]}`))
		default:
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
		}
	}))
	t.Cleanup(server.Close)
	store := &fakeRuntimeStore{}
	worker, fake := testWorker(t, store, server.URL)
	msg := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", ChatType: "group", UserID: "ou_1", Content: "跑一下"}
	if err := worker.handleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handleMessage: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if strings.Contains(strings.Join(fake.text(), ""), "本轮产物暂不回传") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("基线失败应明确提示，实际 %v", fake.text())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if listing.Load() == 0 {
		t.Fatal("应至少尝试一次读取产物基线")
	}
	for _, input := range fake.sentInputs() {
		if input.FileKey != "" || input.ImageKey != "" {
			t.Fatalf("基线失败时不应回传历史产物: %+v", input)
		}
	}
}

func TestStopPreemptsRunningReply(t *testing.T) {
	server, _, bodies := omaTestServer(t, 3*time.Second)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, fake := testWorker(t, store, server.URL)
	first := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", ChatType: "group", UserID: "ou_1", Content: "长任务"}
	if err := worker.handleMessage(context.Background(), first); err != nil {
		t.Fatalf("handleMessage: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(bodies.snapshot()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("首条消息应已提交")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop := &channeltypes.NormalizedMessage{MessageID: "om_2", ChatID: "oc_1", ChatType: "group", UserID: "ou_1", Content: "/stop"}
	if err := worker.handleMessage(context.Background(), stop); err != nil {
		t.Fatalf("handleMessage stop: %v", err)
	}
	deadline = time.Now().Add(time.Second)
	for {
		if strings.Contains(strings.Join(fake.text(), ""), "已请求停止") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("/stop 应立即打断进行中的回复，实际 %v", fake.text())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCardActionSkipsListenerWhenTurnFinished(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			_, _ = w.Write([]byte(`{"data":[{"id":"evt_1","type":"session.status_idle","stop_reason":{"type":"end_turn"}}]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, server.URL)
	action := &channeltypes.CardActionEvent{ChatID: "oc_1", MessageID: "om_card", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: map[string]any{
		"kind":        "confirm",
		"channel_id":  "chnl_1",
		"scope_key":   "chat:oc_1",
		"session_id":  "sesn_1",
		"tool_use_id": "tool_1",
		"result":      "allow",
		"open_id":     "ou_1",
		"chat_type":   "group",
	}}}
	if err := worker.handleCardAction(context.Background(), action); err != nil {
		t.Fatalf("handleCardAction: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	worker.mu.Lock()
	remaining := len(worker.replies["chat:oc_1"])
	worker.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("轮次已结束时不应留下监听任务，实际 %d 个", remaining)
	}
}
