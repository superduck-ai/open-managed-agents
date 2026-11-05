// ============================================================================
// Tool Executor：在原生工具执行之上加“审批 + 崩溃恢复”语义
// ============================================================================
//
// DSH 自带的 NativeToolExecutor 会直接执行工具。但 DSR 模式还需要三件事：
//
//   1. 审批：工具执行前，把调用和参数上报服务器，等待用户 allow / deny /
//      external_result。审批是人工操作，可能很久，所以超时后不能直接失败，
//      而要“挂起会话”，等后续重新认领再继续（见 DsrSuspend）。
//
//   2. 崩溃恢复：如果进程在“已经派发工具调用”和“写下工具结果”之间崩溃，
//      重启后无法知道副作用是否已经发生。因此执行前先写一条
//      plugin:oma/dsr-dispatched 记录；恢复时看到“已派发但没有结果”，
//      就返回 TOOL_OUTCOME_UNKNOWN，提醒模型先核实外部状态再决定是否重试。
//
//   3. 顺序性：同一批工具调用按顺序逐个执行，并且每个调用都先持久化
//      “已派发”再执行、执行完立即持久化结果，保证任意时刻崩溃都能判断状态。
//
// 一次执行的时间线（两个调用 A、B）：
//
//   checkpoint
//   approve(A) ──┐
//   approve(B) ──┘（并行等待审批）
//   append tool/call A -> 写 dispatched A -> checkpoint -> 执行 A -> append tool/result A -> checkpoint
//   append tool/call B -> 写 dispatched B -> checkpoint -> 执行 B -> append tool/result B -> checkpoint
//
// 如果 A 执行到一半进程崩溃：
//   恢复后看到 dispatched A 但没有 tool/result A -> 返回 TOOL_OUTCOME_UNKNOWN，
//   模型会先检查文件/命令的真实结果，而不是盲目再跑一次有副作用的操作。
// ============================================================================

import type {Context} from "@deepseek-ai/cordis";
import {NativeToolExecutor} from "@deepseek-ai/dsh-tool-executor-native";
import type {ToolBatchInput, ToolBatchResult, ToolExecutor} from "@deepseek-ai/dsh-tools";
import {createToolResultMessage} from "@deepseek-ai/dsh-llm";
import {appendPluginRecord, pluginRecordOf} from "@deepseek-ai/dsh-session";
import type {DsrSession} from "./session.ts";

// 审批等待超时、子 agent 未完成等情况下抛出的“挂起”信号。
// DsrSession.receive() 会检查 code === "OMA_DSR_SUSPEND"，
// 把会话标记为 suspended，保留未结束的 turn，等服务器重新派发后继续。
export class DsrSuspend extends Error {
    readonly code = "OMA_DSR_SUSPEND";
}

// 扩展 DSH 的插件记录类型表：允许在会话事件流里写入“这个工具调用已派发”的标记。
declare module "@deepseek-ai/dsh-session/types" {
    interface PluginRecordMap {
        "plugin:oma/dsr-dispatched": { version: 1; callId: string };
    }
}

/**
 * 构造 DSR 工具执行器。
 *
 * @param ctx   cordis 上下文，交给 NativeToolExecutor 使用（工具注册、取消等）。
 * @param owner 所属的 DsrSession，用来做审批、checkpoint 和会话事件读写。
 *
 * @example
 *   // session.ts 安装工具时：
 *   scope.tools.setExecutor(dsrExecutor(scope, this));
 */
