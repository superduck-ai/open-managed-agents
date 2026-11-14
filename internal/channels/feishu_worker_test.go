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
	"uuid"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type fakeRuntimeStore struct {
	mu              sync.Mutex
	record          db.ChannelRecord
	bindings        map[string]string
	deleted         []string
	pairings        []db.ChannelPairingRecord
	deliveries      map[string]db.ChannelReplyDelivery
	inputs          map[string]db.ChannelInput
	inputKeys       map[string]string
	receiveInputErr error
	inputOrder      []string
	env             string
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
	identity       *channeltypes.BotIdentity
	mu             sync.Mutex
	sends          []string
	inputs         []*channeltypes.SendInput
	appends        []string
	downloads      map[string][]byte
	startErr       error
	updateFailures int
	updateAttempts int
	streams        []*channeltypes.SendInput
}

func (f *fakeChannel) Send(_ context.Context, input *channeltypes.SendInput) (*channeltypes.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if input.Card == "" {
		f.sends = append(f.sends, input.Text+input.Markdown)
	}
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
	if f.identity != nil {
		return f.identity
	}
	return &channeltypes.BotIdentity{}
}
func (f *fakeChannel) Stop(context.Context) error { return nil }

func (f *fakeChannel) Stream(_ context.Context, input *channeltypes.SendInput) (channeltypes.StreamController, error) {
	f.mu.Lock()
	f.streams = append(f.streams, input)
	f.mu.Unlock()
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

func (s fakeStream) UpdateCard(_ context.Context, card string) error {
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	s.channel.updateAttempts++
	if s.channel.updateFailures > 0 {
		s.channel.updateFailures--
		return errors.New("update failed")
	}
	s.channel.appends = append(s.channel.appends, card)
	return nil
}
func (fakeStream) Flush(context.Context) error { return nil }
func (fakeStream) Close(context.Context) error { return nil }
func (s fakeStream) update(ctx context.Context, reply *channelReply) error {
	return s.UpdateCard(ctx, reply.render())
}
func (fakeStream) receipt() string             { return "om_1" }
func (fakeStream) close(context.Context) error { return nil }

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
		ChannelType:      "feishu",
		Enabled:          true,
		BindingStatus:    "bound",
		Mode:             "open",
	}
	store.env = "env_1"
	fake := &fakeChannel{}
	worker := &channelWorker{
		checkedInputs: make(map[string]string),
		deliveryOwner: uuid.NewV4().String(),
		supervisor:    newRuntimeSupervisor(store, nil, newOMAClient(baseURL), nil),
		record:        store.record,
		scopes:        make(map[string]*scopeQueue),
		replies:       make(map[string][]*channelReplyTask),
		ctx:           t.Context(),
	}
	worker.supervisor.fileMaxBytes = 500 << 20
	cardServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal") {
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"token","expire":7200}`))
			return
		}
		if r.Method != http.MethodPatch {
			t.Errorf("未预期的卡片请求 %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if err := (fakeStream{channel: fake}).UpdateCard(r.Context(), body.Content); err != nil {
			_, _ = w.Write([]byte(`{"code":999,"msg":"update failed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	t.Cleanup(cardServer.Close)
	worker.adapter = &feishuChannel{worker: worker, client: lark.NewClient("cli_test", "secret", lark.WithOpenBaseUrl(cardServer.URL)), channel: fake, incoming: newFeishuInputPipelines(channeltypes.DefaultBatchConfig())}
	t.Cleanup(worker.adapter.(*feishuChannel).incoming.Dispose)
	t.Cleanup(func() { worker.stopAllReplies() })
	return worker, fake
}

