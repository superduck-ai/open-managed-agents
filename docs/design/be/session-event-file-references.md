# Session 事件文件引用

## 数据边界

`POST /v1/sessions/{session_id}/events` 与 Session 创建时的 `initial_events`
支持在 `user.message.content` 中引用 Files API 文件：

```json
{
  "type": "document",
  "source": {
    "type": "file",
    "file_id": "file_abc123"
  }
}
```

`image` 使用相同结构。最终数据边界固定为：

```text
session_events.payload
  = 用户可见的原始消息和 file_id

code_session_inbound_events.payload
  = worker 专用消息，包含 @"/mnt/session/uploads/..."
```

Session Event 不保存绝对挂载路径，不增加第二份 payload 字段或数据库 migration。事件列表、SSE、
Webhook、前端展示与审计始终读取公开 `session_events.payload`；Code Session inbound 是唯一保存
worker 转换结果的持久化边界。

## 写入校验

文件必须先通过 Session 创建请求的 `resources` 或 `sessions.resources.add` 挂载到当前
Session。Events API 不隐式创建 Resource、不读取对象内容，也不接受客户端提交的本地绝对路径。

Send Events 在事务中锁定 Session 行并按当前 workspace/session 查询活动 File Resource，然后：

1. 从公开事件中提取 `document/image + source.type=file` 的 `file_id`；
2. 要求每个 `file_id` 都有活动 Session Resource；
3. 校验 `image` 的 MIME 为 `image/jpeg`、`image/png`、`image/gif` 或 `image/webp`；
4. `file_id` 缺失或去除首尾空白后为空时拒绝；其他值保留原文，并与 Resource ID 精确匹配；
5. 同一文件重复引用只影响一次 worker 路径注入；
6. 任一引用无效时回滚整批事件及 outcome evaluation 更新；
7. 在同一事务内根据绑定生成去除 file-source block、增加 `@"..."` 引用的 worker 内容；只有全部
   事件都准备成功才写入原始公开 payload，任一输入或内部转换错误都会回滚整批；
8. 提交后 active Code Session 只为已准备内容补充 worker envelope 并写 inbound，不再重新解析
   `file_id` 或挂载路径。

资源查询按 `created_at ASC, uuid ASC` 稳定排序；同一个 Files API 对象多次挂载时，worker 转换
选择最早的活动绑定。跨 workspace、跨 Session、已删除或尚未挂载的文件都表现为当前 Session
中无绑定，并返回明确的 400，提示先调用 Session Resources API。

一旦公开 Session Event 引用了某个 File Resource，该 Resource 在 Session 生命周期内不可单独删除，
删除请求返回 409。事件写入与 Resource 删除都锁定同一条 Session 行，因此不存在“事件已通过校验，
Resource 随即被删除，worker 转换时再也找不到挂载”的竞态。Files API 文件本身也会继续受到既有
活动 Resource 引用保护。过期 Resource 不属于活动绑定，不能用于接受新的文件引用。

Session 创建请求先规范化本次 `resources`，再用同一规则校验 `initial_events`，最后在一个事务
内写入 Session、Resource、公开初始事件和 Environment Work。初始事件支持 `user.message`、
`user.define_outcome`，以及符合既有顺序约束的末尾 `system.message`。创建边界只校验引用，不生成
随后会丢弃的 worker payload；Code Session activation 从已提交的公开历史统一执行实际转换。

Deployment 创建仍保存公开的 `initial_events` 模板；每次运行在物化 Session Resource 后，用本次
运行解析出的 File 绑定校验初始事件。引用未出现在 Deployment `resources` 中、文件已删除或绑定
无法物化时，本次 Deployment Run 以 `session_resource_not_found_error` 失败，不创建 Session。

## Worker 转换

公开事件提交后存在两个 Code Session 转换入口：

