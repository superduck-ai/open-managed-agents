//go:build integration

package db

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestOAuthContinuationPostgres(t *testing.T) {
	ctx, _, store := newCodeSessionClaimTestStore(t)
	now := time.Now().UTC()
	continuation := &OAuthContinuation{SessionUUID: uuid.NewV4().String(), EventUUID: uuid.NewV4().String(), ChannelUUID: uuid.NewV4().String(), BrowserHash: "browser-hash", Identity: true}
	envelope := &secrets.Envelope{Ciphertext: []byte("mapping-test"), Nonce: []byte("mapping-test"), WrappedDEK: []byte("mapping-test"), FormatVersion: 1, KeyProvider: "test", KeyVersion: 1}
	for i, association := range []*OAuthContinuation{nil, continuation} {
		env, err := store.CreateEnvOAuthFlow(ctx, EnvOAuthFlow{
			Continuation: association, UUID: uuid.NewV4().String(), ExternalID: fmt.Sprintf("eof_mapping_%d", i),
			OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, VaultUUID: uuid.NewV4().String(), VaultExternalID: "vlt_mapping",
			ProviderKey: "builtin:feishu", ProviderVersion: 1, SecretName: "TEST_TOKEN", RedirectURL: "https://oma.example/oauth/vault/env-success",
			AuthorizeEndpoint: "https://service.example/authorize", TokenEndpoint: "https://service.example/token", CodeVerifierEnvelope: envelope,
			TokenEndpointAuthMethod: "client_secret_post", TokenEndpointEncoding: "form", NetworkingType: "limited", AllowedHosts: []string{"service.example"},
			Status: "pending", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := store.GetEnvOAuthFlow(ctx, env.ExternalID)
		if err != nil || !reflect.DeepEqual(loaded.Continuation, association) {
			t.Fatalf("ENV 交接字段绑定或扫描错误：%v", err)
		}
		mcp, err := store.CreateMCPOAuthFlow(ctx, MCPOAuthFlow{
			Continuation: association, UUID: uuid.NewV4().String(), ExternalID: fmt.Sprintf("mof_mapping_%d", i),
			OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, VaultUUID: uuid.NewV4().String(), VaultExternalID: "vlt_mapping",
			MCPServerURL: "https://service.example/mcp", RedirectURL: "https://oma.example/oauth/vault/success", DisplayName: "SQL 绑定测试",
			AuthorizationEndpoint: "https://service.example/authorize", TokenEndpoint: "https://service.example/token", Resource: "https://service.example/mcp",
			ClientID: "mapping-client", ClientCredentialSource: "sealed", TokenEndpointAuthMethod: "client_secret_basic", CodeChallengeMethod: "S256", SecretEnvelope: envelope,
			Status: "pending", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}
		loadedMCP, err := store.GetMCPOAuthFlow(ctx, mcp.ExternalID)
		if err != nil || !reflect.DeepEqual(loadedMCP.Continuation, association) {
			t.Fatalf("MCP 交接字段绑定或扫描错误：%v", err)
		}
		var accepted atomic.Int32
		var tasks sync.WaitGroup
		for range 4 {
			tasks.Go(func() {
				claimed, err := store.ClaimMCPOAuthFlow(ctx, mcp.ExternalID, now)
				if err != nil {
					t.Error(err)
				}
				if claimed {
					accepted.Add(1)
				}
			})
		}
		tasks.Wait()
		if accepted.Load() != 1 {
			t.Fatalf("同一 MCP 授权码被并发领取 %d 次", accepted.Load())
		}
		if err := store.CompleteMCPOAuthFlow(ctx, mcp.ExternalID, "vcrd_mapping", now); err != nil {
			t.Fatal(err)
		}
		if claimed, err := store.ClaimMCPOAuthFlow(ctx, mcp.ExternalID, now); err != nil || claimed {
			t.Fatalf("已完成授权被再次换票：%v", err)
		}
	}
}
