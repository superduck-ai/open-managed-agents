package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
