package channels

import (
	"bytes"
	"encoding/json/v2"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSharedChannelCodeDoesNotImportPlatformImplementation(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(path, "github.com/larksuite/") || path == "github.com/superduck-ai/open-managed-agents/internal/channels/feishu" {
				t.Errorf("公共渠道文件 %s 依赖飞书实现：%s", name, path)
			}
		}
	}
}

func TestAdapterMessageKeepsRuntimeInputOutOfPayload(t *testing.T) {
	message := channelMessage{MessageID: "message", Content: "正文", Input: &db.ChannelInput{PreparedContent: "仅限运行时的交接记录"}}
	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("仅限运行时的交接记录")) || bytes.Contains(payload, []byte(`"Input"`)) {
		t.Fatal("跨包导出不得将内部交接记录写入消息载荷")
	}
}
