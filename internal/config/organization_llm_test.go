package config

import "testing"

func TestOrganizationLLMConsoleURL(t *testing.T) {
	cfg, err := loadConfigTestYAML(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OrganizationLLM.ConsoleURL != DefaultOrganizationLLMConsoleURL {
		t.Fatalf("console URL = %q", cfg.OrganizationLLM.ConsoleURL)
	}
	if cfg.OrganizationLLM.BaseURL != DefaultOrganizationLLMBaseURL {
		t.Fatalf("base URL = %q", cfg.OrganizationLLM.BaseURL)
	}

	cleared, err := loadConfigTestYAML(t, "organization_llm:\n  console_url: \"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.OrganizationLLM.ConsoleURL != "" {
		t.Fatalf("cleared console URL = %q", cleared.OrganizationLLM.ConsoleURL)
	}

	withPath, err := loadConfigTestYAML(t, "organization_llm:\n  console_url: \"https://ccloud.example/ai/tenant-management\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if withPath.OrganizationLLM.ConsoleURL != "https://ccloud.example/ai/tenant-management" {
		t.Fatalf("console URL = %q", withPath.OrganizationLLM.ConsoleURL)
	}

	production, err := loadConfigTestYAML(t, "organization_llm:\n  base_url: \"https://narrows-gateway.seewo.com\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if production.OrganizationLLM.BaseURL != "https://narrows-gateway.seewo.com" {
		t.Fatalf("base URL = %q", production.OrganizationLLM.BaseURL)
	}
	if _, err := loadConfigTestYAML(t, "organization_llm:\n  base_url: \"https://user:secret@narrows.example\"\n"); err == nil {
		t.Fatal("accepted gateway URL with userinfo")
	}

	for _, input := range []string{
		"http://ccloud.example/ai",
		"https://user:secret@ccloud.example/ai",
		"https://ccloud.example/ai?token=secret",
		"https://ccloud.example/ai#fragment",
		"ccloud.example/ai",
	} {
		if _, err := loadConfigTestYAML(t, "organization_llm:\n  console_url: \""+input+"\"\n"); err == nil {
			t.Fatalf("accepted console URL %q", input)
		}
	}
}
