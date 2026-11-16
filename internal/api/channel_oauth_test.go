package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestChannelOAuthBrowserBinding(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/oauth/vault/env-success", nil)
	continuation := &db.OAuthContinuation{BrowserHash: channelOAuthBrowserHash("original-browser")}
	if validChannelOAuthBrowser(request, continuation) {
		t.Fatal("缺少浏览器绑定时不得接受渠道回调")
	}
	request.AddCookie(&http.Cookie{Name: "oma_channel_oauth", Value: "another-browser"})
	if validChannelOAuthBrowser(request, continuation) {
		t.Fatal("其他浏览器不得接管原授权")
	}
	request.Header.Del("Cookie")
	request.AddCookie(&http.Cookie{Name: "oma_channel_oauth", Value: "original-browser"})
	if !validChannelOAuthBrowser(request, continuation) || !validChannelOAuthBrowser(request, nil) {
		t.Fatal("原浏览器或原生控制台回调被拒绝")
	}
}

func TestVaultOAuthCallbackKeepsFailureVisible(t *testing.T) {
	for _, test := range []struct {
		name   string
		failed bool
		title  string
	}{
		{"授权失败", true, "授权未完成"},
		{"授权成功", false, "授权成功"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeVaultOAuthCallback(response, "env-oauth", []byte(`{"type":"vault_oauth_complete"}`), test.failed)
			body := response.Body.String()
			if !strings.Contains(body, "<h1>"+test.title+"</h1>") || !strings.Contains(body, "if (window.opener)") || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("回调页面必须提供可见结果，并保留控制台通知")
			}
			if test.failed && !strings.Contains(body, "if (false) setTimeout") {
				t.Fatal("失败回调不得自动关闭")
			}
		})
	}
}
