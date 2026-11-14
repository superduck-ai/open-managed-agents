package channels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type filesServer struct {
	mu         sync.Mutex
	resources  []omaResource
	uploads    int
	attached   int
	downloaded []string
	mountPaths []string
}

func (f *filesServer) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/files":
			upload, header, err := r.FormFile("file")
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			defer upload.Close()
			f.mu.Lock()
			f.uploads++
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(omaFile{ID: "file_up_1", Filename: header.Filename, MimeType: "image/png", Size: 4})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/files/") && strings.HasSuffix(r.URL.Path, "/content"):
			f.mu.Lock()
			f.downloaded = append(f.downloaded, r.URL.Path)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("artifact"))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/files/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/files/")
			metadata := omaFile{ID: id, Filename: "artifact.txt", MimeType: "text/plain", Size: int64(len("artifact"))}
			f.mu.Lock()
			for _, resource := range f.resources {
				if resource.FileID == id {
					metadata.Filename = resource.Filename
					metadata.MimeType = resource.MimeType
					break
				}
			}
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(metadata)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resources"):
			var input struct {
				MountPath string `json:"mount_path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.attached++
			f.mountPaths = append(f.mountPaths, input.MountPath)
			resource := omaResource{ID: "sres_1", Type: "file", FileID: "file_session_1", Filename: "photo.png", MountPath: "/mnt/session" + input.MountPath}
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
	worker, _ := testWorker(t, store, server.URL)
	stub := attachFeishuStub(t, worker)
	stub.attachment = []byte("%PDF-1.7\n附件验收")
	msg := &channeltypes.NormalizedMessage{
		MessageID: "om_1",
		ChatID:    "oc_1",
		UserID:    "ou_1",
		Content:   "看下这份文档",
		Resources: []channeltypes.Resource{{Type: "file", FileKey: "file_key", FileName: "document.pdf"}},
	}
	content, mounted, err := worker.messageContent(context.Background(), "key", "sesn_1", feishuMessage(msg), msg.Content, nil)
	if err != nil {
		t.Fatalf("messageContent: %v", err)
	}
	if files.uploads != 1 || files.attached != 1 {
		t.Fatalf("uploads=%d attached=%d", files.uploads, files.attached)
	}
	if _, ok := mounted["file_session_1"]; !ok {
		t.Fatalf("mounted = %v", mounted)
	}
	if len(files.mountPaths) != 1 || files.mountPaths[0] != "/uploads/file_up_1/document.pdf" {
		t.Fatalf("挂载必须保留文件名: %v", files.mountPaths)
	}
	worker.sendArtifacts(t.Context(), &channelReplyTask{sessionID: "sesn_1", inbound: mounted}, feishuMessage(msg), "key", artifactBaseline{})
	if len(files.downloaded) != 0 {
		t.Fatalf("挂载复制后的用户附件不应作为产物下载: %v", files.downloaded)
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
	if err := json.Unmarshal([]byte(text), &blocks); err != nil || len(blocks) != 2 || blocks[0]["text"] != "看下这份文档" || !strings.Contains(blocks[1]["text"].(string), "/mnt/session/uploads/file_up_1/document.pdf") {
		t.Fatalf("应保留原文并追加实际挂载路径: %s", text)
	}
	stub.mu.Lock()
	downloads := append([]string{}, stub.downloads...)
	stub.mu.Unlock()
	if len(downloads) != 1 || !strings.Contains(downloads[0], "/messages/om_1/resources/file_key") || !strings.Contains(downloads[0], "type=file") {
		t.Fatalf("附件应通过消息资源接口下载: %v", downloads)
	}
	onlyAttachment := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1", RawContentType: "file", Content: `<file key="file_key" name="document.pdf"/>`, Resources: msg.Resources}
	content, _, err = worker.messageContent(context.Background(), "key", "sesn_1", feishuMessage(onlyAttachment), onlyAttachment.Content, nil)
	if err != nil {
		t.Fatalf("messageContent: %v", err)
	}
	encoded, _ = json.Marshal(content)
	if len(content) != 1 || !strings.Contains(content[0]["text"].(string), "/mnt/session/uploads/file_up_1/document.pdf") {
		t.Fatalf("纯附件消息应包含本轮实际路径: %s", encoded)
	}
}

func TestInboundImageUsesNativeContent(t *testing.T) {
	files := &filesServer{}
	server := httptest.NewServer(files.handler(t))
	t.Cleanup(server.Close)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	stub := attachFeishuStub(t, worker)
	stub.attachment = []byte("\x89PNG\r\n\x1a\n")
	msg := &channeltypes.NormalizedMessage{MessageID: "om_image", RawContentType: "image", Content: "![image](image_key)", Resources: []channeltypes.Resource{{Type: "image", FileKey: "image_key", FileName: "vision.png"}}}
	content, mounted, err := worker.messageContent(t.Context(), "key", "sesn_1", feishuMessage(msg), msg.Content, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || content[0]["type"] != "image" || len(mounted) != 0 || files.uploads != 0 {
		t.Fatalf("图片必须作为原生内容交给模型，不能伪造文字或挂载: content=%v uploads=%d", content, files.uploads)
	}
	source := content[0]["source"].(map[string]any)
	if source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != base64.StdEncoding.EncodeToString(stub.attachment) {
		t.Fatalf("图片源不正确: %v", source)
	}
}

func TestInboundImagesRespectRequestBudget(t *testing.T) {
	files := &filesServer{}
	server := httptest.NewServer(files.handler(t))
	t.Cleanup(server.Close)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	stub := attachFeishuStub(t, worker)
	stub.attachment = make([]byte, 2<<20)
	copy(stub.attachment, "\x89PNG\r\n\x1a\n")
	msg := &channeltypes.NormalizedMessage{MessageID: "om_images", Resources: []channeltypes.Resource{
		{Type: "image", FileKey: "image_a", FileName: "a.png"},
		{Type: "image", FileKey: "image_b", FileName: "b.png"},
	}}
	content, mounted, err := worker.messageContent(t.Context(), "key", "sesn_1", feishuMessage(msg), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	images := 0
	for _, block := range content {
		if block["type"] == "image" {
			images++
		}
	}
	encoded, err := json.Marshal(map[string]any{"events": []map[string]any{{"type": "user.message", "content": content}}})
	if err != nil {
		t.Fatal(err)
	}
	if images != 1 || files.uploads != 1 || len(mounted) != 1 || len(encoded) >= 4<<20 {
		t.Fatalf("超出图片预算时必须回退文件，images=%d uploads=%d mounted=%v bytes=%d", images, files.uploads, mounted, len(encoded))
	}
}

func TestRichTextImagePreservesUserText(t *testing.T) {
	files := &filesServer{}
	server := httptest.NewServer(files.handler(t))
	t.Cleanup(server.Close)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	stub := attachFeishuStub(t, worker)
	stub.attachment = []byte("\x89PNG\r\n\x1a\n")
	msg := &channeltypes.NormalizedMessage{MessageID: "om_post", RawContentType: "post", Content: "请描述图片\n![image](image_key)\n保留这句原文", Resources: []channeltypes.Resource{{Type: "image", FileKey: "image_key"}}}
	content, _, err := worker.messageContent(t.Context(), "key", "sesn_1", feishuMessage(msg), msg.Content, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 2 || content[0]["text"] != "请描述图片\n\n保留这句原文" || content[1]["type"] != "image" {
		t.Fatalf("富文本必须保留用户文字，图片以原生块表示: %v", content)
	}
}

func TestReceivedRichTextImageReachesModel(t *testing.T) {
	for _, localized := range []bool{false, true} {
		t.Run(map[bool]string{false: "接收格式", true: "语言外层格式"}[localized], func(t *testing.T) {
			server, _, messages := omaTestServer(t, 0)
			worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
			stub := attachFeishuStub(t, worker)
			stub.attachment = []byte("\x89PNG\r\n\x1a\n")
			body := `{"title":"图片标题","content":[[{"tag":"text","text":"请描述图片"}],[{"tag":"img","image_key":"image_key"}]]}`
			if localized {
				body = `{"zh_cn":` + body + `}`
			}
			event := &larkim.P2MessageReceiveV1{Event: &larkim.P2MessageReceiveV1Data{
				Sender:  &larkim.EventSender{SenderId: &larkim.UserId{OpenId: new("ou_1")}},
				Message: &larkim.EventMessage{MessageId: new("om_post"), ChatId: new("oc_1"), ChatType: new("p2p"), MessageType: new("post"), Content: &body},
			}}
			if err := worker.dispatchMessage(t.Context(), normalize.ParseMessage(event)); err != nil {
				t.Fatal(err)
			}
			worker.adapter.(*feishuChannel).incoming.FlushAll(t.Context())
			deadline := time.After(3 * time.Second)
			for len(messages.snapshot()) == 0 {
				select {
				case <-deadline:
					t.Fatal("富文本未提交")
				case <-time.After(10 * time.Millisecond):
				}
			}
			got := messages.snapshot()[0]
			if !strings.Contains(got, "图片标题") || !strings.Contains(got, "请描述图片") || !strings.Contains(got, `"type":"image"`) || strings.Contains(got, "rich text message") {
				t.Fatalf("接收的富文本必须保留标题、正文及原生图片: %s", got)
			}
			worker.stopAllReplies()
		})
	}
}

func TestInboundAttachmentRespectsNativeFileLimit(t *testing.T) {
	for _, size := range []int{5, 4} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			files := &filesServer{}
			server := httptest.NewServer(files.handler(t))
			t.Cleanup(server.Close)
			worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
			worker.supervisor.fileMaxBytes = 4
			stub := attachFeishuStub(t, worker)
			stub.attachment = make([]byte, size)
			msg := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1", Resources: []channeltypes.Resource{{Type: "file", FileKey: "data", FileName: "data.bin"}}}
			_, _, err := worker.messageContent(t.Context(), "key", "sesn_1", feishuMessage(msg), "", nil)
			if (err != nil) != (size > 4) || size > 4 && files.uploads != 0 || size == 4 && files.uploads != 1 {
				t.Fatalf("飞书入站应沿用OMA原生文件上限: size=%d err=%v uploads=%d", size, err, files.uploads)
			}
		})
	}
}

type feishuStub struct {
	attachment []byte
	byMessage  map[string][]byte
	mu         sync.Mutex
	downloads  []string
}

func attachFeishuStub(t *testing.T, worker *channelWorker) *feishuStub {
	t.Helper()
	stub := &feishuStub{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/open-apis/im/v1/messages/") {
			w.Header().Set("Content-Type", "application/json")
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			fake := worker.adapter.(*feishuChannel).channel.(*fakeChannel)
			if err := (fakeStream{channel: fake}).UpdateCard(r.Context(), body.Content); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"code":0}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/open-apis/im/v1/messages/") {
			stub.mu.Lock()
			stub.downloads = append(stub.downloads, r.URL.Path+"?"+r.URL.RawQuery)
			payload := stub.attachment
			if stub.byMessage != nil {
				payload = stub.byMessage[r.URL.Path]
			}
			stub.mu.Unlock()
			if payload == nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(payload)
			return
		}
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
	worker.adapter.(*feishuChannel).client = lark.NewClient("app_id", "app_secret", lark.WithOpenBaseUrl(server.URL))
	return stub
}

func TestBurstAttachmentsKeepSourceMessage(t *testing.T) {
	oma, _, messages := omaTestServer(t, 0)
	files := &filesServer{}
	fileHandler := files.handler(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/files") || strings.HasSuffix(r.URL.Path, "/resources") {
			fileHandler.ServeHTTP(w, r)
			return
		}
		oma.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	stub := attachFeishuStub(t, worker)
	stub.byMessage = map[string][]byte{
		"/open-apis/im/v1/messages/om_first/resources/file_first":   []byte("第一份附件"),
		"/open-apis/im/v1/messages/om_second/resources/file_second": []byte("第二份附件"),
	}
	for _, id := range []string{"first", "second"} {
		if err := worker.dispatchMessage(t.Context(), &channeltypes.NormalizedMessage{
			MessageID: "om_" + id, ChatID: "oc_1", UserID: "ou_1",
			Resources: []channeltypes.Resource{{Type: "file", FileKey: "file_" + id, FileName: id + ".txt"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	worker.adapter.(*feishuChannel).incoming.FlushAll(t.Context())
	deadline := time.After(3 * time.Second)
	for len(messages.snapshot()) < 2 {
		select {
		case <-deadline:
			t.Fatalf("两份附件必须分别使用自己的 message_id 下载并提交，实际提交 %v", messages.snapshot())
		case <-time.After(10 * time.Millisecond):
		}
	}
	worker.stopAllReplies()
}

func TestOutboundArtifactsSendNewResourcesOnly(t *testing.T) {
	files := &filesServer{resources: []omaResource{
		{ID: "sres_old", Type: "file", FileID: "file_old", Filename: "old.txt", MountPath: "/mnt/user-data/outputs/old.txt"},
		{ID: "sres_new", Type: "file", FileID: "file_new", Filename: "report.txt", MimeType: "text/plain", MountPath: "/mnt/user-data/outputs/report.txt"},
	}}
	server := httptest.NewServer(files.handler(t))
	t.Cleanup(server.Close)
	worker, channel := testWorker(t, &fakeRuntimeStore{}, server.URL)
	attachFeishuStub(t, worker)
	task := &channelReplyTask{ctx: context.Background(), sessionID: "sesn_1", scopeKey: "chat:oc_1", inbound: map[string]struct{}{}}
	msg := &channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1"}
	worker.sendArtifacts(context.Background(), task, feishuMessage(msg), "key", artifactBaseline{"sres_old": "file_old:", "sres_new": "file_new:"})
	if inputs := channel.sentInputs(); len(inputs) != 0 {
		t.Fatalf("历史资源不应重发: %d", len(inputs))
	}
	worker.sendArtifacts(context.Background(), task, feishuMessage(msg), "key", artifactBaseline{"sres_old": "file_old:"})
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
	worker.sendArtifacts(context.Background(), task, feishuMessage(&channeltypes.NormalizedMessage{ChatID: "oc_1"}), "key", artifactBaseline{})
	if inputs := channel.sentInputs(); len(inputs) != 0 {
		t.Fatalf("用户上传的附件不应回传: %d", len(inputs))
	}
}

func TestOutboundArtifactsResendsUpdatedResource(t *testing.T) {
	files := &filesServer{resources: []omaResource{{ID: "sres_output", Type: "file", FileID: "file_old", Filename: "report.txt", MountPath: "/mnt/user-data/outputs/report.txt"}}}
	server := httptest.NewServer(files.handler(t))
	t.Cleanup(server.Close)
	worker, channel := testWorker(t, &fakeRuntimeStore{}, server.URL)
	attachFeishuStub(t, worker)
	task := &channelReplyTask{sessionID: "sesn_1"}
	msg := &channeltypes.NormalizedMessage{ChatID: "oc_1"}
	worker.sendArtifacts(t.Context(), task, feishuMessage(msg), "key", artifactBaseline{})
	baseline, err := worker.resourceBaseline(t.Context(), "key", "sesn_1")
	if err != nil {
		t.Fatal(err)
	}
	files.mu.Lock()
	files.resources[0].FileID = "file_updated"
	files.mu.Unlock()
	worker.sendArtifacts(t.Context(), task, feishuMessage(msg), "key", baseline)
	if inputs := channel.sentInputs(); len(inputs) != 2 {
		t.Fatalf("同一资源更新后应再次发送，实际 %d 次", len(inputs))
	}
	worker.sendArtifacts(t.Context(), task, feishuMessage(msg), "key", baseline)
	if inputs := channel.sentInputs(); len(inputs) != 2 {
		t.Fatalf("同一版本产物不应重复发送，实际 %d 次", len(inputs))
	}
}

func TestArtifactsWaitForLateResources(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls < 3 {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"sres_late","type":"file","file_id":"file_late"}]}`))
	}))
	t.Cleanup(server.Close)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	resources, err := worker.stableArtifactResources(t.Context(), "key", "sesn_1", time.Millisecond, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].ID != "sres_late" || calls != 5 {
		t.Fatalf("必须等迟到清单稳定后交付: calls=%d resources=%v", calls, resources)
	}
}

