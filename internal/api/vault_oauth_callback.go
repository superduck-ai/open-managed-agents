package api

import (
	"fmt"
	"net/http"
)

func writeVaultOAuthCallback(w http.ResponseWriter, channel string, payload []byte, failed bool) {
	title, message, icon := "授权成功", "窗口将在 3 秒后尝试关闭，也可手动关闭。", "M5 12l4 4L19 6"
	if failed {
		icon = "M7 7l10 10M17 7L7 17"
		title, message = "授权未完成", "授权已取消、链接已失效或操作无法继续，请返回原对话重新发起。"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
:root { color-scheme: light dark; font-family: Geist, system-ui, -apple-system, sans-serif; }
* { box-sizing: border-box; }
body { margin: 0;
  background: light-dark(#f9f9f8, #191918); color: light-dark(#21201c, #eeeeec); }
.viewport { min-height: 100vh; min-height: 100dvh; padding: 20px; display: grid; place-items: center; }
main { width: min(100%%, 360px); padding: 32px 20px; text-align: center;
  background: light-dark(#fdfdfc, #111110); border: 1px solid light-dark(#e2e1de, #353431);
  border-radius: 14px; box-shadow: 0 6px 24px rgb(0 0 0 / 0.05); }
svg { width: 48px; height: 48px; padding: 12px; border-radius: 50%%;
  color: light-dark(#218358, #71d083); background: light-dark(#e6f6eb, #132d21); }
main[data-failed="true"] svg { color: light-dark(#ce2c31, #ff9592); background: light-dark(#feebec, #3b181a); }
h1 { margin: 14px 0 6px; font-size: 20px; line-height: 1.4; font-weight: 600; }
p { margin: 0; color: light-dark(#63635e, #b5b3ad); font-size: 14px; line-height: 1.6; }
</style>
</head>
<body>
<div class="viewport">
<main data-failed="%t">
<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="%s"/></svg>
<h1>%s</h1><p>%s</p>
</main>
</div>
<script>
const payload = %s;
try {
  const channel = new BroadcastChannel(%q);
  channel.postMessage(payload);
  channel.close();
} catch (error) {}
if (%t) setTimeout(() => window.close(), 3000);
if (window.opener) {
  window.opener.postMessage(payload, window.location.origin);
}
</script>
</body>
</html>`, title, failed, icon, title, message, payload, channel, !failed)
}
