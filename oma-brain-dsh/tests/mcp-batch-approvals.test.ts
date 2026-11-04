import { expect,test } from "bun:test";



import { toolEvents } from "./backend.ts";

import type { Payload } from "../src/json.ts";

import type { ToolExecutionBridge } from "../src/runtime/contracts.ts";

function pair(): Payload[] {
  const blocks = ["first", "second"].flatMap((name, index) => toolEvents("", `batch_${index}`).slice(1, 4).map(event => ({
    ...event, index,
    ...(event.type === "content_block_start" ? { content_block: { ...event.content_block, name: `mcp__peer__${name}` } } : {}),
    ...(event.type === "content_block_delta" ? { delta: { type: "input_json_delta", partial_json: "{}" } } : {}),
  })));
  return [toolEvents()[0]!, ...blocks, ...toolEvents().slice(4)];
}

test("MCP approval prefetch stops at unregistered, builtin and malformed call barriers", async () => {
  const { prefetchMcpApprovals } = await import("../src/runtime/tool-approvals.ts");
  const { ToolCallId } = await import("@deepseek-ai/dsh-llm");
  for (const barrier of [
    { name: "Bash", arguments: "{}" },
    { name: "mcp__missing", arguments: "{}" },
    { name: "mcp__peer__second", arguments: "not JSON" },
  ]) {
    const requests: string[] = [];
    const bridge: ToolExecutionBridge = {
      approve: async request => { requests.push(request.callId); return { allowed: true }; },
      runTool: (_id, execute) => execute(), approvalChanged() {},
    };
    const calls = [
      { type: "tool-call" as const, id: ToolCallId("first"), name: "mcp__peer__first", arguments: "{}" },
      { type: "tool-call" as const, id: ToolCallId("barrier"), ...barrier },
      { type: "tool-call" as const, id: ToolCallId("last"), name: "mcp__peer__second", arguments: "{}" },
    ];
    const registered = (name: string) => ["mcp__peer__first", "mcp__peer__second"].includes(name);
    prefetchMcpApprovals(bridge, calls, "first", registered, new AbortController().signal);
    prefetchMcpApprovals(bridge, calls, "first", registered, new AbortController().signal);
    expect(requests).toEqual(["first"]);
  }
});
