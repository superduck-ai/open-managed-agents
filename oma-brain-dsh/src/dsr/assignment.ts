// ============================================================================
// Assignment：把 register 返回的上下文组装成本地运行时配置
// ============================================================================
//
// 协议变化后，register 不再只返回租约参数，还会一次性返回整份会话上下文：
//
//   POST /register
//   -> {
//        epoch: "7",
//        protocol: "1",
//        lease: { ttl_ms, grace_ms, heartbeat_interval_ms },
//        observability_enabled: true,
//        trace_export: { content: false },
//        context: {                                  // 新增：随注册冻结的上下文
//          model: "claude-sonnet-5-5",
//          models: { ... },
//          cwd: "/workspace/repo",
//          system_prompt: "You are ...",
//          append_system_prompt: "...",
//          runtime_tools: ["Bash", "Read", "Write"],
//          runtime_tool_permissions: { ... },
//          subagents: { agents: [...] },
//          mcp_config: { mcpServers: { ... } },      // 根会话的外部 MCP
//          subagent_contexts: {                      // 每个子 agent 的冻结配置
//            researcher: { mcp_config: {...}, runtime_tool_permissions: {...} }
//          }
//        }
//      }
//
// 因此 dsrAssignment 的职责从“拉上下文 + 映射”简化为“映射”：
//
//   claim + registration
//            │
//            ▼
//      dsrAssignment()
//            │
//            ▼
//   Assignment { session_id, launch, mcp_config, subagent_contexts, ... }
//            │
//            ├─> omaRuntimeConfig()       -> 模型/工具/提示词/子线程配置
//            └─> api.assignments          -> 模型代理、沙箱按会话查询
//
// 为什么要“冻结”上下文：会话可能持续很久，期间平台侧配置可能变化。
// register 返回的这份快照是本次执行周期的权威配置；子 agent 的 MCP 配置
// 也按名字冻结下来，避免运行中途不同线程读到不一致的设置。
//
// 注意：这些字段已在 DsrClient.registerOnce() 中做过运行时校验
// （model 非空、cwd 以 / 开头、runtime_tools 是字符串数组、
//   models/mcp_config/subagent_contexts 是对象、权限结构合法），
// 所以下面的类型断言只负责把外部 DTO 映射到内部 Assignment 结构。
// ============================================================================

import type {OmaApi} from "../api.ts";
import {validateAssignment, type Assignment} from "../assignment.ts";
import type {Claim, Registration} from "./wire.ts";

/**
 * 组装 Assignment 并做一次结构校验。
 *
 * 字段来源分两块：
 *   - claim：session_id / worker_epoch / ingress_token / model_token / trace_parent；
 *   - registration：可观测性开关、trace 导出配置，以及核心的 context 快照。
 *
 * @example
 *   const registration = await client.register();
 *   const assignment = dsrAssignment(api, client.claim, registration);
 *   api.assignments.set(assignment.session_id, assignment);
 *
 *   // 之后工具执行时可以通过 api.assignment(sessionId) 取回这份配置。
 */
export function dsrAssignment(api: OmaApi, claim: Claim, registration: Registration): Assignment {
    // register 响应里的 context 是本次执行周期的冻结快照。
    const context = registration.context;
    const assignment: Assignment = {
        // 身份与凭证：session_token 以 ingress_token 的名字进入运行时，
        // 供模型代理、沙箱、MCP 等下游使用。
        session_id: claim.code_session_id,
        worker_epoch: claim.worker_epoch,
        ingress_token: claim.session_token,
        model_token: claim.model_token,
        // 可观测性配置来自 register；registered_at 是本地时间，
        // 用于判断这份 assignment 的新鲜度（不是服务器时间）。
        observability_enabled: registration.observability_enabled,
        registered_at: Date.now(),
        trace_parent: claim.trace_parent,
        trace_export: registration.trace_export,
        // 根会话（非 inline 子 agent）使用的外部 MCP server 列表。
        external_mcp_config: context.mcp_config,
        // 每个具名子 agent 的冻结上下文，key 是子 agent 名字。
        // omaRuntimeConfig 创建子线程时按 thread.name 查这里：
        //   - 查不到且不是 inline -> 抛 "Missing frozen subagent context"；
        //   - inline 子 agent 没有预置配置，回退到根会话的配置。
        // 例：{researcher: {mcp_config: {...}, runtime_tool_permissions: {...}}}
        subagent_contexts: context.subagent_contexts,
        launch: {
            // version: 2 是当前 Assignment 的 schema 版本，运行时据此选择解析逻辑。
            version: 2,
            model: context.model as string,
            models: context.models as Assignment["launch"]["models"],
            work_dir: context.cwd as string,
            // 系统提示词有三层来源，优先级从高到低：
            //   system_prompt > custom_system_prompt > 空字符串。
            system: (context.system_prompt ?? context.custom_system_prompt ?? "") as string,
            // append_system_prompt 是追加到末尾的环境提示词（例如时间、平台说明）。
            environment_prompt: (context.append_system_prompt ?? "") as string,
            memory_catalog_prompt: (context.memory_catalog_prompt ?? "") as string,
            // 其余可选配置缺省时用安全默认值：数组用 []，对象用 undefined。
            skills: (context.runtime_skills ?? []) as Assignment["launch"]["skills"],
            subagents: (context.subagents ?? undefined) as Assignment["launch"]["subagents"],
            tools: (context.runtime_tools ?? []) as string[],
            tool_permissions: (context.runtime_tool_permissions ?? undefined) as Assignment["launch"]["tool_permissions"],
        },
        // 内置 sandbox MCP：地址指向本会话专属端点，并携带 session_token。
        // 运行时里的沙箱工具通过它执行文件系统/命令操作。
        mcp_config: {
            mcpServers: {
                sandbox: {
                    type: "http",
                    url: `${api.base}/v1/dsr/sessions/${encodeURIComponent(claim.code_session_id)}/sandbox/mcp`,
                    headers: {Authorization: `Bearer ${claim.session_token}`, "DSR-Protocol": "1"}
                }
            }
        },
    };
    // 校验 session_id / worker_epoch / sandbox 端点 / launch.model / work_dir 等
    // 关键不变量；不合法会立即抛错，避免把坏配置带进长时间运行的会话。
    validateAssignment(assignment);
    return assignment;
}
