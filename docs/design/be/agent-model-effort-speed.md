# Agent model 对象支持 effort 与 speed 透传

> Issue: #250
> 日期: 2026-08-16
> 分支: `feat/agent-model-effort-speed`

## 背景

官方 Claude Managed Agents 的 agent 定义中，`model` 字段除模型 ID 字符串外，还支持对象形式 `{"id": "...", "speed": "fast", "effort": "high"}`：
- `effort`：模型推理强度，接受字符串（`low` / `medium` / `high` / `xhigh` / `max`）或对象（`{"type": "high"}`）
- `speed`：推理速度（`standard` / `fast`）
- `inference_geo`：推理地域（`us` / `global`）——**本项目不做**（开源版不做地域限制，有意设计）

## 问题

OMA 目前对这两个字段处理不当：

1. **`effort` 静默丢弃**：`internal/agents/handler.go:725-728` 的 `agentModelInput` 只有 `ID`/`Speed`，`json.Unmarshal` 忽略未知字段，用户传 `effort: "high"` **不报错也不生效**
2. **`speed` 只回显不生效**：声明时通过校验（`:766-768`）、响应回显，但**从不下发到运行时**——`internal/environments/environment_manager.go:210-217` 的 `modelIDFromAgentSnapshot` 只取 `model.id`

这属于「API 收下了但实际不生效」的静默失效。

## 方案

### 1. `normalizeModel` 支持 `effort`

`internal/agents/handler.go`：
- `agentModelInput` 增加 `Effort json.RawMessage`（兼容字符串和对象两种形式）
- 归一化为规范形式：`{"type": "high"}`（对象）或 `"high"`（字符串）→ 统一输出 `{"type": "high"}` 结构（对齐官方响应回显格式）
- 校验取值集合：`low` / `medium` / `high` / `xhigh` / `max`，非法值报错（不再静默丢弃）
- `normalizedAgentModel` 增加 `Effort json.RawMessage` 字段

### 2. `speed` 传递到运行时

`internal/environments/environment_manager.go`：
- `modelIDFromAgentSnapshot` 扩展为读取完整 model 对象（id + speed + effort）
- 在 e2b 运行时 / 平台代理的模型请求中传递 `speed`（fast 模式）

> 注：`speed`/`effort` 的实际执行在 Claude Code worker 侧（sandbox 内），OMA host 侧负责**配置下发**——把 model 对象完整传给 environment-manager payload，由 worker 消费。

### 3. 响应回填

- 省略 `effort` 时响应回填 `null`（或省略字段），省略 `speed` 回填 `"standard"`（既有行为）

## 测试

- `normalizeModel`：字符串形式、对象形式（effort 字符串/对象）、非法 effort 值报错、speed 合法/非法
- `modelIDFromAgentSnapshot`：完整 model 对象 → id + speed + effort 传递
- 回归：现有 agent create/update 测试

## 验收

- 官方 SDK 创建带 `effort` / `speed` 的 agent，字段真实生效（非静默丢弃）
- 非法 `effort` 值返回明确错误
- environment-manager payload 携带完整 model 对象（id + speed + effort）
- 响应回显与官方格式一致
