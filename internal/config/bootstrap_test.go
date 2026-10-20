package config

import "testing"

func TestLoadYAMLResolvesBootstrapIdentityFields(t *testing.T) {
	cfg, err := loadConfigTestYAML(t, `
bootstrap:
  organization_name: OMA Local
  workspace_name: main
  user_email: test@oma.ai
  user_name: Local Test
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bootstrap.OrganizationName != "OMA Local" || cfg.Bootstrap.UserEmail != "test@oma.ai" || cfg.Bootstrap.UserName != "Local Test" {
		t.Fatalf("Bootstrap = %+v", cfg.Bootstrap)
	}
	if len(cfg.Bootstrap.SeedAPIKeys) != 1 || cfg.Bootstrap.SeedAPIKeys[0].Key != DefaultAPIKey {
		t.Fatalf("SeedAPIKeys = %+v", cfg.Bootstrap.SeedAPIKeys)
	}
}

func TestLoadYAMLRejectsInvalidBootstrapIdentity(t *testing.T) {
	cases := map[string]string{
		"missing user email":     "\nbootstrap:\n  user_email: \"\"\n",
		"invalid user email":     "\nbootstrap:\n  user_email: not-an-email\n",
		"empty organization":     "\nbootstrap:\n  organization_name: \"\"\n",
		"empty workspace ext id": "\nbootstrap:\n  workspace_external_id: \"\"\n",
		"empty user name":        "\nbootstrap:\n  user_name: \"\"\n",
	}
	for name, addition := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConfigTestYAML(t, addition); err == nil {
				t.Fatalf("Load() error = nil for %s, want failure", name)
			}
		})
	}
}