func TestArtifactsRequireNormalTurnCompletion(t *testing.T) {
	for _, tc := range []struct {
		name        string
		typeName    string
		stopReason  string
		wantFiles   int
		interrupted bool
	}{
		{name: "失败", typeName: "session.error"},
		{name: "中断", typeName: "session.status_idle", stopReason: "interrupted"},
		{name: "重试耗尽", typeName: "session.status_idle", stopReason: "retries_exhausted"},
		{name: "中断后上游返回正常终态", typeName: "session.status_idle", stopReason: "end_turn", interrupted: true},
		{name: "正常完成", typeName: "session.status_idle", stopReason: "end_turn", wantFiles: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := &filesServer{}
			fileHandler := files.handler(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/events") {
					if r.Method == http.MethodPost {
						files.mu.Lock()
						files.resources = []omaResource{{ID: "sres_output", Type: "file", FileID: "file_output", Filename: "partial.txt", MimeType: "text/plain", MountPath: "/mnt/user-data/outputs/partial.txt"}}
						files.mu.Unlock()
						_, _ = w.Write([]byte(`{"data":[{"id":"evt_user","type":"user.message"}]}`))
						return
					}
					terminal := omaEvent{ID: "evt_terminal", Type: tc.typeName}
					if tc.stopReason != "" {
						terminal.StopReason, _ = json.Marshal(map[string]string{"type": tc.stopReason})
					}
					events := []omaEvent{{ID: "evt_user", Type: "user.message"}}
					if tc.interrupted {
						events = append(events, omaEvent{ID: "evt_stop", Type: "user.interrupt"})
					}
					events = append(events, terminal)
					_ = json.NewEncoder(w).Encode(map[string]any{"data": events})
					return
				}
				fileHandler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			worker, _ := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, server.URL)
			worker.supervisor.subscribeEvents = testEventSubscriber(t, func() []omaEvent {
				terminal := omaEvent{ID: "evt_terminal", Type: tc.typeName}
				if tc.stopReason != "" {
					terminal.StopReason, _ = json.Marshal(tc.stopReason)
				}
				events := []omaEvent{{ID: "evt_user", Type: "user.message"}}
				if tc.interrupted {
					events = append(events, omaEvent{ID: "evt_stop", Type: "user.interrupt"})
				}
				return append(events, terminal)
			})
			attachFeishuStub(t, worker)
			task := worker.startReplyTask(t.Context(), "chat:oc_1", "sesn_1")
			worker.deliver(task, &channelMessage{ChatID: "oc_1", UserID: "ou_1", Content: "本轮输入"}, "key")
			files.mu.Lock()
			downloads := append([]string{}, files.downloaded...)
			files.mu.Unlock()
			if len(downloads) != tc.wantFiles {
				t.Fatalf("本轮的产物交付与终态不符: %v", downloads)
			}
		})
	}
}
