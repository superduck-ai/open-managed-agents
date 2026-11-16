package channels

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChannelMounts(t *testing.T) {
	for _, tc := range []struct {
		name, workspace, body string
		valid                 bool
	}{
		{"默认不挂载", "workspace-a", `{}`, true},
		{"清除挂载", "workspace-a", `{"memory_store_id":null,"vault_id":null}`, true},
		{"有效挂载", "workspace-a", `{"memory_store_id":"mem-a","vault_id":"vault-a"}`, true},
		{"跨工作区", "workspace-b", `{"memory_store_id":"mem-a"}`, false},
		{"已归档记忆", "workspace-a", `{"memory_store_id":"mem-archived"}`, false},
		{"已归档凭证库", "workspace-a", `{"vault_id":"vault-archived"}`, false},
		{"无效类型", "workspace-a", `{"memory_store_id":123}`, false},
		{"不存在凭证库", "workspace-a", `{"vault_id":"missing"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			h := handler{store: scopedStore{}}
			err := h.validateMounts(httptest.NewRecorder(), request, tc.workspace)
			if (err == nil) != tc.valid {
				t.Fatalf("挂载校验结果 = %v", err)
			}
			body, _ := io.ReadAll(request.Body)
			if string(body) != tc.body {
				t.Fatal("转发正文被改变")
			}
		})
	}
}
