package feishu

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

func TestFeishuFileUploadDistinguishesRejectionFromUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
		rejected       bool
	}{
		{name: "业务明确拒绝", status: 200, response: `{"code":234001}`, rejected: true},
		{name: "请求明确拒绝", status: 429, response: `{"code":234001}`, rejected: true},
		{name: "服务端结果未知", status: 502, response: `{"code":234001}`},
		{name: "超时结果未知", status: 408, response: `{"code":234001}`},
		{name: "缺少文件回执", status: 200, response: `{"code":0,"data":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/open-apis/im/v1/files" {
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.response)
					return
				}
				_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"test-token","expire":7200}`)
			}))
			t.Cleanup(server.Close)
			adapter := &Adapter{Client: lark.NewClient("app_id", "app_secret", lark.WithOpenBaseUrl(server.URL))}
			key, err := adapter.uploadFile(t.Context(), "result.txt", []byte("result"))
			if err == nil || key != "" || errors.Is(err, contracts.ErrMediaUploadRejected) != tc.rejected {
				t.Fatalf("上传回执分类错误: key=%s err=%v", key, err)
			}
		})
	}
}
