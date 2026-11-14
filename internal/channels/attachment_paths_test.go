package channels

import (
	"encoding/json/v2"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

func TestAttachmentPathsUseNativeStableNamespace(t *testing.T) {
	files := &filesServer{}
	server := httptest.NewServer(files.handler(t))
	defer server.Close()
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	worker.supervisor.putAttachment = files.putAttachment
	worker.adapter = &attachmentBatchAdapter{channelAdapter: worker.adapter, data: []byte("a,b\n1,2")}
	msg := &channelMessage{MessageID: "message", Resources: []channelAttachment{{Type: "file", FileKey: "first", FileName: "data.csv"}, {Type: "file", FileKey: "second", FileName: "data.csv"}}}
	for range 2 {
		content, mounted, err := worker.messageContent(t.Context(), "key", "sesn_1", msg, "请对比两份同名文件", nil)
		encoded, _ := json.Marshal(content)
		if err != nil || len(content) != 2 || len(mounted) != 2 || files.writes != 2 {
			t.Fatalf("稳定路径未复用或同名文件覆盖: writes=%d err=%v", files.writes, err)
		}
		for _, path := range files.mountPaths {
			if !strings.HasPrefix(path, sandboxmount.MessageAttachmentsRoot+"/") || !strings.Contains(string(encoded), sandboxmount.SandboxPath(path)) || !strings.HasSuffix(path, "/data.csv") {
				t.Fatalf("本轮附件路径不正确: %s %s", path, encoded)
			}
		}
	}
	msg.MessageID = "next-message"
	if _, _, err := worker.messageContent(t.Context(), "key", "sesn_1", msg, "新一轮", nil); err != nil || files.writes != 4 {
		t.Fatal("不同消息不应共用附件路径", err)
	}
}
