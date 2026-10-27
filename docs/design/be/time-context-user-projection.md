# 时间上下文插件扩展

## 目标与证据

原始 CHAT-TXT-002 要求只回显 ASCII 标记，模型偶发将紧随用户输入的时间正文一起复制。此前内部摘要与公开SSE已确认额外内容来自模型输出。仅有 `source.kind=time-context` 不会让模型看到来源区别，时间内容需要自身携带边界。

早期请求编码阶段投影及其20次回归属于历史方案，见[原修复报告](/Users/arthur/GolandProjects/oma-verify/reports/chat-time-tags-fix-20261026.md)。当前方案改为插件组合，删除 `projectUserContent` 和 `src/prompt/time-context.ts`，不向system增加插件规则，不过滤公开输出，不修改原插件源码或依赖。

## 组合与执行顺序

`kernel.ts` 注册 `src/plugins/time-context.ts`，由扩展加载原生插件。扩展保留原配置schema和默认值，目前OMA仍传入 `timeZone=Asia/Shanghai`。

```mermaid
sequenceDiagram
    participant Loop as DSH agent loop
    participant OMA as OMA扩展中间件
    participant Native as 原生时间插件
    participant Log as DSH持久日志
    Loop->>OMA: agent/pre-step
    OMA->>Native: await next()
    Native-->>OMA: 原生decision与新增时间消息
    OMA->>OMA: 按source.kind和form包装正文与sections
    OMA-->>Loop: 返回新decision，保留其他字段
    Loop->>Log: 记录完整消息
    Loop->>Loop: 模型编码器原样转换content
```

原生插件自身以prepend注册。OMA在它加载完成后再以prepend注册，成为外层中间件，`await next()` 返回时原生消息已经生成。实际持久日志与模型请求测试验证这一顺序，不依赖业务代码直接调用渲染函数。

扩展仅转换本次decision中的新时间快照。role、id、source.kind、form及其他字段保留，`content`和`source.sections[].text`同步更新，不原地修改输入。当前原生插件生成一个文本块和一个同名section。step拒绝或signal取消时不转换。

## 模型可见格式

```text


<system-reminder>
Runtime time context, separate from the user's message. Use when relevant; exclude from text the user asks you to copy or transform.

Time sampled while preparing turn …, step …: …
Browser time zone for this request: …
Elapsed since the preceding model-visible message: …
</system-reminder>


```

时间消息仍是user；标签及前后换行属于该合成消息。Anthropic可以将相邻user合并，但原用户正文与完整时间提醒保持为两个文本块。OpenAI Chat Completions保持两个user消息。通用适配器不识别时间来源或标签。

## 生命周期与兼容范围

- 时间采样、浏览器时区选择、十分钟刷新、原生timeContext投影及其stateVersion均由原插件维护。
- 新快照携带完整边界入日志。恢复时原样使用，不重复包装；刷新只创建一个新快照，旧快照保持原样。
- 普通用户输入即使含时间模板或同名标签也不转换；来源判定只依据内部元数据。
- 没有数据库或历史日志迁移。切换前已持久化的裸时间快照保持原样，本次新会话回归不能证明旧裸快照会话的复制问题已解决。
- 不引入诊断开关、输出裁剪或系统提示词规则；也不复制、fork或patch原插件。

## 验证

本地测试覆盖真实插件组合与持久日志、content/sections一致、普通用户标签、Anthropic与OpenAI编码、重复请求、恢复与十分钟刷新、图片顺序，以及根/子Agent system不含插件说明。完整Brain测试、类型检查及仓库重复/复杂度检查在实现后统一执行。

真实回归使用新报告：原始CHAT-TXT-002固定nonce、新资源、kimi-k2.5、timeout=300s；补充文本、多轮、Bash和按运行时读取日期。历史结果不计为本版本通过，单次有限重复不保证真实模型未来绝不偶发错误。

```bash
./scripts/reproduce_chat_txt002.sh /Users/arthur/.codex/worktrees/d1f1/open-managed-agents 20
```

从oma-verify运行，需要已启动的d1f1平台及可读取的slot凭据，helper通过环境变量提供ANTHROPIC_API_KEY，不需要mock端口。suite为chat-compare，case为CHAT-TXT-002，使用新报告且不resume，遇失败即停。

## 2026-10-26 执行结果

全Brain单测622通过、5跳过，类型、重复与复杂度检查通过。新实现的原始用例20/20，相关六项6/6，完整证据、源码hash及兼容范围见[插件扩展回归报告](/Users/arthur/GolandProjects/oma-verify/reports/chat-time-plugin-extension-20261026.md)。
