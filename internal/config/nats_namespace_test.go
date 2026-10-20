package config

import "testing"

func TestLoadYAMLValidatesNATSNamespace(t *testing.T) {
	invalid := []string{"S1", "s1.extra", "s*", "s" + repeatChar('a', 16), "-lead", "a.b", "s1 upper"}
	for _, namespace := range invalid {
		_, err := loadConfigTestYAML(t, "nats:\n  namespace: "+namespace+"\n")
		if err == nil {
			t.Errorf("Load() error = nil for nats.namespace %q, want failure", namespace)
		}
	}
	for _, namespace := range []string{"s1", "slot-1_x", "trail-"} {
		cfg, err := loadConfigTestYAML(t, "nats:\n  namespace: "+namespace+"\n")
		if err != nil {
			t.Fatalf("Load() error = %v for nats.namespace %q, want success", err, namespace)
		}
		if cfg.NATS.Namespace != namespace {
			t.Fatalf("NATS.Namespace = %q, want %q", cfg.NATS.Namespace, namespace)
		}
	}
	if cfg, err := loadConfigTestYAML(t, ""); err != nil || cfg.NATS.Namespace != "" {
		t.Fatalf("Load() without namespace = %q, %v, want empty namespace", cfg.NATS.Namespace, err)
	}
}

func repeatChar(char byte, count int) string {
	value := make([]byte, count)
	for i := range value {
		value[i] = char
	}
	return string(value)
}

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
