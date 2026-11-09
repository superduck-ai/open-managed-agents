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
	provider db.PlatformOAuthProvider
	writes   int
}

func (s *oauthProviderStoreStub) ListPlatformOAuthProviders(context.Context) ([]db.PlatformOAuthProvider, error) {
	other := "friday"
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
	return s.provider, nil
}
func (s *oauthProviderStoreStub) UpdatePlatformOAuthProvider(_ context.Context, provider db.PlatformOAuthProvider) (db.PlatformOAuthProvider, error) {
	s.writes++
	s.provider = provider
	s.provider.Version++
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

func TestOAuthProviderRejectsProtectedChangesAndIncompleteClients(t *testing.T) {
	service, store, records := oauthServiceFixture(t)
	for _, payload := range []string{
		`{"expected_version":1,"name":"Feishu"}`, `{"expected_version":1,"authorize_endpoint":null}`,
		`{"expected_version":1,"scope":"read"}`, `{"expected_version":1,"client_id":"client"}`,
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
	if err := service.Delete(t.Context(), Actor{AccountUUID: "actor"}, store.provider.ExternalID, 1); !errors.Is(err, ErrOAuthProviderProtected) {
		t.Fatal("protected deletion allowed")
	}
	if store.writes != 0 {
		t.Fatal("rejected operation wrote data")
	}
	if len(*records) == 0 {
		t.Fatal("protected refusal missing audit")
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

func TestOAuthProviderFridayProtectedConfiguration(t *testing.T) {
	service, store, records := oauthServiceFixture(t)
	store.provider.ExternalID, store.provider.BuiltinKey, store.provider.Name = "poap_FridayBuiltinOAuthClient", "friday", "Friday"
	store.provider.Scope, store.provider.TokenEndpointAuthMethod, store.provider.TokenEndpointEncoding = "read", "client_secret_basic", "form"
	for _, input := range []OAuthProviderUpdateInput{
		{ExpectedVersion: 1, Scope: new("offline_access")},
		{ExpectedVersion: 1, TokenEndpoint: new("https://changed.test/token")},
		{ExpectedVersion: 1, ClientID: new("synthetic-client")},
	} {
		if _, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, input); err == nil {
			t.Fatal("Friday 接受不合法配置")
		}
	}
	if err := service.Delete(t.Context(), Actor{}, store.provider.ExternalID, 1); !errors.Is(err, ErrOAuthProviderProtected) {
		t.Fatal("Friday 允许删除")
	}
	view, err := service.Update(t.Context(), Actor{}, store.provider.ExternalID, OAuthProviderUpdateInput{ExpectedVersion: 1, ClientID: new("synthetic-friday-client"), ClientSecret: new("synthetic-friday-secret")})
	if err != nil || view.ProviderKey != "builtin:friday" || !view.Configured {
		t.Fatal("Friday 首配失败", err)
	}
	original := bytes.Clone(store.provider.ClientIDEnvelope.Ciphertext)
	view, err = service.Update(t.Context(), Actor{}, view.ID, OAuthProviderUpdateInput{ExpectedVersion: view.Version, ClientSecret: new("synthetic-rotated-secret")})
	if err != nil || !bytes.Equal(original, store.provider.ClientIDEnvelope.Ciphertext) {
		t.Fatal("Friday 轮换破坏客户端 ID", err)
	}
	encoded, err := json.Marshal(view)
	if err != nil || bytes.Contains(encoded, []byte("synthetic-rotated-secret")) {
		t.Fatal("Friday 响应泄漏秘密")
	}
	for _, record := range *records {
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "provider_key" && attr.Value.String() != "builtin:friday" {
				t.Error("Friday 审计身份错误")
			}
			return true
		})
	}
}
