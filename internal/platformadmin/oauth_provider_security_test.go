package platformadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

type oauthProviderStoreStub struct {
	provider   db.PlatformOAuthProvider
	writes     int
	afterWrite func()
}

func (s *oauthProviderStoreStub) ListPlatformOAuthProviders(context.Context) ([]db.PlatformOAuthProvider, error) {
	other := "custom"
	if s.provider.BuiltinKey == other {
		other = "feishu"
	}
	return []db.PlatformOAuthProvider{s.provider, {BuiltinKey: other}}, nil
}
func (s *oauthProviderStoreStub) GetPlatformOAuthProviderByExternalID(context.Context, string) (db.PlatformOAuthProvider, error) {
	return s.provider, nil
}
func (s *oauthProviderStoreStub) CreatePlatformOAuthProvider(_ context.Context, provider db.PlatformOAuthProvider) (db.PlatformOAuthProvider, error) {
	s.writes++
	s.provider = provider
	s.provider.Version = 1
	if s.afterWrite != nil {
		s.afterWrite()
	}
	return s.provider, nil
}
func (s *oauthProviderStoreStub) UpdatePlatformOAuthProvider(_ context.Context, provider db.PlatformOAuthProvider) (db.PlatformOAuthProvider, error) {
	s.writes++
	s.provider = provider
	s.provider.Version++
	if s.afterWrite != nil {
		s.afterWrite()
	}
	return s.provider, nil
}
func (s *oauthProviderStoreStub) DeletePlatformOAuthProvider(context.Context, string, int64) error {
	s.writes++
	return nil
}

type oauthAuditHandler struct {
	slog.Handler
	records *[]slog.Record
}

func (h oauthAuditHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h oauthAuditHandler) Handle(_ context.Context, record slog.Record) error {
	*h.records = append(*h.records, record.Clone())
	return nil
}

func oauthServiceFixture(t *testing.T) (*OAuthProviderService, *oauthProviderStoreStub, *[]slog.Record) {
	t.Helper()
	key, err := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{4}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &oauthProviderStoreStub{provider: db.PlatformOAuthProvider{UUID: "provider-uuid", ExternalID: "poap_FeishuBuiltinOAuthClient", BuiltinKey: "feishu", Protected: true, Version: 1, Name: "Feishu", AuthorizeEndpoint: "https://auth.test/authorize", TokenEndpoint: "https://auth.test/token", Scope: "offline_access", TokenEndpointAuthMethod: "client_secret_post", TokenEndpointEncoding: "json"}}
	records := new([]slog.Record)
	return NewOAuthProviderService(store, secrets.NewService(key), slog.New(oauthAuditHandler{records: records})), store, records
}

func TestOAuthProviderRejectsIncompleteClients(t *testing.T) {
	service, store, records := oauthServiceFixture(t)
	for _, payload := range []string{
		`{"expected_version":1,"client_id":"client"}`,
		`{"expected_version":1,"client_secret":"secret"}`, `{"expected_version":1,"client_id":"","client_secret":"secret"}`,
		`{"expected_version":1,"client_id":"client","client_secret":""}`, `{"expected_version":2,"client_id":"client","client_secret":"secret"}`,
	} {
		var input OAuthProviderUpdateInput
		if err := json.Unmarshal([]byte(payload), &input); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Update(t.Context(), Actor{AccountUUID: "actor"}, store.provider.ExternalID, input); err == nil {
			t.Fatalf("invalid update accepted: %s", payload)
		}
	}
	for _, payload := range []string{`{"client_secret":null}`, `{"client_id":null}`, `{"protected":true}`, `{"builtin_key":"feishu"}`} {
		var input OAuthProviderUpdateInput
		if err := json.Unmarshal([]byte(payload), &input); err == nil {
			t.Fatal("invalid client or protection field accepted")
		}
	}
	if store.writes != 0 {
		t.Fatal("rejected operation wrote data")
	}
	if len(*records) == 0 {
		t.Fatal("invalid operation missing audit")
	}
}

