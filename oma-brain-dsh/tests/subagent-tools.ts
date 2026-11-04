import { Backend, modelSystemText } from "./backend.ts";
import { ToolCallId } from "@deepseek-ai/dsh-llm";
import type { CcrSession } from "../src/ccrv2/session.ts";
import type { Payload } from "../src/json.ts";

export async function executeTool(session: CcrSession, name: string, args: Payload): Promise<Payload> {
  const result = await session.ctx.tools.execute({
    calls: [{ type: "tool-call", id: ToolCallId(crypto.randomUUID()), name, arguments: JSON.stringify(args) }],
    agent: session.agent, signal: session.abort.signal,
  });
  const outcome = result.results[0]!.result;
  if (outcome.isError) throw new Error(JSON.stringify(outcome.content));
  return outcome.value as Payload;
}

export async function runSubagent(session: CcrSession, name: string, args: Payload): Promise<{ subagentId: string; report: string }> {
  const callId = ToolCallId(crypto.randomUUID());
  const endings = new Map<string, { report: string; completed: boolean }>();
  const ended = new Map<string, () => void>();
  const dispose = session.ctx.on("subagent/end", info => {
    endings.set(info.id, {
      report: info.lastAssistantMessage?.filter(block => block.type === "text").map(block => block.text).join("\n") ?? "",
      completed: info.stopReason === "completed",
    });
    ended.get(info.id)?.();
  });
  let settled = false;
  const dispatched = session.ctx.tools.execute({ calls: [{ type: "tool-call", id: callId, name, arguments: JSON.stringify(args) }], agent: session.agent, signal: session.abort.signal })
    .finally(() => { settled = true; });
  try {
    while (!settled) {
      const approval = [...session.approvals].find(([, value]) => value.details.tool_use_id === callId);
      if (approval) {
        await session.control({ type: "control_response", response: { subtype: "success", request_id: approval[0], response: { behavior: "allow" } } });
        break;
      }
      await Bun.sleep(1);
    }
    const result = await dispatched;
    const outcomeResult = result.results[0]!.result;
    if (outcomeResult.isError) throw new Error(JSON.stringify(outcomeResult.content));
    const id = (outcomeResult.value as Payload).subagentId ?? args.agent_id;
    if (!endings.has(id)) await new Promise<void>(resolve => { ended.set(id, resolve); });
    await session.subagents.settleEvents();
    const outcome = endings.get(id)!;
    if (!outcome.completed) throw new Error("Subagent turn stopped before completion");
    return { subagentId: id, report: outcome.report };
  } finally { dispose(); }
}

export function subagentScripts(backend: Backend, parent: Payload[][], child: Payload[][]): void {
  let parentIndex = 0;
  let childIndex = 0;
  backend.onModel = async request => {
    if (modelSystemText(request).includes("# Subagent rules")) {
      const response = child[childIndex++];
      if (!response) throw new Error("Child model script exhausted");
      return response;
    }
    return parent[Math.min(parentIndex++, parent.length - 1)]!;
  };
}
