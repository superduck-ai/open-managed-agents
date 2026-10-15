# OMA HTTP2 请求正文转发异常排查

排查时间：2026-10-15。环境：FAT，域名 `oma.test.seewo.com`。

## 现象

Sandbox 内能读取 `/mnt/user-data/outputs/helloworld.txt`，但 OMA 资源列表为空。文件仍在 rclone 缓存中，`Dirty: true`；上传接口 `/v1/filestore/fs/createFile` 重复返回 400，消息为 `Missing required multipart field: params`。

普通 JSON 请求也能复现：通过域名发送 HTTP/2 无 Content-Length 的流式请求时，正文解析失败；切换 HTTP/1.1 或补上正确长度后，正文能正常解析。直连 APISIX Pod `172.30.69.164:443` 时，HTTP/2 无长度请求也能正常解析。

## 复现方式

从能访问域名和 Pod IP 的机器执行，curl 需支持 HTTP/2。请求内容只有 `{}`，无需凭据，也不会发送邮件。

依次测试 HTTP/1.1 流式、HTTP/2 无长度、HTTP/2 显式长度，以及直连 APISIX 的 HTTP/2 无长度请求：

```bash
diagnosticUrl='https://oma.test.seewo.com/api/auth/send_magic_link'

printf '{}' | curl --noproxy '*' --http1.1 -sS --max-time 10 \
  -X POST -T - -H 'Content-Type: application/json' -H 'Expect:' \
  -H 'X-OMA-Diagnostic: ops-h1-stream' \
  -w '\nstatus=%{http_code} protocol=%{http_version} remote_ip=%{remote_ip}\n' "$diagnosticUrl"

printf '{}' | curl --noproxy '*' --http2 -sS --max-time 10 \
  -X POST -T - -H 'Content-Type: application/json' -H 'Expect:' \
  -H 'X-OMA-Diagnostic: ops-h2-no-length' \
  -w '\nstatus=%{http_code} protocol=%{http_version} remote_ip=%{remote_ip}\n' "$diagnosticUrl"

printf '{}' | curl --noproxy '*' --http2 -sS --max-time 10 \
  -X POST -T - -H 'Content-Type: application/json' -H 'Expect:' \
  -H 'Content-Length: 2' -H 'X-OMA-Diagnostic: ops-h2-with-length' \
  -w '\nstatus=%{http_code} protocol=%{http_version} remote_ip=%{remote_ip}\n' "$diagnosticUrl"

printf '{}' | curl --noproxy '*' --http2 -sS --max-time 10 \
  --resolve 'oma.test.seewo.com:443:172.30.69.164' \
  -X POST -T - -H 'Content-Type: application/json' -H 'Expect:' \
  -H 'X-OMA-Diagnostic: ops-apisix-direct' \
  -w '\nstatus=%{http_code} protocol=%{http_version} remote_ip=%{remote_ip}\n' "$diagnosticUrl"
```

保留 `-T -`，用于发送未知长度的正文；不要改成可能自动补长度的 `-d`。后三组需确认输出 `protocol=2`。

## 当前对照结果

| 请求方式        | 实际连接目标                   | 返回消息                      |
| --------------- | ------------------------------ | ----------------------------- |
| HTTP/1.1 流式   | 域名入口 `10.20.10.16:443`     | `Enter a valid email address` |
| HTTP/2 无长度   | 域名入口 `10.20.10.16:443`     | `Invalid email login request` |
| HTTP/2 显式长度 | 域名入口 `10.20.10.16:443`     | `Enter a valid email address` |
| HTTP/2 无长度   | APISIX Pod `172.30.69.164:443` | `Enter a valid email address` |

四组状态码都为 400，应比较消息：`Enter a valid email address` 表示 JSON 已解析，但邮箱为空；`Invalid email login request` 表示正文解析失败。上述两种入口的各组请求均重复测试 3 次；后端日志确认一组正常与失败样本到达同一个 oma-server Pod、同一个镜像版本。

2026-10-15 09:25（北京时间）在后端 Pod `172.30.64.249` 的网络命名空间抓包：失败探针到达 Pod 网卡时已是 HTTP/1.1，且 Content-Length、Transfer-Encoding 都缺失。抓包仅覆盖请求首包，没有覆盖完整 TCP 会话。

HTTP/2 转 HTTP/1.1 时，有正文的请求应使用正确的 Content-Length，或按 chunked 格式编码并设置 Transfer-Encoding。两者都没有时，HTTP/1.1 后端会按无正文处理，导致上传请求在进入文件写入逻辑前失败。

## 运维待查

1. 确认 `10.20.10.16` 的归属、对应虚拟服务，以及实际链路是否经过阿里云 WAF、F5。当前 DNS 为 `oma.test.seewo.com → gtm.test.seewo.com → 10.20.10.16`，尚未确认该 IP 是 F5 VIP。
2. 确认入口回源的 IP、端口、协议和 APISIX 实例名单。该 APISIX Pod 的直连路径正常，不代表所有 APISIX 实例都正常。
3. 对同一个 `X-OMA-Diagnostic` 请求比较入口及回源出口，定位首次出现“HTTP/1.1 有正文却无长度或分块编码”的节点。如果是 F5，检查 HTTP/2、HTTP profiles、request chunking 和相关 iRules；如经过 WAF 或 mesh，也检查相应转发环节。
