// ============================================================================
// 工具身份映射：把运行时工具名翻译成 DSR 协议里的 ToolIdentity
// ============================================================================
//
// 工具在本地运行时用“扁平名字”标识，例如：
//   Bash、Read、mcp__sandbox__fetch、subagent、AskUserQuestion
//
// 但上报到服务器（审批请求、tool.result 公共事件）时，需要结构化身份：
//
//   type ToolIdentity =
//     | {kind: "builtin";    name: string}
//     | {kind: "custom";     name: string}
//     | {kind: "delegation"; name: string}
//     | {kind: "mcp";        name: string; server: string}
//
// 这个文件只负责这层纯函数映射，不访问网络、不抛业务错误。
// 服务器和前端据此知道“这是哪个工具”，从而展示正确的图标、审批文案和权限策略。
//
// 映射示例：
//   "Bash"                    -> {kind: "builtin",    name: "bash"}
//   "MultiEdit"               -> {kind: "builtin",    name: "edit"}          // 归一化成 edit
//   "mcp__sandbox__read_file" -> {kind: "mcp",        server: "sandbox", name: "read_file"}
//   "subagent"                -> {kind: "delegation", name: "subagent"}
//   "AskUserQuestion"         -> {kind: "custom",     name: "ask_user_question"}
//   "SomeNewBuiltin"          -> {kind: "builtin",    name: "SomeNewBuiltin"} // 未登记时原样保留
// ============================================================================

import type { ToolIdentity } from "./wire.ts";

// 本地工具名 -> 对外规范名。只登记“名字不一致”的内置工具：
//   - MultiEdit 和 Edit 在协议里都算 edit；
//   - WebSearch / WebFetch 使用 snake_case 的对外名称。
// 未登记的内置工具（例如未来新增的）会走最后的 fallback，保持原名。
const builtins: Record<string, string> = { Bash: "bash", Read: "read", Write: "write", Edit: "edit", MultiEdit: "edit", Glob: "glob", Grep: "grep", WebSearch: "web_search", WebFetch: "web_fetch" };

/**
 * 把运行时工具名转换成协议身份。
 *
 * MCP 工具约定使用 `mcp__<server>__<tool>` 命名：
 *   正则里的 `(.+?)` 是非贪婪匹配，所以服务器名取到第一个 `__` 为止，
 *   后面的部分全部算工具名，允许工具名自身包含双下划线。
 *   例："mcp__srv__a__b" -> {kind: "mcp", server: "srv", name: "a__b"}
 */
export function dsrToolIdentity(name: string): ToolIdentity {
  const match = /^mcp__(.+?)__(.+)$/.exec(name);
  if (match) return { kind: "mcp", server: match[1]!, name: match[2]! };
  // 子 agent 相关工具属于“委派”类别，前端会用不同样式展示。
  if (["list_agents", "subagent", "send_message", "interrupt_agent"].includes(name)) return { kind: "delegation", name };
  // 向用户提问是产品自定义能力，归一化成 snake_case 的 ask_user_question。
  if (name === "AskUserQuestion") return { kind: "custom", name: "ask_user_question" };
  // 其余都按内置工具处理；已登记的名称做归一化，未登记的保留原名。
  return { kind: "builtin", name: builtins[name] ?? name };
}
