// ============================================================================
// Wire types：DSR 协议的外部数据合同
// ============================================================================
//
// 这里的字段名大多保持 snake_case，因为它们直接对应服务器 JSON。
// 它们只在“边界”使用：client.ts 解析后，再映射到内部模型。
// 修改这些类型等于修改协议，必须和 Go 服务端同步。
//
// 阅读顺序建议：
//   Claim / Registration  —— worker 如何拿到并维持一个会话
//   Input / Delivery      —— 服务器如何投递输入、worker 如何回执
//   Permission / Decision —— 工具审批
//   Transcript / PublicEvent —— 持久化与对外事件
// ============================================================================

import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";

/**
 * 一次成功认领的身份凭证。
 *
 * @example
 *   {
 *     code_session_id: "cse_123",
 *     worker_epoch: "7",                    // 每次重新分配递增，用于 fencing
 *     session_token: "st_...",              // 调会话级接口的凭证
 *     model_token: "mt_...",                // 调模型代理的凭证
 *     claim_expires_at: "2026-11-04T12:00:30Z",
 *     trace_parent: "00-...-..."            // 可选，链路追踪
 *   }
 */
export interface Claim {
  code_session_id: string;
  worker_epoch: string;
  session_token: string;
  model_token: string;
  claim_expires_at: string;
  trace_parent?: string;
}

/**
 * register 的响应：协商协议版本与租约参数。
 *
 * @example
 *   {
 *     epoch: "7",                          // 必须与 claim.worker_epoch 一致
 *     protocol: "1",
 *     lease: { ttl_ms: 30000, grace_ms: 10000, heartbeat_interval_ms: 10000 },
 *     observability_enabled: true,
 *     trace_export: { content: false }
 *   }
 */
export interface RegistrationContext {
  model: string;
  models: Record<string, import("../assignment.ts").ModelConfig>;
  cwd: string;
  system_prompt?: string;
  custom_system_prompt?: string;
  append_system_prompt?: string;
  memory_catalog_prompt?: string;
  runtime_skills?: import("../assignment.ts").Assignment["launch"]["skills"];
  runtime_tools: string[];
  runtime_tool_permissions?: import("../runtime/contracts.ts").ToolPermissions;
  subagents?: import("../runtime/contracts.ts").SubagentConfig;
  mcp_config: unknown;
  subagent_contexts: Record<
    string,
    {
      mcp_config: unknown;
      runtime_tool_permissions?: import("../runtime/contracts.ts").ToolPermissions;
    }
  >;
}

export interface Registration {
  context: RegistrationContext;
  epoch: string;
  protocol: "1";
  lease: { ttl_ms: number; grace_ms: number; heartbeat_interval_ms: number };
  observability_enabled: boolean;
  trace_export?: { content: boolean };
}

/**
 * 一次输入的执行结论。
 *
 * 失败码含义：
 *   execution_failed     —— 模型/工具执行本身失败；
 *   worker_shutdown      —— worker 进程关闭导致中断；
 *   interrupted_recovery —— 恢复后发现上次未正常结束；
 *   quota_exhausted      —— 额度用尽。
 */
export type Finish =
  | { result: "success" | "cancelled" }
  | {
      result: "failed";
      failure_code:
        | "execution_failed"
        | "worker_shutdown"
        | "interrupted_recovery"
        | "quota_exhausted";
    };

/**
 * 工具身份。前面 tools.ts 的 dsrToolIdentity() 负责生成。
 *
 * @example
 *   {kind: "builtin", name: "bash"}
 *   {kind: "mcp", server: "sandbox", name: "read_file"}
 */
export type ToolIdentity =
  | { kind: "builtin" | "custom" | "delegation"; name: string }
  | { kind: "mcp"; name: string; server: string };

/**
 * 工具审批请求：worker 说“模型想执行这个工具，请用户决定”。
 *
 * @example
 *   {
 *     input_id: "in_42",
 *     turn: "3",
 *     dsh_session_id: "ses_root",
 *     tool_use_id: "call_abc",
 *     tool: {kind: "builtin", name: "bash"},
 *     input: {command: "rm -rf build"},
 *     available: true
 *   }
 */
export interface PermissionRequest {
  input_id: string;
  turn: string;
  dsh_session_id: string;
  tool_use_id: string;
  tool: ToolIdentity;
  input: Record<string, unknown>;
  available?: boolean;
}

/**
 * 外部执行结果：用户选择“由外部系统执行并直接返回结果”，
 * 而不是 allow/deny。内容以文本块形式塞回工具结果。
 */
export interface ExternalResult {
  content: { type: "text"; text: string }[];
  isError: boolean;
}

/**
 * 审批决定。
 *
 * allow           —— 允许执行，可附带修改后的参数；
 * deny            —— 拒绝，message 会展示给模型；
 * external_result —— 不执行本地工具，直接使用外部结果。
 *
 * @example
 *   {behavior: "allow", updated_input: {command: "ls -la"}}
 *   {behavior: "deny", message: "不允许删除目录"}
 */
export type Decision =
  | { behavior: "allow"; updated_input?: Record<string, unknown> }
  | { behavior: "deny"; message?: string }
  | { behavior: "external_result"; external_result: ExternalResult };

