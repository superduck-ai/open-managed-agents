package filestore

import "context"

type principalContextKey struct{}

// Principal 是 Filestore 资源层唯一使用的授权身份。
//
// 所有字段都来自专用 Filestore JWT 及其数据库范围回查；该类型不承载
// workspace API key 或 code-session 凭证的兼容身份。
type Principal struct {
	Subject                string
	OrganizationID         int64
	OrganizationUUID       string
	OrganizationExternalID string
	WorkspaceID            int64
	WorkspaceUUID          string
	WorkspaceExternalID    string
	AccountID              int64
	AccountUUID            string
	AccountExternalID      string

	FilesystemInternalID int64
	FilesystemUUID       string
	FilesystemExternalID string
	Readonly             bool
	WritePrefixes        []string
	// OrganizationTaints 是已经与数据库现值核对过的组织级安全与合规标签，
	// 供资源层后续策略判断使用；当前不直接改变文件操作权限。
	OrganizationTaints []string
	// WorkspaceCMEKEnabled 表示工作区当前配置了客户管理密钥，
	// 仅传递策略上下文，不表示对象存储请求已经应用相应密钥。
	WorkspaceCMEKEnabled bool
}

// WithPrincipal 把 Filestore 身份写入资源私有的 context key，防止其他 API
// 将其误读为全局身份，或把全局 Principal 误送入 Filestore handler。
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext 只读取 Filestore 自己写入的授权身份。
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
