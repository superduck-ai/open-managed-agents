package clitools

type Binding struct {
	Name              string
	Variable          string
	ProviderVariable  string
	LegacyProviderKey string
}

var Bindings = [...]Binding{{Name: "Friday", Variable: "FRIDAY_4A_TOKEN", ProviderVariable: "OMA_FRIDAY_OAUTH_PROVIDER", LegacyProviderKey: "friday_oauth_provider"}}