- Session 已 active：Session 写入事务使用绑定快照准备 worker 内容，`QueuePublicSessionEvents`
  只补充 Code Session envelope 并写 realtime inbound；
- Session 尚在启动：activation 持有 Session 行锁，读取完整公开历史和活动 File Resource，按顺序
  生成 inbound 后再把 Code Session 切为 active。

两个入口都遵循相同的内容转换规则：保留原始文本和非 file-source content block，移除公开的
file-source block，并在消息末尾增加去重后的 Claude Code 原生引用。realtime 在 Session 写事务内
执行该转换；activation 从持久化公开历史和活动绑定重新执行转换：

转换器在边界一次解码事件 envelope、content block 与 file source，并复用同一份解析结果完成引用
解析和 worker 内容生成；公开 payload 仍保持原始字节语义，不承载内部挂载路径。

```text
用户原始文本

@"/mnt/session/uploads/reference document.pdf"
@"/mnt/session/uploads/chart.png"
```

Filestore `/uploads/...` 通过统一路径合同映射到 Sandbox `/mnt/session/uploads/...`。空格由双引号
保护，反斜杠与双引号按 Claude Code quoted `@` mention 形式转义；路径控制字符仍由 Resource
写入边界拒绝。

```mermaid
sequenceDiagram
    participant C as Client
    participant S as Sessions API
    participant DB as PostgreSQL
    participant Q as Code Session inbound
    participant W as Worker

    C->>S: events.send(file_id blocks)
    S->>DB: lock Session + resolve active Resources
    S->>S: validate references + prepare worker content
    S->>DB: insert original session_events.payload
    S-->>C: original public event
    alt Code Session active
        S->>Q: add worker envelope + persist prepared payload
    else Code Session initializing
        S->>S: realtime path skips
        S->>DB: activation reads public history + mounts
        S->>Q: persist transformed startup payloads
    end
    Q->>W: deliver/replay persisted inbound payload
```

## 重试、重连与展示保护

转换后的 worker payload 继续写入既有 Code Session inbound event。投递重试、断线恢复、ACK 与
幂等重放直接使用这条已转换记录，不会重新读取或改写 Session Event，也不会把绝对路径投影回
公开 API。Realtime 内容准备与公开事件写入处于同一事务，输入错误返回 400，路径转换、ID 生成、
存量状态解析或序列化等内部错误返回安全的 500；两类错误都会回滚整批，不会留下只有公开事件而
没有已准备 worker 内容的提交。事务提交后的失败仅可能来自 Code Session 查询、envelope 生成或
inbound 持久化，并由 Session handler 在最终处理边界记录一次。

Session 标题只由 Session 创建或更新接口的 `title` 控制。worker 消息转换不调用标题生成逻辑，
也不修改 Session metadata、公开事件或前端展示内容。

运行中新增文件的顺序固定为：

```text
Files API 上传
→ Session Resources API 挂载
→ events.send(file_id)
→ 服务端解析当前挂载路径
→ Code Session inbound 持久化 @"绝对路径"
```

Resource 成功新增后，现有 Sandbox 最迟在固定 `1s` metadata cache 刷新后看到目录变化；Events
API 不为缓存刷新增加人为延迟。

## 验收

- 普通文本消息的 public/worker 转换保持原行为；
- 单文件、多文件、重复文件、空格、反斜杠和双引号路径正确转换；
- 未挂载、跨租户、已删除、MIME 不匹配与客户端本地路径均被拒绝；
- 对话中 `resources.add` 后发送的文件进入 realtime inbound；
- activation 前的文件事件从公开历史转换并按顺序进入 inbound；
- 已引用文件的 Session Resource 不能被单独删除，activation/retry 始终可重新解析相同挂载；
- Deployment initial event 只能引用同一 Deployment `resources` 中物化的文件；
- worker 重连和重试重放既有 inbound，不重复转换；
- API、事件列表、SSE、Webhook、标题与前端展示不出现 `/mnt/session/uploads`。