func TestDispatchMessageBatchesWithinThreadOnly(t *testing.T) {
	server, _, recorder := omaTestServer(t, 0)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	tickets := []*scopeTicket{worker.enqueueScope("thread:oc_1:thread_a"), worker.enqueueScope("thread:oc_1:thread_b")}
	for _, input := range []struct{ thread, text string }{{"thread_a", "甲一"}, {"thread_b", "乙"}, {"thread_a", "甲二"}} {
		msg := &channeltypes.NormalizedMessage{
			ChatID: "oc_1", UserID: "ou_1", MessageID: input.text, Content: input.text,
			RawEvent: map[string]any{"thread_id": input.thread},
		}
		if err := worker.dispatchMessage(t.Context(), msg); err != nil {
			t.Fatal(err)
		}
	}
	worker.adapter.(*feishuChannel).incoming.FlushAll(t.Context())
	for _, ticket := range tickets {
		worker.releaseScope(ticket)
	}
	deadline := time.After(3 * time.Second)
	for len(recorder.snapshot()) < 2 {
		select {
		case <-deadline:
			t.Fatalf("两个话题必须分别提交: %v", recorder.snapshot())
		case <-time.After(10 * time.Millisecond):
		}
	}
	for _, body := range recorder.snapshot() {
		if strings.Contains(body, "甲") && strings.Contains(body, "乙") {
			t.Fatalf("不同话题不能合批: %s", body)
		}
	}
	joined := strings.Join(recorder.snapshot(), "")
	if !strings.Contains(joined, `甲一\n\n甲二`) {
		t.Fatalf("同话题连续消息应保留合批: %s", joined)
	}
	if !strings.Contains(joined, `"channel_message_ids":["甲一","甲二"]`) || !strings.Contains(joined, `"channel_message_ids":["乙"]`) {
		t.Fatalf("每轮必须保留全部来源消息: %s", joined)
	}
	worker.stopAllReplies()
}

func TestDispatchMessageSeparatesActorsWithinThread(t *testing.T) {
	server, _, recorder := omaTestServer(t, 0)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	for _, actor := range []string{"actor_a", "actor_b"} {
		msg := &channeltypes.NormalizedMessage{
			ChatID: "oc_1", UserID: actor, MessageID: actor, Content: actor,
			RawEvent: map[string]any{"thread_id": "same_thread"},
		}
		if err := worker.dispatchMessage(t.Context(), msg); err != nil {
			t.Fatal(err)
		}
	}
	worker.adapter.(*feishuChannel).incoming.FlushAll(t.Context())
	deadline := time.After(3 * time.Second)
	for len(recorder.snapshot()) < 2 {
		select {
		case <-deadline:
			t.Fatalf("不同发送人必须分别提交: %v", recorder.snapshot())
		case <-time.After(10 * time.Millisecond):
		}
	}
	for _, body := range recorder.snapshot() {
		if strings.Contains(body, "actor_a") && strings.Contains(body, "actor_b") {
			t.Fatalf("不能把其他发送人的正文归入本轮: %s", body)
		}
	}
	worker.stopAllReplies()
}

func TestDispatchCommandFlushesSameActorPendingText(t *testing.T) {
	server, _, recorder := omaTestServer(t, 0)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	adapter := worker.adapter.(*feishuChannel)
	adapter.incoming.Dispose()
	config := channeltypes.DefaultBatchConfig()
	config.DelayMs = time.Minute
	adapter.incoming = newFeishuInputPipelines(config)
	for _, text := range []string{"命令前的文本", "/help"} {
		if err := worker.dispatchMessage(t.Context(), &channeltypes.NormalizedMessage{
			ChatID: "oc_1", ChatType: "p2p", UserID: "actor", MessageID: text, Content: text,
		}); err != nil {
			t.Fatal(err)
		}
	}
	waitForScope(t, worker, "chat:oc_1")
	if len(recorder.snapshot()) != 1 || !strings.Contains(recorder.snapshot()[0], "命令前的文本") {
		t.Fatalf("命令必须先交接同一发送人的缓冲文本: %v", recorder.snapshot())
	}
	worker.stopAllReplies()
}

