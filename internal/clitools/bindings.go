package clitools

// OAuthProviders 将已登记的 CLI 环境变量关联到授权提供方。
type OAuthProviders map[string]string

type Binding struct {
	Name             string
	Variable         string
	ProviderVariable string
}

var Bindings = [...]Binding{{Name: "Friday", Variable: "FRIDAY_4A_TOKEN", ProviderVariable: "OMA_FRIDAY_OAUTH_PROVIDER"}}

// OAuthConfig 读取会话快照里为 CLI 配置的提供方，兼容早期 Friday 快照。
type OAuthConfig struct {
	Providers            OAuthProviders `json:"cli_oauth_providers"`
	LegacyFridayProvider string         `json:"friday_oauth_provider"`
}

func (c OAuthConfig) Provider(variable string) string {
	for _, cli := range Bindings {
		if cli.Variable != variable {
			continue
		}
		if provider := c.Providers[variable]; provider != "" {
			return provider
		}
		if variable == "FRIDAY_4A_TOKEN" {
			return c.LegacyFridayProvider
		}
	}
	return ""
}