export function dsrExecutor(ctx: Context, owner: DsrSession): ToolExecutor {
    const native = new NativeToolExecutor(ctx);
    return {
        // describe 只负责把工具描述给模型，不需要审批，直接透传。
        describe: tools => native.describe(tools),
        execute: async input => {
            // 没有 agent / log 的调用不是“会话里的工具调用”（例如内部探测），
            // 不涉及审批和会话持久化，直接交给原生执行器。
            if (!input.agent || !input.log) return native.execute(input);

            // 开始新一批调用前先 checkpoint：把之前缓存的公开事件、transcript、
            // 未完成 trace 都落盘，保证后续恢复看到一致状态。
            await owner.checkpoint();
            const agent = input.agent;
            // 根会话的 id 就是 owner.id；否则是子 agent 线程的 id。
            const thread = agent.id === owner.id ? undefined : agent.id;
            const events = agent.session.snapshotEvents();
            // 已持久化的“已派发”调用集合。用于恢复后识别未知结果。
            const dispatched = new Set(events.map(pluginRecordOf).filter(record => record?.type === "plugin:oma/dsr-dispatched")
                .map(record => (record!.data as { callId: string }).callId));

            // 审批阶段：并行等待这一批里所有“尚未派发”的调用。
            // 并行是因为审批彼此独立，串行会让用户逐个点确认，体验很差。
            await Promise.all(input.calls.map(async call => {
                let argumentsValue: unknown;
                // 参数不是合法 JSON 或不是对象：跳过审批，交给原生执行器返回它自己的错误。
                try {
                    argumentsValue = JSON.parse(call.arguments);
                } catch {
                    return;
                }
                if (!argumentsValue || typeof argumentsValue !== "object" || Array.isArray(argumentsValue)) return;
                // 已派发过的调用不再审批：它已经进入过执行流程，本次要做的是
                // “判断结果是否未知”，而不是重新征求许可。
                if (!dispatched.has(call.id)) await owner.approve({
                    callId: call.id,
                    toolName: call.name,
                    input: argumentsValue as Record<string, unknown>
                }, input.signal, thread);
            }));

            const results: ToolBatchResult["results"][number][] = [];
            const contexts: ToolBatchResult["additionalContexts"][number][] = [];
            // concluded 表示原生执行器判定本批已经结束（例如某个工具要求停止后续调用）。
            let concluded = false;
            // 逐个执行，保证 call / dispatched / result 的持久化顺序是确定的。
            for (const call of input.calls) {
                // 如果之前已有同 id 的 tool/call 事件（恢复场景），复用它的 seq；
                // 否则现在追加一条，拿到新的 seq 作为结果事件的来源锚点。
                const previous = agent.session.snapshotEvents().find(event => event.type === "tool/call" && event.data.callId === call.id);
                const callSeq = previous?.seq ?? agent.session.append("tool/call", {
                    ...input.log,
                    callId: call.id,
                    name: call.name,
                    arguments: call.arguments
                }).seq;
                let result: ToolBatchResult["results"][number];
                if (concluded || input.signal.aborted) {
                    // 本批已结束或被中断：不再执行剩余调用，但每个调用都必须有结果，
                    // 否则会话事件流会出现“有 call 没 result”的不完整状态。
                    const message = "Tool call skipped because the batch has concluded or was interrupted";
                    result = {
                        callId: call.id,
                        result: {isError: true, error: {message}, content: [{type: "text", text: message}]}
                    };
                } else if (dispatched.has(call.id)) {
                    // 关键恢复分支：记录显示“已派发”，但当前这次执行没有对应的结果
                    //（通常意味着上个进程在执行中途崩溃）。不能假设成功或失败，
                    // 也不能自动重试有副作用的操作。
                    const message = "TOOL_OUTCOME_UNKNOWN: The preceding worker dispatched this call without a durable result. Verify external state before repeating side effects.";
                    result = {
                        callId: call.id,
                        result: {isError: true, error: {message}, content: [{type: "text", text: message}]}
                    };
                } else {
                    // 写前日志（write-ahead）：先持久化“已派发”，再真正执行。
                    // 这样任何时刻崩溃，恢复时都能看出这个调用可能已经产生副作用。
                    appendPluginRecord(agent.session, "plugin:oma/dsr-dispatched", {version: 1, callId: call.id});
                    await owner.checkpoint();
                    // 只执行当前这一个调用（maxParallelToolCalls: 1），维持严格顺序。
                    const single: ToolBatchInput = {...input, calls: [call], log: undefined, maxParallelToolCalls: 1};
                    const settled = await native.execute(single);
                    result = settled.results[0]!;
                    contexts.push(...settled.additionalContexts);
                    concluded ||= settled.concluded;
                }
                // 追加 tool/result。surfaceOp: "append" 表示这是展示面事件；
                // sourceEventSeqs 指向对应的 tool/call，便于前端/投影关联。
                agent.session.append("tool/result", {
                    ...input.log,
                    message: createToolResultMessage({
                        callId: call.id,
                        content: result.result.content,
                        isError: result.result.isError
                    })
                }, {surfaceOp: "append", sourceEventSeqs: [callSeq]});
                // 每个结果立即 checkpoint：即使下一个调用前崩溃，已完成的结果也不会丢。
                await owner.checkpoint();
                results.push(result);
            }
            return {results, additionalContexts: contexts, concluded};
        },
    };
}
