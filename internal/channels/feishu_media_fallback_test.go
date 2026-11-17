package channels

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

func TestFeishuImageFallbackRequiresConfirmedUploadRejection(t *testing.T) {
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jV6kAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, imageResponse, fileResponse, sendResponse string
		status, files, sends                            int
		wantError                                       bool
	}{
		{name: "明确拒绝后改发文件", imageResponse: `{"code":234001}`, files: 1, sends: 1},
		{name: "缺少应用权限不降级", imageResponse: `{"code":99991672}`, wantError: true},
		{name: "结构化权限错误不降级", imageResponse: `{"code":1,"error":{"permission_violations":[{"type":"action_scope_required","subject":"im:resource:upload"}]}}`, wantError: true},
		{name: "HTTP拒绝访问不降级", imageResponse: `{"code":1}`, status: 403, wantError: true},
		{name: "服务端结果未知不降级", imageResponse: `{"code":1}`, status: 502, wantError: true},
		{name: "上传超时不降级", imageResponse: `{"code":1}`, status: 408, wantError: true},
		{name: "缺少上传回执不降级", imageResponse: `{"code":0,"data":{}}`, wantError: true},
		{name: "发图片失败不降级", imageResponse: `{"code":0,"data":{"image_key":"image_key"}}`, sendResponse: `{"code":230001}`, sends: 1, wantError: true},
		{name: "文件降级也被拒绝", imageResponse: `{"code":234001}`, fileResponse: `{"code":234001}`, files: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var images, files, sends int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/open-apis/im/v1/images":
					images++
					if tc.status != 0 {
						w.WriteHeader(tc.status)
					}
					_, _ = io.WriteString(w, tc.imageResponse)
				case "/open-apis/im/v1/files":
					files++
					upload, _, err := r.FormFile("file")
					if err != nil {
						t.Error(err)
						return
					}
					defer upload.Close()
					data, err := io.ReadAll(upload)
					if err != nil || !bytes.Equal(data, image) || r.FormValue("file_name") != "result.png" {
						t.Errorf("降级必须保留原图片字节与文件名: %s %v", r.FormValue("file_name"), err)
					}
					if tc.fileResponse != "" {
						_, _ = io.WriteString(w, tc.fileResponse)
					} else {
						_, _ = io.WriteString(w, `{"code":0,"data":{"file_key":"file_key"}}`)
					}
				case "/open-apis/im/v1/messages/original/reply":
					sends++
					if tc.sendResponse != "" {
						_, _ = io.WriteString(w, tc.sendResponse)
					} else {
						_, _ = io.WriteString(w, `{"code":0,"data":{"message_id":"receipt"}}`)
					}
				default:
					_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"test-token","expire":7200}`)
				}
			}))
			t.Cleanup(server.Close)
			worker, _ := testWorker(t, &fakeRuntimeStore{}, "")
			worker.record.ReplyInThread = true
			worker.adapter.(*feishuChannel).Config.ReplyInThread = true
			adapter := worker.adapter.(*feishuChannel)
			adapter.Client = lark.NewClient("app_id", "app_secret", lark.WithOpenBaseUrl(server.URL))
			receipt, err := adapter.SendMedia(t.Context(), &channelMessage{ChatType: "group", ChatID: "chat", MessageID: "original"}, omaResource{Filename: "result.png", MimeType: "image/png"}, "image/png", image)
			if (err != nil) != tc.wantError || !tc.wantError && receipt != "receipt" {
				t.Fatalf("交付回执不符合预期: %s %v", receipt, err)
			}
			if (tc.status >= 500 || tc.status == http.StatusRequestTimeout) && errors.Is(err, errChannelMediaUploadRejected) {
				t.Fatal("未知结果不能标为明确拒绝并自动重试")
			}
			if images != 1 || files != tc.files || sends != tc.sends {
				t.Fatalf("图片=%d 文件=%d 消息=%d", images, files, sends)
			}
		})
	}
}
