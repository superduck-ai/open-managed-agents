package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFridayImportConfiguration(t *testing.T) {
	for _, input := range []string{"{}", "client_id: synthetic-client", "client_secret: synthetic-secret", "client_id: [invalid", "client_id: synthetic-client\nclient_secret: synthetic-secret\nunknown: value"} {
		path := filepath.Join(t.TempDir(), "friday.yaml")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readFridayClient(path); err == nil || strings.Contains(err.Error(), "synthetic-") {
			t.Fatal("Friday 配置未安全拒绝")
		}
	}
	path := filepath.Join(t.TempDir(), "friday.yaml")
	if err := os.WriteFile(path, []byte("client_id: ' synthetic-client '\nclient_secret: ' synthetic-secret '\n"), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := readFridayClient(path)
	if err != nil || client.ClientID != "synthetic-client" || client.ClientSecret != "synthetic-secret" {
		t.Fatal("Friday 导入输入解析失败")
	}
}
