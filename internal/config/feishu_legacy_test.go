package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyFeishuConfigRequiresOfflineImport(t *testing.T) {
	for _, value := range []string{"{}", "null", "{client_id: private-client, client_secret: private-secret}"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("vault: {feishu_oauth_client: "+value+"}"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadYAMLConfig(path)
		if err == nil || !strings.Contains(err.Error(), "oauth-provider-migrate") || strings.Contains(err.Error(), "private-") {
			t.Fatalf("unexpected validation: %v", err)
		}
	}
}
