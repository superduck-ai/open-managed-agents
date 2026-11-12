package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/scene/registration"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

func testCipher(t *testing.T) *secrets.Service {
	t.Helper()
	provider, err := local.New(local.KeyMaterial{Version: 1, KEK: make([]byte, 32)}, nil)
	if err != nil {
		t.Fatalf("local provider: %v", err)
	}
	return secrets.NewService(provider)
}

type fakeLocalStore struct {
	localStore
	agent       db.Agent
	agentErr    error
	created     db.ChannelRecordInput
	createErr   error
	records     []db.ChannelRecord
	pairings    []db.ChannelPairingRecord
	environment string
	envErr      error
}

func (f *fakeLocalStore) GetAgent(context.Context, string, string) (db.Agent, error) {
	return f.agent, f.agentErr
}

func (f *fakeLocalStore) CreateChannelRecord(_ context.Context, input db.ChannelRecordInput) (db.ChannelRecord, error) {
	f.created = input
	if f.createErr != nil {
		return db.ChannelRecord{}, f.createErr
	}
	return db.ChannelRecord{
		ExternalID:       input.ExternalID,
		OrganizationUUID: input.OrganizationUUID,
		WorkspaceUUID:    input.WorkspaceUUID,
		AgentExternalID:  input.AgentExternalID,
		Name:             input.Name,
		ChannelType:      input.ChannelType,
		Mode:             input.Mode,
		Enabled:          input.Enabled,
		BindingStatus:    input.BindingStatus,
		AppID:            input.AppID,
		CreatedAt:        time.Now(),
	}, nil
}

func (f *fakeLocalStore) ListAgentChannelRecords(context.Context, string, string, string) ([]db.ChannelRecord, error) {
	return f.records, nil
}

func (f *fakeLocalStore) ListChannelPairingRecords(context.Context, string) ([]db.ChannelPairingRecord, error) {
	return f.pairings, nil
}

func (f *fakeLocalStore) GetChannelAgentEnvironment(context.Context, string, string, string) (string, error) {
	return f.environment, f.envErr
}

func TestLocalRuntimeEnabled(t *testing.T) {
	cases := []struct {
		name   string
		cfg    config.Config
		expect bool
	}{
		{name: "默认无桥接时使用原生运行时", cfg: config.Config{}, expect: true},
		{name: "配置桥接时保持桥接", cfg: config.Config{ChannelBridge: config.ChannelBridgeConfig{URL: "http://bridge", Token: "token"}}, expect: false},
		{name: "显式原生覆盖桥接", cfg: config.Config{Channels: config.ChannelsConfig{Runtime: "local"}, ChannelBridge: config.ChannelBridgeConfig{URL: "http://bridge", Token: "token"}}, expect: true},
		{name: "显式桥接", cfg: config.Config{Channels: config.ChannelsConfig{Runtime: "bridge"}}, expect: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := localRuntimeEnabled(tc.cfg); got != tc.expect {
				t.Fatalf("localRuntimeEnabled = %v, want %v", got, tc.expect)
			}
		})
	}
}

func TestOMABaseURL(t *testing.T) {
	cases := []struct {
		name   string
		cfg    config.Config
		expect string
	}{
		{name: "显式地址优先", cfg: config.Config{Channels: config.ChannelsConfig{OMAURL: "https://oma.example"}, Server: config.ServerConfig{Addr: "127.0.0.1:38080"}}, expect: "https://oma.example"},
		{name: "回落到监听地址", cfg: config.Config{Server: config.ServerConfig{Addr: "127.0.0.1:38080"}}, expect: "http://127.0.0.1:38080"},
		{name: "空主机补齐回环", cfg: config.Config{Server: config.ServerConfig{Addr: ":38081"}}, expect: "http://127.0.0.1:38081"},
		{name: "未配置监听地址", cfg: config.Config{}, expect: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := omaBaseURL(tc.cfg); got != tc.expect {
				t.Fatalf("omaBaseURL = %q, want %q", got, tc.expect)
			}
		})
	}
}

