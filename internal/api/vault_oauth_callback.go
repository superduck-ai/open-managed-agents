package api

import (
	"fmt"
	"net/http"
)

func writeVaultOAuthCallback(w http.ResponseWriter, channel string, payload []byte, failed bool) {
	title, message := "授权成功", "请返回原对话或控制台查看结果。"
	if failed {
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
</head>
<body>
<main><h1>%s</h1><p>%s</p></main>
<script>
const payload = %s;
try {
  const channel = new BroadcastChannel(%q);
  channel.postMessage(payload);
  channel.close();
} catch (error) {}
if (window.opener) {
  window.opener.postMessage(payload, window.location.origin);
  if (%t) setTimeout(() => window.close(), 250);
}
</script>
</body>
</html>`, title, title, message, payload, channel, !failed)
}