func TestOAuthProviderEncryptionRotationAndSafeAudit(t *testing.T) {
	service, store, records := oauthServiceFixture(t)
	actor := Actor{AccountUUID: "actor"}
	view, err := service.Update(t.Context(), actor, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: 1, ClientID: new("synthetic-client"), ClientSecret: new("synthetic-secret")})
	if err != nil || !view.Configured || !view.HasClientID || view.ClientID != "synthetic-client" || view.Version != 2 {
		t.Fatalf("configure: %v", err)
	}
	idCiphertext := bytes.Clone(store.provider.ClientIDEnvelope.Ciphertext)
	secretCiphertext := bytes.Clone(store.provider.ClientSecretEnvelope.Ciphertext)
	if _, err := service.Update(t.Context(), actor, view.ID, OAuthProviderUpdateInput{ExpectedVersion: 2, ClientID: new("other-client")}); err == nil {
		t.Fatal("changed app without secret accepted")
	}
	updated, err := service.Update(t.Context(), actor, view.ID, OAuthProviderUpdateInput{ExpectedVersion: 2, ClientSecret: new("rotated-secret")})
	if err != nil || updated.Version != 3 || !bytes.Equal(idCiphertext, store.provider.ClientIDEnvelope.Ciphertext) || bytes.Equal(secretCiphertext, store.provider.ClientSecretEnvelope.Ciphertext) {
		t.Fatal("secret-only rotation changed ID or kept secret")
	}
	response, err := json.Marshal(updated)
	if err != nil || bytes.Contains(response, []byte("rotated-secret")) || bytes.Contains(response, []byte("envelope")) {
		t.Fatal("secret leaked in response")
	}
	for _, record := range *records {
		if record.Level != slog.LevelInfo || record.Message != "platform oauth provider audit" {
			t.Fatal("unexpected audit event")
		}
		attrs := make(map[string]any)
		record.Attrs(func(attr slog.Attr) bool { attrs[attr.Key] = attr.Value.Any(); return true })
		for _, key := range []string{"actor_account_uuid", "provider_uuid", "provider_key", "action", "changed_fields", "result", "request_id", "error_code"} {
			if _, ok := attrs[key]; !ok {
				t.Fatalf("missing audit attr: %s", key)
			}
		}
		encoded, _ := json.Marshal(attrs)
		if bytes.Contains(encoded, []byte("synthetic-")) || bytes.Contains(encoded, []byte("rotated-secret")) {
			t.Fatal("audit leaked client values")
		}
	}
	service.secretSvc = nil
	if _, err := service.List(t.Context()); !errors.Is(err, ErrOAuthProviderUnavailable) {
		t.Fatal("KMS failure masked as unconfigured")
	}
}

func TestOAuthProviderListDoesNotCacheOrExposeSecret(t *testing.T) {
	service, store, _ := oauthServiceFixture(t)
	if _, err := service.Update(t.Context(), Actor{AccountUUID: "actor"}, store.provider.ExternalID,
		OAuthProviderUpdateInput{ExpectedVersion: 1, ClientID: new("public-client"), ClientSecret: new("private-secret")}); err != nil {
		t.Fatal(err)
	}
	handler := &Handler{oauthProviders: service}
	recorder := httptest.NewRecorder()
	handler.listOAuthProviders(recorder, httptest.NewRequest("GET", "/oauth-providers", nil))
	if recorder.Code != 200 || recorder.Header().Get("Cache-Control") != "no-store" || !bytes.Contains(recorder.Body.Bytes(), []byte("public-client")) || bytes.Contains(recorder.Body.Bytes(), []byte("private-secret")) || bytes.Contains(recorder.Body.Bytes(), []byte("envelope")) {
		t.Fatal("客户端响应缓存或秘密边界错误")
	}
}

func TestOAuthProviderCustomConfiguration(t *testing.T) {
	service, store, records := oauthServiceFixture(t)
	store.provider.ExternalID, store.provider.BuiltinKey, store.provider.Name = "poap_example", "", "Custom"
	store.provider.Scope, store.provider.TokenEndpointAuthMethod, store.provider.TokenEndpointEncoding = "read", "client_secret_basic", "form"
	view, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: 1, ClientID: new("synthetic-custom-client"), ClientSecret: new("synthetic-custom-secret")})
	if err != nil || view.ProviderKey != "custom:poap_example" || !view.Configured {
		t.Fatal("Custom 首配失败", err)
	}
	original := bytes.Clone(store.provider.ClientIDEnvelope.Ciphertext)
	view, err = service.Update(t.Context(), Actor{}, view.ID, OAuthProviderUpdateInput{ExpectedVersion: view.Version, ClientSecret: new("synthetic-rotated-secret")})
	if err != nil || !bytes.Equal(original, store.provider.ClientIDEnvelope.Ciphertext) {
		t.Fatal("Custom 轮换破坏客户端 ID", err)
	}
	encoded, err := json.Marshal(view)
	if err != nil || bytes.Contains(encoded, []byte("synthetic-rotated-secret")) {
		t.Fatal("Custom 响应泄漏秘密")
	}
	for _, record := range *records {
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "provider_key" && attr.Value.String() != "custom:poap_example" {
				t.Error("Custom 审计身份错误")
			}
			return true
		})
	}
}