func TestDecodeChannelMutationMounts(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "空挂载", body: `{"name":"渠道","mode":"open","memory_store_id":null,"vault_id":null,"git_repository":null}`},
		{name: "挂载 Memory", body: `{"name":"渠道","mode":"open","memory_store_id":"mem_1"}`, wantErr: true},
		{name: "挂载 Vault", body: `{"name":"渠道","mode":"open","vault_id":"vlt_1"}`, wantErr: true},
		{name: "挂载 Git", body: `{"name":"渠道","mode":"open","git_repository":{"url":"https://example.com/repo.git"}}`, wantErr: true},
		{name: "扫码授权", body: `{"name":"渠道","mode":"open","authorization_id":"chra_1"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			_, err := decodeChannelMutation(httptest.NewRecorder(), request)
			if tc.wantErr != (err != nil) {
				t.Fatalf("decodeChannelMutation err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestServiceWithoutChannelConfig(t *testing.T) {
	service := NewService(config.Config{}, nil, nil, nil)
	if service.AgentHandler() == nil || service.WorkspaceHandler() == nil {
		t.Fatal("未配置渠道时也必须返回可用的处理器")
	}
	service.Start(context.Background())
	service.Stop()
	var nilService *Service
	nilService.Start(context.Background())
	nilService.Stop()
}

type fakeRegistrationStore struct {
	localStore
	records map[string]db.ChannelRegistrationRecord
}

func (f *fakeRegistrationStore) CreateChannelRegistration(_ context.Context, input db.ChannelRegistrationInput) (db.ChannelRegistrationRecord, error) {
	record := db.ChannelRegistrationRecord{
		ExternalID: input.ExternalID, Kind: input.Kind, OrganizationUUID: input.OrganizationUUID,
		WorkspaceUUID: input.WorkspaceUUID, ChannelExternalID: input.ChannelExternalID, Name: input.Name,
		Phase: input.Phase, URL: input.URL, ExpiresAt: input.ExpiresAt, NextPollAt: input.NextPollAt,
		IntervalSeconds: input.IntervalSeconds, Domain: input.Domain, DeviceCodeEnvelope: input.DeviceCodeEnvelope,
		Owner: input.Owner, HeartbeatAt: time.Now(),
	}
	f.records[record.ExternalID] = record
	return record, nil
}

func (f *fakeRegistrationStore) GetChannelRegistration(_ context.Context, organizationUUID, workspaceUUID, externalID string) (db.ChannelRegistrationRecord, error) {
	record, ok := f.records[externalID]
	if !ok || record.OrganizationUUID != organizationUUID || record.WorkspaceUUID != workspaceUUID {
		return db.ChannelRegistrationRecord{}, db.ErrNotFound
	}
	return record, nil
}

func (f *fakeRegistrationStore) GetChannelQrRegistration(context.Context, string) (db.ChannelRegistrationRecord, error) {
	return db.ChannelRegistrationRecord{}, db.ErrNotFound
}

func (f *fakeRegistrationStore) UpdateChannelRegistration(_ context.Context, update db.ChannelRegistrationUpdate) (db.ChannelRegistrationRecord, error) {
	record, ok := f.records[update.ExternalID]
	if !ok || record.Phase != registrationPhaseWaiting {
		return db.ChannelRegistrationRecord{}, db.ErrNotFound
	}
	record.Owner = update.Owner
	record.URL = update.URL
	record.ExpiresAt = update.ExpiresAt
	record.IntervalSeconds = update.IntervalSeconds
	record.HeartbeatAt = time.Now()
	f.records[record.ExternalID] = record
	return record, nil
}

func (f *fakeRegistrationStore) TransitionChannelRegistration(_ context.Context, update db.ChannelRegistrationUpdate) (db.ChannelRegistrationRecord, error) {
	record, ok := f.records[update.ExternalID]
	if !ok || record.Phase != registrationPhaseWaiting {
		return db.ChannelRegistrationRecord{}, db.ErrNotFound
	}
	record.Phase = update.Phase
	record.URL = update.URL
	record.ExpiresAt = update.ExpiresAt
	record.NextPollAt = update.NextPollAt
	record.IntervalSeconds = update.IntervalSeconds
	record.Domain = update.Domain
	record.DeviceCodeEnvelope = update.DeviceCodeEnvelope
	record.AppID = update.AppID
	record.AppSecretEnvelope = update.AppSecretEnvelope
	record.DeviceCodeEnvelope = update.DeviceCodeEnvelope
	f.records[record.ExternalID] = record
	return record, nil
}

func (f *fakeRegistrationStore) DeleteChannelRegistration(_ context.Context, externalID string) error {
	delete(f.records, externalID)
	return nil
}

type fakeRegistrar struct {
	result *registration.RegisterAppResult
	err    error
	delay  time.Duration
	opts   *registration.Options
}

func (f *fakeRegistrar) RegisterApp(ctx context.Context, opts *registration.Options) (*registration.RegisterAppResult, error) {
	f.opts = opts
	opts.OnQRCode(&registration.QRCodeInfo{URL: "https://accounts.feishu.cn/page/qr?tp=sdk", ExpireIn: 600})
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.result == nil {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.result, nil
}

func newAuthorizationService(t *testing.T, registrar appRegistrar) (*localService, *fakeRegistrationStore) {
	t.Helper()
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	t.Cleanup(tokenServer.Close)
	store := &fakeRegistrationStore{records: map[string]db.ChannelRegistrationRecord{}}
	service := newLocalService(store, testCipher(t), nil, nil)
	service.registrar = registrar
	service.client = tokenServer.Client()
	service.tokenURL = tokenServer.URL
	return service, store
}

func TestAuthorizationUsesSDKRegistration(t *testing.T) {
	registrar := &fakeRegistrar{result: &registration.RegisterAppResult{ClientID: "cli_new", ClientSecret: "secret"}, delay: 20 * time.Millisecond}
	service, store := newAuthorizationService(t, registrar)
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	view, err := service.beginAuthorization(context.Background(), principal, "我的机器人")
	if err != nil {
		t.Fatalf("beginAuthorization: %v", err)
	}
	if view["status"] != "pending" || view["url"] == "" || view["id"] == "" {
		t.Fatalf("授权视图 = %v", view)
	}
	if registrar.opts == nil || registrar.opts.AppPreset == nil || registrar.opts.AppPreset.Name != "我的机器人" {
		t.Fatalf("SDK 选项 = %+v", registrar.opts)
	}
	if registrar.opts.Addons == nil || len(registrar.opts.Addons.Scopes.Tenant) == 0 || len(registrar.opts.Addons.Events.Items.Tenant) == 0 {
		t.Fatalf("SDK 权限预置缺失: %+v", registrar.opts.Addons)
	}
	if registrar.opts.Source != "open-managed-agents" {
		t.Fatalf("SDK Source = %q", registrar.opts.Source)
	}
	stored := store.records[view["id"].(string)]
	if stored.URL == "" {
		t.Fatal("二维码地址必须落库")
	}
	deadline := time.Now().Add(2 * time.Second)
	var bound map[string]any
	for time.Now().Before(deadline) {
		bound, err = service.authorizationStatus(context.Background(), principal, stored.ExternalID)
		if err != nil {
			t.Fatalf("authorizationStatus: %v", err)
		}
		if bound["status"] == "bound" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if bound["status"] != "bound" || bound["app_id"] != "cli_new" {
		t.Fatalf("授权完成状态 = %v", bound)
	}
	appID, secret, err := service.authorizationCredentials(context.Background(), principal, stored.ExternalID)
	if err != nil || appID != "cli_new" || secret != "secret" {
		t.Fatalf("授权凭据 = %q, %q, %v", appID, secret, err)
	}
}

func TestAuthorizationDeniedByUser(t *testing.T) {
	registrar := &fakeRegistrar{err: &registration.AccessDeniedError{RegisterAppError: &registration.RegisterAppError{Code: "access_denied"}}, delay: 10 * time.Millisecond}
	service, store := newAuthorizationService(t, registrar)
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	view, err := service.beginAuthorization(context.Background(), principal, "机器人")
	if err != nil {
		t.Fatalf("beginAuthorization: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var status map[string]any
	for time.Now().Before(deadline) {
		status, err = service.authorizationStatus(context.Background(), principal, view["id"].(string))
		if err != nil {
			t.Fatalf("authorizationStatus: %v", err)
		}
		if status["status"] == "error" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status["status"] != "error" {
		t.Fatalf("拒绝后的状态 = %v，记录 %+v", status, store.records)
	}
}

func TestAuthorizationWithForeignHeartbeatStaysPending(t *testing.T) {
	service, store := newAuthorizationService(t, &fakeRegistrar{})
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	record, err := store.CreateChannelRegistration(context.Background(), db.ChannelRegistrationInput{
		ExternalID: "chra_y", Kind: registrationKindAuth, OrganizationUUID: "org", WorkspaceUUID: "ws",
		Phase: registrationPhaseWaiting, ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	view, err := service.authorizationStatus(context.Background(), principal, record.ExternalID)
	if err != nil {
		t.Fatalf("authorizationStatus: %v", err)
	}
	if view["status"] != "pending" {
		t.Fatalf("其他实例仍在轮询时应保持 pending，实际 %v", view)
	}
}

func TestAuthorizationWithoutRunnerBecomesUnknown(t *testing.T) {
	service, store := newAuthorizationService(t, &fakeRegistrar{})
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	record, err := store.CreateChannelRegistration(context.Background(), db.ChannelRegistrationInput{
		ExternalID: "chra_x", Kind: registrationKindAuth, OrganizationUUID: "org", WorkspaceUUID: "ws",
		Phase: registrationPhaseWaiting, ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	record.HeartbeatAt = time.Now().Add(-time.Hour)
	store.records[record.ExternalID] = record
	view, err := service.authorizationStatus(context.Background(), principal, record.ExternalID)
	if err != nil {
		t.Fatalf("authorizationStatus: %v", err)
	}
	if view["status"] != "unknown" {
		t.Fatalf("进程重启后无执行器应为 unknown，实际 %v", view)
	}
}

func TestRegistrationStatusMapping(t *testing.T) {
	cases := map[string]string{"waiting": "pending", "bound": "bound", "expired": "expired", "denied": "error", "unknown": "unknown", "other": "unknown"}
	for phase, expect := range cases {
		if got := registrationStatus(phase); got != expect {
			t.Fatalf("registrationStatus(%s) = %s", phase, got)
		}
	}
}

func TestChannelSecretRoundTrip(t *testing.T) {
	cipher := testCipher(t)
	record := db.ChannelRecord{ExternalID: "chnl_a", OrganizationUUID: "org", WorkspaceUUID: "ws"}
	sealed, err := sealChannelSecret(context.Background(), cipher, record, "secret-value")
	if err != nil || sealed == nil {
		t.Fatalf("sealChannelSecret = %v, %v", sealed, err)
	}
	opened, err := openSealedSecret(context.Background(), cipher, record, *sealed)
	if err != nil || opened != "secret-value" {
		t.Fatalf("openSealedSecret = %q, %v", opened, err)
	}
	other := db.ChannelRecord{ExternalID: "chnl_b", OrganizationUUID: "org", WorkspaceUUID: "ws"}
	if _, err := openSealedSecret(context.Background(), cipher, other, *sealed); err == nil {
		t.Fatal("跨渠道解密应当失败")
	}
}

func TestLocalServiceCreateRequiresValidModeAndName(t *testing.T) {
	store := &fakeLocalStore{agent: db.Agent{ExternalID: "agent_a", Name: "Agent"}}
	service := newLocalService(store, testCipher(t), nil, nil)
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	mode := "unsupported"
	if _, err := service.create(context.Background(), principal, "agent_a", channelMutation{Name: stringPointer("渠道"), Mode: &mode}); err == nil {
		t.Fatal("非法模式应当失败")
	}
	open := "open"
	if _, err := service.create(context.Background(), principal, "agent_a", channelMutation{Name: stringPointer("   "), Mode: &open}); err == nil {
		t.Fatal("空名称应当失败")
	}
	view, err := service.create(context.Background(), principal, "agent_a", channelMutation{Name: stringPointer("飞书机器人"), Mode: &open})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if view["binding_status"] != "unbound" || store.created.BindingStatus != "unbound" {
		t.Fatalf("未绑定渠道状态 = %v / %v", view["binding_status"], store.created.BindingStatus)
	}
	if store.created.ExternalID == "" || store.created.Mode != "open" || store.created.ChannelType != "feishu" {
		t.Fatalf("创建参数 = %+v", store.created)
	}
}

func TestOMAClientSendsWorkspaceScopedRequests(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("X-Api-Key") != "key" || r.Header.Get("anthropic-beta") != omaManagedAgentsBeta {
			t.Errorf("请求头缺失: %v", r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1/sessions":
			var payload struct {
				Agent string            `json:"agent"`
				Env   string            `json:"environment_id"`
				Meta  map[string]string `json:"metadata"`
			}
			if err := json.Unmarshal(body, &payload); err != nil || payload.Agent != "agent_a" || payload.Env != "env_a" {
				t.Errorf("会话创建正文 = %s", body)
			}
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
		case "/v1/sessions/sesn_1/events":
			_, _ = w.Write([]byte(`{"data":[{"id":"evt_1","type":"user.message"}]}`))
		default:
			t.Errorf("未预期路径 %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newOMAClient(server.URL)
	sessionID, err := client.createSession(context.Background(), "key", "agent_a", "env_a", "标题", map[string]string{"source": "feishu"})
	if err != nil || sessionID != "sesn_1" {
		t.Fatalf("createSession = %q, %v", sessionID, err)
	}
	if err := client.sendUserMessage(context.Background(), "key", sessionID, "你好"); err != nil {
		t.Fatalf("sendUserMessage: %v", err)
	}
	if len(paths) != 2 || paths[0] != "/v1/sessions?beta=true" {
		t.Fatalf("请求路径 = %v", paths)
	}
}

func TestTerminalEvent(t *testing.T) {
	cases := []struct {
		event  omaEvent
		expect bool
	}{
		{event: omaEvent{Type: "session.status_idle", StopReason: json.RawMessage(`"end_turn"`)}, expect: true},
		{event: omaEvent{Type: "session.status_idle", StopReason: json.RawMessage(`{"type":"interrupted"}`)}, expect: true},
		{event: omaEvent{Type: "session.status_idle", StopReason: json.RawMessage(`""`)}, expect: false},
		{event: omaEvent{Type: "agent.message"}, expect: false},
		{event: omaEvent{Type: "session.error"}, expect: true},
	}
	for _, tc := range cases {
		if got := terminalEvent(tc.event); got != tc.expect {
			t.Fatalf("terminalEvent(%s, %s) = %v", tc.event.Type, tc.event.StopReason, got)
		}
	}
}

func TestChannelSessionTitle(t *testing.T) {
	if got := channelSessionTitle("  修复登录问题  "); got != "  修复登录问题  " {
		t.Fatalf("标题 = %q", got)
	}
	long := make([]rune, 60)
	for index := range long {
		long[index] = '好'
	}
	if got := []rune(channelSessionTitle(string(long))); len(got) != channelTitleLimit {
		t.Fatalf("标题长度 = %d", len(got))
	}
	if got := channelSessionTitle(""); got != "飞书会话" {
		t.Fatalf("空标题 = %q", got)
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestChannelReplyHonorsDisplayOptions(t *testing.T) {
	message := omaEvent{Type: "agent.message", Content: []byte(`[{"type":"text","text":"你好"}]`)}
	thinking := omaEvent{Type: "agent.thinking"}
	tool := omaEvent{Type: "agent.tool_use", Name: "Bash", Content: []byte(`{"command":"ls"}`)}
	quiet := &channelReply{}
	if text, changed := quiet.consume(db.ChannelRecord{}, message); text != "你好\n" || !changed {
		t.Fatalf("普通回复 = %q, %v", text, changed)
	}
	if _, changed := (&channelReply{}).consume(db.ChannelRecord{}, thinking); changed {
		t.Fatal("未开启思考时不应产生更新")
	}
	if _, changed := (&channelReply{}).consume(db.ChannelRecord{}, tool); changed {
		t.Fatal("未开启工具调用时不应产生更新")
	}
	loud := &channelReply{card: true}
	if _, changed := loud.consume(db.ChannelRecord{IncludeThinking: true}, thinking); !changed || !loud.thinking {
		t.Fatal("开启思考后应进入思考状态")
	}
	if card := loud.render(); !strings.Contains(card, "正在思考") {
		t.Fatalf("思考状态卡片 = %s", card)
	}
	if text, changed := loud.consume(db.ChannelRecord{IncludeToolCalls: true}, tool); text != "🔧 Bash\n" || !changed || loud.thinking {
		t.Fatalf("工具输出 = %q, %v，思考中 = %v", text, changed, loud.thinking)
	}
	if card := loud.render(); strings.Contains(card, "正在思考") || !strings.Contains(card, "Bash") {
		t.Fatalf("工具调用后的卡片 = %s", card)
	}
}

func TestReplyMessageRequestsThreadReply(t *testing.T) {
	var replyBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal"):
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"token","expire":7200}`))
		case strings.Contains(r.URL.Path, "/im/v1/messages/") && strings.HasSuffix(r.URL.Path, "/reply"):
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &replyBody); err != nil {
				t.Errorf("解析回复请求: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"message_id":"om_2"}}`))
		default:
			t.Errorf("未预期的请求 %s", r.URL.Path)
			_, _ = w.Write([]byte(`{"code":0}`))
		}
	}))
	defer server.Close()
	worker := &channelWorker{
		client: lark.NewClient("cli_test", "secret", lark.WithOpenBaseUrl(server.URL)),
		record: db.ChannelRecord{ReplyInThread: true},
	}
	messageID, err := worker.replyMessage(context.Background(), "om_1", "text", `{"text":"好的"}`)
	if err != nil {
		t.Fatalf("replyMessage: %v", err)
	}
	if messageID != "om_2" {
		t.Fatalf("回复消息 ID = %q", messageID)
	}
	if replyBody["reply_in_thread"] != true {
		t.Fatalf("回复请求必须带 reply_in_thread=true，实际 %v", replyBody)
	}
}
