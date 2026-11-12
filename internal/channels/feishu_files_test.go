package channels

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

type filesServer struct {
	mu         sync.Mutex
	resources  []omaResource
	uploads    int
	attached   int
	downloaded []string
}

func (f *filesServer) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/files":
			f.mu.Lock()
			f.uploads++
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"file_up_1","filename":"photo.png","mime_type":"image/png","size_bytes":4}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/files/"):
			f.mu.Lock()
			f.downloaded = append(f.downloaded, r.URL.Path)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("artifact"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resources"):
			f.mu.Lock()
			f.attached++
			resource := omaResource{ID: "sres_1", Type: "file", FileID: "file_up_1", Filename: "photo.png", MountPath: "/mnt/user-data/files/file_up_1/photo.png"}
			f.resources = append(f.resources, resource)
			f.mu.Unlock()
			payload, _ := json.Marshal(resource)
			_, _ = w.Write(payload)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/resources"):
			f.mu.Lock()
			payload, _ := json.Marshal(map[string]any{"data": f.resources})
			f.mu.Unlock()
			_, _ = w.Write(payload)
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	})
}

func TestInboundAttachmentUploadsToFilesAndMountsResource(t *testing.T) {
	files := &filesServer{}
	server := httptest.NewServer(files.handler(t))
	t.Cleanup(server.Close)
	store := &fakeRuntimeStore{}
	worker, channel := testWorker(t, store, server.URL)
	channel.downloads = map[string][]byte{"img_key": []byte("\x89PNG\r\n\x1a\n")}
	msg := &channeltypes.NormalizedMessage{
		ChatID:    "oc_1",
		UserID:    "ou_1",
		Content:   "看下这张图",
		Resources: []channeltypes.Resource{{Type: "image", FileKey: "img_key", FileName: "photo.png"}},
	}
	content, skipped, mounted, err := worker.messageContent(context.Background(), "key", "sesn_1", msg, msg.Content)
	if err != nil {
		t.Fatalf("messageContent: %v", err)
	}
	if len(skipped) != 0 || files.uploads != 1 || files.attached != 1 {
		t.Fatalf("uploads=%d attached=%d skipped=%v", files.uploads, files.attached, skipped)
	}
	if _, ok := mounted["file_up_1"]; !ok {
		t.Fatalf("mounted = %v", mounted)
	}
	encoded, _ := json.Marshal(content)
	text := string(encoded)
	if strings.Contains(text, "base64") {
		t.Fatalf("入站附件不应再以 base64 内容块发送: %s", text)
	}
	if strings.Contains(text, "/mnt/user-data") {
		t.Fatalf("附件正文不应再拼接挂载路径: %s", text)
	}
	var blocks []map[string]any
	if err := json.Unmarshal([]byte(text), &blocks); err != nil || len(blocks) != 1 {
		t.Fatalf("正文已带文本时不应追加附件提示: %s", text)
	}
	onlyAttachment := &channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1", Resources: msg.Resources}
	content, _, _, err = worker.messageContent(context.Background(), "key", "sesn_1", onlyAttachment, "")
	if err != nil {
		t.Fatalf("messageContent: %v", err)
	}
	encoded, _ = json.Marshal(content)
	if !strings.Contains(string(encoded), "photo.png") || strings.Contains(string(encoded), "已发送附件") {
		t.Fatalf("纯附件消息只应带文件名: %s", encoded)
	}
}

func TestInboundAttachmentFailsLoudly(t *testing.T) {
	files := &filesServer{}
	server := httptest.NewServer(files.handler(t))
	t.Cleanup(server.Close)
	worker, channel := testWorker(t, &fakeRuntimeStore{}, server.URL)
	channel.downloads = map[string][]byte{"big": make([]byte, channelAttachmentBytes+1)}
	msg := &channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1", Resources: []channeltypes.Resource{{Type: "file", FileKey: "big", FileName: "big.zip"}}}
	if _, _, _, err := worker.messageContent(context.Background(), "key", "sesn_1", msg, ""); err == nil {
		t.Fatal("超大附件应返回错误")
	}
}

func attachFeishuUploader(t *testing.T, worker *channelWorker) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-apis/im/v1/files":
			_, _ = w.Write([]byte(`{"code":0,"data":{"file_key":"file_key_1"}}`))
		case "/open-apis/im/v1/images":
			_, _ = w.Write([]byte(`{"code":0,"data":{"image_key":"image_key_1"}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
		}
	}))
	t.Cleanup(server.Close)
	worker.client = lark.NewClient("app_id", "app_secret", lark.WithOpenBaseUrl(server.URL))
	return server
}

func TestOutboundArtifactsSendNewResourcesOnly(t *testing.T) {
	files := &filesServer{resources: []omaResource{
		{ID: "sres_old", Type: "file", FileID: "file_old", Filename: "old.txt"},
		{ID: "sres_new", Type: "file", FileID: "file_new", Filename: "report.txt", MimeType: "text/plain"},
	}}
	server := httptest.NewServer(files.handler(t))
	t.Cleanup(server.Close)
	worker, channel := testWorker(t, &fakeRuntimeStore{}, server.URL)
	attachFeishuUploader(t, worker)
	task := &channelReplyTask{ctx: context.Background(), sessionID: "sesn_1", scopeKey: "chat:oc_1", inbound: map[string]struct{}{}}
	msg := &channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1"}
	worker.sendArtifacts(context.Background(), task, msg, "key", artifactBaseline{"sres_old": {}, "sres_new": {}})
	if inputs := channel.sentInputs(); len(inputs) != 0 {
		t.Fatalf("历史资源不应重发: %d", len(inputs))
	}
	worker.sendArtifacts(context.Background(), task, msg, "key", artifactBaseline{"sres_old": {}})
	inputs := channel.sentInputs()
	if len(inputs) != 1 || inputs[0].FileKey != "file_key_1" {
		t.Fatalf("应发送 1 个产物: %+v", inputs)
	}
}

func TestOutboundSkipsInboundAttachments(t *testing.T) {
	files := &filesServer{resources: []omaResource{{ID: "sres_up", Type: "file", FileID: "file_up_1", Filename: "photo.png", MimeType: "image/png"}}}
	server := httptest.NewServer(files.handler(t))
	t.Cleanup(server.Close)
	worker, channel := testWorker(t, &fakeRuntimeStore{}, server.URL)
	task := &channelReplyTask{ctx: context.Background(), sessionID: "sesn_1", inbound: map[string]struct{}{"file_up_1": {}}}
	worker.sendArtifacts(context.Background(), task, &channeltypes.NormalizedMessage{ChatID: "oc_1"}, "key", artifactBaseline{})
	if inputs := channel.sentInputs(); len(inputs) != 0 {
		t.Fatalf("用户上传的附件不应回传: %d", len(inputs))
	}
}