func TestOAuthProviderOptionalClientsAndLogo(t *testing.T) {
	service, store, _ := oauthServiceFixture(t)
	input := OAuthProviderCreateInput{Name: "Custom", AuthorizeEndpoint: "https://auth.test/authorize", TokenEndpoint: "https://auth.test/token"}
	for _, invalidInput := range []OAuthProviderCreateInput{
		{Name: input.Name, AuthorizeEndpoint: input.AuthorizeEndpoint, TokenEndpoint: input.TokenEndpoint, ClientID: "client"},
		{Name: input.Name, AuthorizeEndpoint: input.AuthorizeEndpoint, TokenEndpoint: input.TokenEndpoint, ClientSecret: "secret"},
		{Name: input.Name, AuthorizeEndpoint: input.AuthorizeEndpoint, TokenEndpoint: input.TokenEndpoint, LogoURL: "javascript:alert(1)"},
	} {
		if _, err := service.Create(t.Context(), Actor{}, invalidInput); err == nil {
			t.Fatal("非法客户端配置被接受")
		}
	}
	if store.writes != 0 {
		t.Fatal("非法配置写入了数据库")
	}
	input.LogoURL = " https://example.test/logo.png "
	created, err := service.Create(t.Context(), Actor{}, input)
	if err != nil || created.Configured || created.HasClientID || created.HasClientSecret || created.LogoURL != "https://example.test/logo.png" {
		t.Fatalf("未配置客户端创建失败：%+v，%v", created, err)
	}
	updated, err := service.Update(t.Context(), Actor{}, created.ID, OAuthProviderUpdateInput{ExpectedVersion: created.Version, LogoURL: new("")})
	if err != nil || updated.LogoURL != "" || updated.Configured {
		t.Fatalf("清除 Logo 失败：%+v，%v", updated, err)
	}
	configured, err := service.Update(t.Context(), Actor{}, created.ID, OAuthProviderUpdateInput{ExpectedVersion: updated.Version, ClientID: new("client"), ClientSecret: new("secret")})
	if err != nil || !configured.Configured {
		t.Fatalf("后续补充凭据失败：%+v，%v", configured, err)
	}
}

type oauthResponseReader struct {
	secrets.KeyProvider
	unavailable bool
}

func (p *oauthResponseReader) UnwrapDEK(ctx context.Context, wrapped secrets.WrappedKey) ([]byte, error) {
	if p.unavailable {
		return nil, errors.New("测试：写入后解密不可用")
	}
	return p.KeyProvider.UnwrapDEK(ctx, wrapped)
}

func TestOAuthProviderResponseDoesNotDecryptAfterWrite(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			service, store, records := oauthServiceFixture(t)
			key, err := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{4}, 32)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			reader := &oauthResponseReader{KeyProvider: key}
			service.secretSvc = secrets.NewService(reader)
			if operation == "update" {
				if err := service.sealClient(t.Context(), &store.provider, "client", "secret"); err != nil {
					t.Fatal(err)
				}
			}
			store.afterWrite = func() { reader.unavailable = true }
			var view OAuthProviderView
			if operation == "create" {
				view, err = service.Create(t.Context(), Actor{}, OAuthProviderCreateInput{Name: "Custom", AuthorizeEndpoint: "https://auth.test/authorize", TokenEndpoint: "https://auth.test/token", ClientID: "client", ClientSecret: "secret"})
			} else {
				view, err = service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: 1, ClientSecret: new("rotated")})
			}
			if err != nil || view.ClientID != "client" || !view.Configured || store.writes != 1 {
				t.Fatalf("已提交操作响应错误：%+v，%v", view, err)
			}
			if len(*records) != 1 {
				t.Fatalf("审计数量 = %d", len(*records))
			}
		})
	}
}

