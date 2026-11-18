package channels

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOMAResponseLargerThanFourMiB(t *testing.T) {
	text := strings.Repeat("x", 5<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.MarshalWrite(w, struct {
			Content string `json:"content"`
		}{text})
	}))
	defer server.Close()
	var response struct {
		Content string `json:"content"`
	}
	if err := newOMAClient(server.URL).request(t.Context(), "key", http.MethodGet, "/events", nil, &response); err != nil || response.Content != text {
		t.Fatalf("OMA合法响应不能被渠道截断: length=%d err=%v", len(response.Content), err)
	}
}

func TestOMAInputHistoryUsesNativeTypeFilterAcrossPages(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("types[]") != "user.message" {
			t.Error("入站归属检查不应读取工具和模型历史")
		}
		if calls == 1 {
			if r.URL.Query().Get("page") != "anchor" {
				t.Error("必须使用原生起始位置")
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"first","type":"user.message"}],"next_page":"second"}`))
			return
		}
		if r.URL.Query().Get("page") != "second" {
			t.Error("丢失原生分页位置")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"last","type":"user.message"}]}`))
	}))
	defer server.Close()
	events, err := newOMAClient(server.URL).listEventPages(t.Context(), "key", "session", url.Values{"types[]": {"user.message"}, "page": {"anchor"}})
	if err != nil || len(events) != 2 || calls != 2 {
		t.Fatalf("原生输入过滤分页失败: events=%d calls=%d err=%v", len(events), calls, err)
	}
}
