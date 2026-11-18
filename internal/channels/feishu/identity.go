package feishu

import (
	"context"
	"errors"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func (f *Adapter) OAuthProvider() vaults.EnvOAuthProviderSnapshot {
	return vaults.EnvOAuthProviderSnapshot{
		Builtin: true, Enabled: true, Configured: true, ProviderKey: "builtin:feishu", Name: "渠道操作者核验",
		Version: 1, ClientID: f.Config.AppID, ClientSecret: f.Config.Secret,
		AuthorizeEndpoint:       "https://accounts.feishu.cn/open-apis/authen/v1/authorize",
		TokenEndpoint:           "https://open.feishu.cn/open-apis/authen/v2/oauth/token",
		TokenEndpointAuthMethod: "client_secret_post", TokenEndpointEncoding: "json", Scope: "contact:user.employee_id:readonly",
	}
}

func (f *Adapter) OAuthUserID(ctx context.Context, token string) (string, error) {
	client := lark.NewClient(f.Config.AppID, f.Config.Secret, lark.WithLogLevel(larkcore.LogLevelError))
	info, err := client.Authen.V1.UserInfo.Get(ctx, larkcore.WithUserAccessToken(token))
	if err != nil {
		return "", err
	}
	if !info.Success() || info.Data == nil || info.Data.OpenId == nil {
		return "", errors.New("无法核验飞书授权操作者")
	}
	return *info.Data.OpenId, nil
}