func TestOAuthProviderDefaultVariableAndAvailability(t *testing.T) {
	service, store, _ := oauthServiceFixture(t)
	for _, name := range []string{"bad name", "1TOKEN"} {
		if _, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: 1, DefaultSecretName: new(name)}); err == nil {
			t.Fatal("非法默认变量名被接受")
		}
	}
	if store.writes != 0 {
		t.Fatal("非法默认值写入数据库")
	}
	view, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: 1, DefaultSecretName: new(" CUSTOM_ACCESS_TOKEN "), Enabled: new(false)})
	if err != nil || view.Enabled || view.DefaultSecretName != "CUSTOM_ACCESS_TOKEN" {
		t.Fatal("内置客户端默认值或停用配置失败", err)
	}
	view, err = service.Update(t.Context(), Actor{}, view.ID, OAuthProviderUpdateInput{ExpectedVersion: view.Version, Enabled: new(true)})
	if err != nil || !view.Enabled || view.DefaultSecretName != "CUSTOM_ACCESS_TOKEN" {
		t.Fatal("重新启用丢失默认值", err)
	}
	created, err := service.Create(t.Context(), Actor{}, OAuthProviderCreateInput{Name: "Custom", AuthorizeEndpoint: "https://auth.test/authorize", TokenEndpoint: "https://auth.test/token", DefaultSecretName: "CUSTOM_TOKEN"})
	if err != nil || !created.Enabled || created.DefaultSecretName != "CUSTOM_TOKEN" {
		t.Fatal("自定义客户端默认值失败", err)
	}
}

func TestOAuthProviderPlatformManagementIsUniform(t *testing.T) {
	service, store, _ := oauthServiceFixture(t)
	view, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: 1, Name: new("Team OAuth"), AuthorizeEndpoint: new("https://changed.test/authorize"), TokenEndpoint: new("https://changed.test/token"), Scope: new("read")})
	if err != nil || view.Name != "Team OAuth" || view.Scope != "read" {
		t.Fatal("平台客户端不可编辑", err)
	}
	if err := service.Delete(t.Context(), Actor{}, view.ID, view.Version); err != nil {
		t.Fatal("平台客户端不可删除", err)
	}
}

func TestOAuthProviderArchiveAndRestorePreserveConfiguration(t *testing.T) {
	service, store, _ := oauthServiceFixture(t)
	store.provider.Enabled = true
	archived, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: 1, Archived: new(true)})
	if err != nil || !archived.Archived || !archived.Enabled {
		t.Fatal("归档未保留启用状态", err)
	}
	restored, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: archived.Version, Archived: new(false)})
	if err != nil || restored.Archived || !restored.Enabled || restored.Name != archived.Name {
		t.Fatal("恢复归档配置失败", err)
	}
}

func TestOAuthProviderDefaultHostsUpdate(t *testing.T) {
	service, store, _ := oauthServiceFixture(t)
	hosts := []string{" api.example.test "}
	view, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: store.provider.Version, DefaultAllowedHosts: &hosts})
	if err != nil || len(view.DefaultAllowedHosts) != 1 || view.DefaultAllowedHosts[0] != "api.example.test" {
		t.Fatal("默认允许主机未保存", err)
	}
	invalidHosts := []string{"https://api.example.test/path"}
	if _, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: view.Version, DefaultAllowedHosts: &invalidHosts}); err == nil {
		t.Fatal("默认主机允许了 URL")
	}
	cleared, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: view.Version, DefaultAllowedHosts: new([]string{})})
	if err != nil || len(cleared.DefaultAllowedHosts) != 0 {
		t.Fatal("默认允许主机无法清空", err)
	}
}

func TestPlatformOAuthProviderCannotMutateWorkspaceProvider(t *testing.T) {
	service, store, _ := oauthServiceFixture(t)
	store.provider.WorkspaceUUID = "workspace"
	if _, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatal("平台更新未拒绝工作区服务方", err)
	}
	if err := service.Delete(t.Context(), Actor{}, store.provider.ExternalID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatal("平台删除未拒绝工作区服务方", err)
	}
	if store.writes != 0 {
		t.Fatal("工作区记录被平台管理路径修改")
	}
}
