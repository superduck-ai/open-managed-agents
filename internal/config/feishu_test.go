package config

import "testing"

func TestFeishuConfiguration(t *testing.T) {
	for _, cfg := range []AuthConfig{
		{Feishu: FeishuConfig{AppID: "cli_test"}},
		{Feishu: FeishuConfig{AppSecret: "secret"}},
		{Feishu: FeishuConfig{AppID: "cli_test", AppSecret: " "}},
		{Feishu: FeishuConfig{AppID: "cli_test", AppSecret: "secret"}, SMTP: EmailSMTPConfig{Addr: "invalid"}},
	} {
		if err := validateAuthConfig(EnvironmentDev, cfg); err == nil {
			t.Fatal("incomplete delivery configuration was accepted")
		}
	}
	for _, smtp := range []EmailSMTPConfig{{}, {Addr: "smtp.example.com:587", Username: "sender@example.com", Password: "password"}} {
		if err := validateAuthConfig(EnvironmentProd, AuthConfig{SMTP: smtp, Feishu: FeishuConfig{AppID: "cli_test", AppSecret: "secret"}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadFeishuConfiguration(t *testing.T) {
	prepareLoadTest(t)
	cfg, err := loadConfigTestYAML(t, `
auth:
  feishu:
    app_id: " cli_test "
    app_secret: " feishu-secret "
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Feishu.AppID != "cli_test" || cfg.Auth.Feishu.AppSecret != " feishu-secret " || cfg.Auth.SMTP.Addr != "smtp.example.com:587" {
		t.Fatal("Feishu configuration did not preserve credentials and existing SMTP")
	}
}
