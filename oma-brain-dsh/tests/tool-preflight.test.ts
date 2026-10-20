import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { Backend, textEvents, toolEvents } from "./backend.ts";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import type { Payload } from "../src/json.ts";

test("tool preflight publishes each attempted call once before failed or successful results", async () => {
  for (const scenario of [
    { name: "Bash", args: {}, allowed: true, failed: true, executions: 0 },
    { name: "unknown_tool", args: {}, allowed: false, failed: true, executions: 0 },
    { name: "archive_agent", args: { thread_id: "missing" }, allowed: true, failed: true, executions: 0 },
    { name: "list_agents", args: {}, allowed: true, failed: false, executions: 0 },
    { name: "Bash", args: { command: "pwd" }, allowed: true, failed: false, executions: 1 },
  ]) {
    const b = new Backend();
    await b.ready;
    b.a.launch.subagents = { inlineAgents: true, agents: [] };
    const ctx = new Context();
    const api = new OmaApi(b.url);
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, b.a);
    try {
      await session.open();
      b.onApproval = async (payload) => {
        await session.control({ type: "control_response", response: {
          subtype: "success", request_id: payload.request_id,
          response: { behavior: scenario.allowed ? "allow" : "deny" },
        } });
      };
      b.modelScript = [toolEvents("", "attempt").map((event: Payload) =>
        event.type === "content_block_start" ? { ...event, content_block: { ...event.content_block, name: scenario.name } } :
        event.type === "content_block_delta" ? { ...event, delta: { type: "input_json_delta", partial_json: JSON.stringify(scenario.args) } } : event), textEvents("done")];
      await session.receive("input", { content: "Local diagnostic" });
      const events = [...b.publicLog.values()];
      const callIndex = events.findIndex(event => event.type === "control_request");
      const resultIndex = events.findIndex(event => event.type === "user");
      expect(events.filter(event => event.type === "control_request")).toHaveLength(1);
      expect(callIndex).toBeGreaterThanOrEqual(0);
      expect(resultIndex).toBeGreaterThan(callIndex);
      expect(events[callIndex]!.request).toMatchObject({ tool_use_id: "attempt", tool_name: scenario.name, input: scenario.args });
      expect(events[resultIndex]!.message.content[0]).toMatchObject({ tool_use_id: "attempt", is_error: scenario.failed });
      expect(b.executions).toHaveLength(scenario.executions);
    } finally {
      await session.close();
      await ctx.fiber.dispose();
      await b.stop();
    }
  }
});
