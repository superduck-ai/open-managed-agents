import { expect, test } from "bun:test";
import { Context, type Fiber } from "@deepseek-ai/cordis";
import { SessionId } from "@deepseek-ai/dsh-session";
import { ToolCallId } from "@deepseek-ai/dsh-llm";
import { mountKernel } from "../src/kernel.ts";
import * as SandboxTools from "../src/sandbox-tools/index.ts";
import type { SandboxExecutor, ToolExecutionBridge } from "../src/runtime/contracts.ts";

test("sandbox tools are built in, isolated per agent and removed with their plugin", async () => {
  const ctx = new Context();
  await mountKernel(ctx);
  const calls: string[] = [];
  const plugins = new Map<string, Fiber>();
  const bridge: ToolExecutionBridge = {
    blocked: () => false,
    async approve() { return { allowed: true }; },
    async recordApproved() {},
    async flushLog() {},
    async recordDispatch() {},
    async recordUnknown() {},
    async recordNotDispatched() {},
    async recordResult() {},
    stop() {},
  };
  const create = (id: string) => ctx.agents.create({
    sessionId: SessionId(id),
    agentOptions: { provider: "unused", model: "unused" },
    async setup(agentScope) {
      const scope = agentScope.isolate("sandboxExecutor");
      const executor: SandboxExecutor = {
        async provision() {},
        async call(tool, args) {
          calls.push(`${id}:${tool.remoteName}:${args.command}`);
          return { content: [{ type: "text", text: id }] };
        },
      };
      await scope.plugin({
        name: "test-sandbox-provider",
        apply(ctx: Context) { ctx.provide("sandboxExecutor", executor); },
      });
      plugins.set(id, await scope.plugin(SandboxTools, { bridge }));
    },
  });
  try {
    const a = await create("sandbox_a");
    const b = await create("sandbox_b");
    for (const agent of [a.agent, b.agent]) {
      for (const name of ["Bash", "Read", "Write", "Edit", "Grep", "Glob"]) {
        expect(agent.ctx.tools.get(name, agent)).toBeDefined();
      }
    }
    expect(ctx.tools.get("Bash")).toBeUndefined();
    const run = (agent: typeof a.agent, command: string) => agent.ctx.tools.execute({
      callId: ToolCallId(crypto.randomUUID()), name: "Bash",
      arguments: { command }, agent, signal: new AbortController().signal,
    });
    expect((await run(a.agent, "first")).isError).toBe(false);
    expect((await run(b.agent, "second")).isError).toBe(false);
    expect(calls).toEqual(["sandbox_a:bash:first", "sandbox_b:bash:second"]);
    const invalid = await a.agent.ctx.tools.execute({
      callId: ToolCallId("invalid_read"), name: "Read", arguments: { command: "wrong" },
      agent: a.agent, signal: new AbortController().signal,
    });
    expect(invalid.isError).toBe(true);
    expect(calls).toHaveLength(2);
    await plugins.get("sandbox_a")!.dispose();
    expect(a.agent.ctx.tools.get("Bash", a.agent)).toBeUndefined();
    expect(b.agent.ctx.tools.get("Bash", b.agent)).toBeDefined();
    expect((await run(b.agent, "still available")).isError).toBe(false);
    expect(calls.at(-1)).toBe("sandbox_b:bash:still available");
    await a.dispose();
    await b.dispose();
  } finally {
    await ctx.fiber.dispose();
  }
});