/**
 * 审批请求的当前状态。
 *
 * pending   —— 等待用户；
 * cancelled —— 已被取消；
 * closed    —— 已关闭（超时/会话结束等）；
 * resolved  —— 已决定，带 decision。
 */
export type Permission =
  | { request_id: string; state: "pending" | "cancelled" | "closed" }
  | { request_id: string; state: "resolved"; decision: Decision };

/**
 * 服务器推送给 worker 的输入。
 *
 * 两条车道：
 *   task    —— 真正执行业务的输入，目前只有 user.message；
 *   control —— 会话控制，必须尽快生效，不排在长任务后面。
 *
 * task 示例：
 *   {id: "in_1", lane: "task", kind: "user.message",
 *    content: [{type: "text", text: "帮我修复测试"}],
 *    session_thread_id: "ses_child",          // 可选：发给子线程
 *    oma_system_context: [{id: "ctx", text: "..."}],
 *    trace_context: {"traceparent": "..."}}
 *
 * control 示例：
 *   {id: "in_2", lane: "control", kind: "user.interrupt"}
 *   {id: "in_3", lane: "control", kind: "permission.resolved", request_id: "pr_1", resolution: {behavior: "allow"}}
 */
export type Input =
  | {
      id: string;
      lane: "task";
      kind: "user.message";
      content: unknown[];
      session_thread_id?: string;
      oma_system_context?: { id: string; text: string }[];
      trace_context?: Record<string, string>;
    }
  | {
      id: string;
      lane: "control";
      kind: "permission.resolved";
      request_id: string;
      resolution: Decision;
    }
  | {
      id: string;
      lane: "control";
      kind: "user.interrupt";
      session_thread_id?: string;
    };

/**
 * 输入投递状态回执。
 *
 * received  —— 已收到，准备处理（可重报，服务器去重）；
 * processing—— 正在处理；
 * processed —— 已完成；
 * error     —— 无法处理（unsupported_input）。
 *
 * @example
 *   {id: "in_1", status: "received"}
 *   {id: "in_1", status: "processed"}
 *   {id: "in_2", status: "processed", error: "unsupported_input"}
 */
export interface Delivery {
  id: string;
  status: "received" | "processing" | "processed";
  error?: "unsupported_input";
}

/**
 * 回执接口的响应。results 必须与请求里的 updates 一一对应。
 *
 * outcome：applied（本次生效）/ ignored（重复或已处理）/ scheduled（排队）。
 */
export interface DeliveryResult {
  applied: number;
  scheduled: number;
  ignored: number;
  results: {
    id: string;
    status: Delivery["status"];
    outcome: "applied" | "ignored" | "scheduled";
  }[];
}

export interface Transcript {
  dsh_session_id: string;
  parent_dsh_session_id: string | null;
  session_thread_id: string | null;
  next_offset: string;
}

export interface TranscriptPage {
  records: (SessionHeader | SessionEvent)[];
  next_from_offset: string | null;
  next_offset: string;
}

/**
 * 上报给服务器/前端的公共事件。
 *
 * 服务器依靠 id 去重，所以绝大多数事件使用随机 UUID；
 * 子线程 created 事件使用确定性 id（见 subagents.ts）。
 *
 * 主要类别：
 *   preview.*              流式预览（start/delta），可高频发送；
 *   agent.message          完整的 assistant 消息；
 *   agent.thinking         思考块；
 *   tool.result            工具结果（surfaceOp=append 时产生）；
 *   system.message         系统提示（例如审批参数已修改）；
 *   session.thread_*       子线程生命周期；
 *   agent.thread_*         子线程消息；
 *   session.error          会话级错误。
 *
 * @example
 *   {id: "uuid", type: "agent.message", content: [{type: "text", text: "done"}]}
 *   {id: "uuid", type: "tool.result", tool_use_id: "call_1",
 *    tool: {kind: "builtin", name: "bash"}, is_error: false, content: [...]}
 */
export type PublicEvent =
  | {
      id: string;
      type: "preview.start";
      event: { type: "agent.message" | "agent.thinking"; id: string };
    }
  | {
      id: string;
      type: "preview.delta";
      event_id: string;
      delta: Record<string, unknown>;
    }
  | {
      id: string;
      type: "agent.message";
      content: unknown[];
      model_request_start_id?: string;
      session_thread_id?: string;
    }
  | {
      id: string;
      type: "agent.thinking";
      model_request_start_id?: string;
      session_thread_id?: string;
    }
  | {
      id: string;
      type: "tool.result";
      tool_use_id: string;
      tool: ToolIdentity;
      is_error: boolean;
      content: unknown[];
      session_thread_id?: string;
    }
  | {
      id: string;
      type: "system.message";
      content: unknown[];
      session_thread_id?: string;
    }
  | {
      id: string;
      type:
        | "session.thread_created"
        | "session.thread_status_running"
        | "session.thread_status_idle"
        | "session.thread_status_terminated"
        | "agent.thread_message_sent"
        | "agent.thread_message_received"
        | "agent.thread_context_compacted"
        | "session.error";
      session_thread_id?: string;
      [key: string]: unknown;
    };