func TestScopeTurnsKeepOrderAcrossCancellation(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
	first := worker.enqueueScope("thread:a")
	second := worker.enqueueScope("thread:a")
	third := worker.enqueueScope("thread:a")
	other := worker.enqueueScope("thread:b")
	select {
	case <-second.ready:
		t.Fatal("同作用域后一轮不能提前开始")
	default:
	}
	select {
	case <-other.ready:
	default:
		t.Fatal("独立作用域不应互相阻塞")
	}
	worker.releaseScope(first)
	if worker.dropScope(second) {
		t.Fatal("已经获得执行权的轮次不应当作排队取消")
	}
	select {
	case <-third.ready:
		t.Fatal("取消和交接重叠不能提前释放当前执行权")
	default:
	}
	worker.releaseScope(second)
	select {
	case <-third.ready:
	default:
		t.Fatal("当前轮完成后必须唤醒下一轮")
	}
	worker.releaseScope(third)
	worker.releaseScope(other)
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
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle","agent":{"id":"agent_1"},"environment_id":"env_1"}`))
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
	if err := worker.handleMessage(context.Background(), feishuMessage(msg), channelScopeKey(feishuMessage(msg))); err != nil {
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
	if _, err := worker.sessionFor(context.Background(), feishuMessage(first), channelScopeKey(feishuMessage(first))); err != nil {
		t.Fatalf("sessionFor first: %v", err)
	}
	if _, err := worker.sessionFor(context.Background(), feishuMessage(second), channelScopeKey(feishuMessage(second))); err != nil {
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
	worker.runCommand(context.Background(), "chat:oc_1", feishuMessage(&channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1"}), "/new")
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
	worker.runCommand(context.Background(), "chat:oc_1", feishuMessage(&channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1"}), "/stop")
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
	answers, err := buildAnswers([]channelQuestion{{ID: "q1", Title: "分支", MultiSelect: true, Options: []channelQuestionOption{{Label: "main"}, {Label: "dev"}}}, {ID: "q2", Title: "自定义"}}, map[string]any{
		"q1":        []any{"main", "dev"},
		"q1_custom": "",
		"q2":        []any{},
		"q2_custom": "自定义",
	})
	if err != nil {
		t.Fatal(err)
	}
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
		adapter:    &feishuChannel{channel: &fakeChannel{startErr: errors.New("websocket dial failed")}},
		replies:    make(map[string][]*channelReplyTask),
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
	if err := worker.handleMessage(context.Background(), feishuMessage(first), "chat:"+first.ChatID); err != nil {
		t.Fatalf("handleMessage first: %v", err)
	}
	if err := worker.handleMessage(context.Background(), feishuMessage(second), "chat:"+second.ChatID); err != nil {
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
	if err := worker.handleMessage(context.Background(), feishuMessage(reset), "chat:"+reset.ChatID); err != nil {
		t.Fatalf("handleMessage reset: %v", err)
	}
	if err := worker.handleMessage(context.Background(), feishuMessage(next), "chat:"+next.ChatID); err != nil {
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
	seedCardActionReceipt(worker, "card_input")
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
	action := &channeltypes.CardActionEvent{ChatID: "oc_1", MessageID: "om_card", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: map[string]any{
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

func TestCardActionResumesOriginalReplyDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			_, _ = w.Write([]byte(`{"data":[{"id":"card_input","type":"user.message"},{"id":"tool_1","type":"agent.custom_tool_use","name":"AskUserQuestion","input":{"questions":[{"id":"q","question":"颜色","options":[{"label":"红"}]}]}}]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, server.URL)
	seedCardActionReceipt(worker, "card_input")
	worker.supervisor.subscribeEvents = testEventSubscriber(t, func() []omaEvent { return []omaEvent{{ID: "card_input", Type: "user.message"}} })
	action := &channeltypes.CardActionEvent{ChatID: "oc_1", MessageID: "om_card", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{FormValue: map[string]any{"q": "红"}, Value: map[string]any{
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
		if len(tasks) == 1 && !tasks[0].listener && tasks[0].inputEventID == "card_input" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("重启后卡片操作应重建回复监听，实际 %d 个任务", len(tasks))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestInlineApprovalRetriesUpdatingSameCard(t *testing.T) {
	server, _, _ := omaTestServer(t, 0)
	worker, fake := testWorker(t, &fakeRuntimeStore{}, server.URL)
	fake.updateFailures = 1
	task := worker.startReplyTask(t.Context(), "chat:oc_1", "sesn_1")
	defer worker.finishReply(task)
	runner := newReplyRunner(worker, task, feishuMessage(&channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1"}))
	runner.stream = fakeStream{channel: fake}
	runner.reply.consume(worker.record, omaEvent{ID: "tool_1", Type: "agent.tool_use", Permission: "ask", Name: "bash"})
	if err := runner.update(t.Context()); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.updateAttempts != 2 || len(fake.inputs) != 0 || len(fake.appends) != 1 || !strings.Contains(fake.appends[0], "等待批准") {
		t.Fatalf("审批更新应重试同一卡片，不发送另一张卡片: attempts=%d sends=%d updates=%v", fake.updateAttempts, len(fake.inputs), fake.appends)
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
	if err := worker.handleMessage(context.Background(), feishuMessage(msg), "chat:"+msg.ChatID); err != nil {
		t.Fatalf("handleMessage: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if strings.Contains(strings.Join(fake.text(), ""), "本轮尚未提交") {
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
	if err := worker.handleMessage(context.Background(), feishuMessage(first), "chat:"+first.ChatID); err != nil {
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
	if err := worker.handleMessage(context.Background(), feishuMessage(stop), "chat:"+stop.ChatID); err != nil {
		t.Fatalf("handleMessage stop: %v", err)
	}
	deadline = time.Now().Add(time.Second)
	for {
		if strings.Contains(strings.Join(fake.text(), ""), "已停止") {
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
			_, _ = w.Write([]byte(`{"data":[{"id":"card_input","type":"user.message"},{"id":"evt_1","type":"session.status_idle","stop_reason":{"type":"end_turn"}}]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, server.URL)
	seedCardActionReceipt(worker, "card_input")
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
	if err := worker.handleCardAction(context.Background(), action); err == nil {
		t.Fatal("已经结束的轮次必须拒绝旧卡操作")
	}
	worker.mu.Lock()
	remaining := len(worker.replies["chat:oc_1"])
	worker.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("轮次已结束时不应留下监听任务，实际 %d 个", remaining)
	}
}

func waitForScope(t *testing.T, worker *channelWorker, scope string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ticket := worker.enqueueScope(scope)
	if err := worker.waitScope(ctx, ticket); err != nil {
		t.Fatal(err)
	}
	worker.releaseScope(ticket)
}

func TestUnapprovedStopDoesNotCancelActiveReply(t *testing.T) {
	server, _, bodies := omaTestServer(t, 0)
	worker, _ := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, server.URL)
	worker.record.Mode = "pairing"
	task := worker.startReplyTask(t.Context(), "chat:oc_1", "sesn_1")
	defer worker.finishReply(task)
	msg := &channeltypes.NormalizedMessage{ChatID: "oc_1", ChatType: "group", UserID: "ou_unapproved", MessageID: "om_stop", Content: "/stop"}
	if err := worker.handleMessage(t.Context(), feishuMessage(msg), "chat:oc_1"); err != nil {
		t.Fatal(err)
	}
	if task.ctx.Err() != nil || len(bodies.snapshot()) != 0 {
		t.Fatalf("未授权命令不得取消已运行回复或发送中断: err=%v events=%v", task.ctx.Err(), bodies.snapshot())
	}
}

func TestSendWithoutEventIDsRecoversAcceptedInput(t *testing.T) {
	var submitted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			submitted.Store(true)
			_, _ = w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/events"):
			if submitted.Load() {
				_, _ = w.Write([]byte(`{"data":[{"id":"old","type":"session.status_idle","stop_reason":"end_turn"},{"id":"new_input","type":"user.message","metadata":{"channel_id":"chnl_1","channel_type":"feishu","channel_account_id":"app_1","channel_message_id":"om_new","channel_message_ids":["om_new"],"channel_chat_id":"oc_1","channel_actor_id":"ou_1"}},{"id":"answer","type":"agent.message","content":"本轮快速回复"},{"id":"done","type":"session.status_idle","stop_reason":"end_turn"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"data":[{"id":"old","type":"session.status_idle","stop_reason":"end_turn"}]}`))
			}
		default:
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle","data":[]}`))
		}
	}))
	t.Cleanup(server.Close)
	worker, fake := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, server.URL)
	worker.supervisor.subscribeEvents = testEventSubscriber(t, func() []omaEvent {
		return []omaEvent{{ID: "new_input", Type: "user.message"}, {ID: "answer", Type: "agent.message", Content: []byte(`"本轮快速回复"`)}, {ID: "done", Type: "session.status_idle", StopReason: []byte(`"end_turn"`)}}
	})
	msg := &channeltypes.NormalizedMessage{ChatID: "oc_1", ChatType: "p2p", UserID: "ou_1", MessageID: "om_new", Content: "马上回答"}
	if err := worker.handleMessage(t.Context(), feishuMessage(msg), "chat:oc_1"); err != nil {
		t.Fatal(err)
	}
	waitForScope(t, worker, "chat:oc_1")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !submitted.Load() || !strings.Contains(strings.Join(fake.appends, ""), "本轮快速回复") {
		t.Fatalf("没有事件ID的提交回执也不能跳过已产生的新答案: submitted=%v updates=%v", submitted.Load(), fake.appends)
	}
}

func TestStopUpdatesOriginalConversationCard(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		t.Run(map[bool]string{true: "已确认", false: "未确认"}[confirmed], func(t *testing.T) {
			var sent, interrupted atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/resources"):
					_, _ = w.Write([]byte(`{"data":[]}`))
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
					var body struct {
						Events []struct {
							Type string `json:"type"`
						} `json:"events"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.Events[0].Type == "user.interrupt" {
						interrupted.Store(true)
						if !confirmed {
							http.Error(w, "stop unavailable", http.StatusServiceUnavailable)
							return
						}
						_, _ = w.Write([]byte(`{}`))
					} else {
						sent.Store(true)
						_, _ = w.Write([]byte(`{"data":[{"id":"input","type":"user.message"}]}`))
					}
				case strings.HasSuffix(r.URL.Path, "/events"):
					if interrupted.Load() && confirmed {
						_, _ = w.Write([]byte(`{"data":[{"id":"input","type":"user.message"},{"id":"tool","type":"agent.tool_use","name":"bash","input":{"command":"sleep 30"}},{"id":"result","type":"agent.tool_result","tool_use_id":"tool","is_error":true,"content":[{"type":"text","text":"Tool call interrupted."}]},{"id":"end","type":"session.status_idle","stop_reason":{"type":"end_turn"}}]}`))
					} else if sent.Load() {
						_, _ = w.Write([]byte(`{"data":[{"id":"input","type":"user.message"},{"id":"tool","type":"agent.tool_use","name":"bash","input":{"command":"sleep 30"}}]}`))
					} else {
						_, _ = w.Write([]byte(`{"data":[]}`))
					}
				default:
					_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
				}
			}))
			t.Cleanup(server.Close)
			worker, fake := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, server.URL)
			worker.record.IncludeToolCalls = true
			worker.supervisor.subscribeEvents = testEventSubscriber(t, func() []omaEvent {
				return []omaEvent{{ID: "input", Type: "user.message"}, {ID: "tool", Type: "agent.tool_use", Name: "bash", Input: []byte(`{"command":"sleep 30"}`)}}
			})
			if err := worker.handleMessage(t.Context(), &channelMessage{ChatID: "oc_1", UserID: "ou_1", Content: "长任务"}, "chat:oc_1"); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				fake.mu.Lock()
				running := len(fake.appends) > 0 && strings.Contains(fake.appends[len(fake.appends)-1], "正在调用工具")
				fake.mu.Unlock()
				if running {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("原卡片没有进入工具运行状态")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := worker.handleMessage(t.Context(), &channelMessage{ChatID: "oc_1", UserID: "ou_1", Content: "/stop"}, "chat:oc_1"); err != nil {
				t.Fatal(err)
			}
			waitForScope(t, worker, "chat:oc_1")
			fake.mu.Lock()
			defer fake.mu.Unlock()
			cards := 0
			for _, input := range fake.inputs {
				if input.Card != "" {
					cards++
				}
			}
			last := fake.appends[len(fake.appends)-1]
			expected := "停止待确认"
			if confirmed {
				expected = "已停止"
			}
			if confirmed && !strings.Contains(last, "Tool call interrupted.") {
				t.Fatalf("原卡片遗漏停止后的工具结果: %s", last)
			}
			if cards != 1 || !strings.Contains(last, expected) || strings.Contains(last, "停止对话") || !strings.Contains(last, "sleep 30") {
				t.Fatalf("必须更新同一张卡片、保留工具面板并按确认结果结束: cards=%d card=%s", cards, last)
			}
		})
	}
}
