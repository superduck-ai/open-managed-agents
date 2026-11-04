import type { Context } from "@deepseek-ai/cordis";
import { NativeToolExecutor } from "@deepseek-ai/dsh-tool-executor-native";
import type { ToolBatchInput, ToolBatchResult, ToolExecutor } from "@deepseek-ai/dsh-tools";
import { createToolResultMessage } from "@deepseek-ai/dsh-llm";
import { appendPluginRecord, pluginRecordOf } from "@deepseek-ai/dsh-session";
import type { DsrSession } from "./session.ts";

export class DsrSuspend extends Error { readonly code = "OMA_DSR_SUSPEND"; }

declare module "@deepseek-ai/dsh-session/types" {
  interface PluginRecordMap {
    "plugin:oma/dsr-dispatched": { version: 1; callId: string };
  }
}

export function dsrExecutor(ctx: Context, owner: DsrSession): ToolExecutor {
  const native = new NativeToolExecutor(ctx);
  return {
    describe: tools => native.describe(tools),
    execute: async input => {
      if (!input.agent || !input.log) return native.execute(input);
      await owner.checkpoint();
      const agent = input.agent;
      const thread = agent.id === owner.id ? undefined : agent.id;
      const events = agent.session.snapshotEvents();
      const dispatched = new Set(events.map(pluginRecordOf).filter(record => record?.type === "plugin:oma/dsr-dispatched")
        .map(record => (record!.data as { callId: string }).callId));
      await Promise.all(input.calls.map(async call => {
        let argumentsValue: unknown;
        try { argumentsValue = JSON.parse(call.arguments); } catch { return; }
        if (!argumentsValue || typeof argumentsValue !== "object" || Array.isArray(argumentsValue)) return;
        if (!dispatched.has(call.id)) await owner.approve({ callId: call.id, toolName: call.name, input: argumentsValue as Record<string, unknown> }, input.signal, thread);
      }));
      const results: ToolBatchResult["results"][number][] = [];
      const contexts: ToolBatchResult["additionalContexts"][number][] = [];
      let concluded = false;
      for (const call of input.calls) {
        const previous = agent.session.snapshotEvents().find(event => event.type === "tool/call" && event.data.callId === call.id);
        const callSeq = previous?.seq ?? agent.session.append("tool/call", { ...input.log, callId: call.id, name: call.name, arguments: call.arguments }).seq;
        let result: ToolBatchResult["results"][number];
        if (concluded || input.signal.aborted) {
          const message = "Tool call skipped because the batch has concluded or was interrupted";
          result = { callId: call.id, result: { isError: true, error: { message }, content: [{ type: "text", text: message }] } };
        } else if (dispatched.has(call.id)) {
          const message = "TOOL_OUTCOME_UNKNOWN: The preceding worker dispatched this call without a durable result. Verify external state before repeating side effects.";
          result = { callId: call.id, result: { isError: true, error: { message }, content: [{ type: "text", text: message }] } };
        } else {
          appendPluginRecord(agent.session, "plugin:oma/dsr-dispatched", { version: 1, callId: call.id });
          await owner.checkpoint();
          const single: ToolBatchInput = { ...input, calls: [call], log: undefined, maxParallelToolCalls: 1 };
          const settled = await native.execute(single);
          result = settled.results[0]!;
          contexts.push(...settled.additionalContexts);
          concluded ||= settled.concluded;
        }
        agent.session.append("tool/result", { ...input.log, message: createToolResultMessage({ callId: call.id, content: result.result.content, isError: result.result.isError }) }, { surfaceOp: "append", sourceEventSeqs: [callSeq] });
        await owner.checkpoint();
        results.push(result);
      }
      return { results, additionalContexts: contexts, concluded };
    },
  };
}
