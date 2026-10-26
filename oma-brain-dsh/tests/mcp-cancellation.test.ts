import { expect, test } from "bun:test";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { kill } from "node:process";
import { ToolCallId } from "@deepseek-ai/dsh-llm";
import { ToolExecution } from "../src/runtime/execution.ts";
import type { ToolExecutionBridge } from "../src/runtime/contracts.ts";
import { OmaApi } from "../src/api.ts";
import { SandboxMcp } from "../src/oma-sandbox/client.ts";
import { sandboxToolDefinitions } from "../src/sandbox-tools/definitions.ts";

const binary = process.env.OMA_TEST_MCP_BINARY;
const tool = sandboxToolDefinitions().find((tool) => tool.name === "Bash")!;

async function sandbox() {
  if (!binary) throw new Error("OMA_TEST_MCP_BINARY must point to the real Rust MCP executable");
  const socket = createServer();
  await new Promise<void>((resolve) => socket.listen(0, "127.0.0.1", resolve));
  const address = socket.address();
  if (!address || typeof address === "string") throw new Error("Missing test port");
  const url = `http://127.0.0.1:${address.port}`;
  await new Promise<void>((resolve) => socket.close(() => resolve()));
  const workdir = await mkdtemp(join(tmpdir(), "oma-cancellation-"));
  const process = Bun.spawn([binary, "--transport", "http", "--addr", `127.0.0.1:${address.port}`, "--workdir", workdir], {
    stdout: "ignore", stderr: "ignore",
  });
  const methods: string[] = [];
  const api = new OmaApi(url, (async (input, init) => {
    const request = new Request(input, init);
    if (request.method === "POST") {
      const message = await request.clone().json();
      if (message.method) methods.push(message.method);
    }
    return fetch(request);
  }) as typeof fetch);
  api.assignments.set("cancellation-test", {
    session_id: "cancellation-test", mcp_config: { mcpServers: { sandbox: { type: "http", url: `${url}/mcp` } } },
  } as Parameters<typeof api.assignments.set>[1]);
  const client = new SandboxMcp(api, "cancellation-test");
  const close = async () => {
    await client.close();
    try {
      const pid = Number(await readFile(join(workdir, "pid"), "utf8"));
      if (pid > 0) kill(-pid, "SIGKILL");
    } catch {}
    process.kill();
    await process.exited;
    await rm(workdir, { recursive: true, force: true });
  };
  try {
    const deadline = Date.now() + 5_000;
    while (true) {
      try { await fetch(`${url}/mcp`); break; } catch {}
      if (Date.now() > deadline) throw new Error("MCP did not start");
      await Bun.sleep(10);
    }
    return { client, process, workdir, methods, close };
  } catch (error) { await close(); throw error; }
}

async function waitForStart(workdir: string) {
  const deadline = Date.now() + 5_000;
  while (true) {
    try { if (await readFile(join(workdir, "started"), "utf8") === "started") return; } catch {}
    if (Date.now() > deadline) throw new Error("Bash did not start");
    await Bun.sleep(10);
  }
}

test.skipIf(!binary)("lost MCP server rejects the tool outcome instead of reporting cancellation", async () => {
  const f = await sandbox();
  const abort = new AbortController();
  const outcome = f.client.call(tool, { command: "echo $$ > pid; printf started > started; sleep 120" }, abort.signal)
    .then(() => undefined, (error: unknown) => error);
  try {
    await waitForStart(f.workdir);
    f.process.kill();
    await f.process.exited;
    abort.abort();
    expect(await outcome).toBeInstanceOf(Error);
  } finally { await f.close(); }
}, 20_000);

for (const user of [false, true]) test.skipIf(!binary)(`ordinary ${user ? "user interruption" : "cancellation"} stops Bash without replay`, async () => {
  const f = await sandbox();
  const abort = new AbortController();
  const events: string[] = [];
  const bridge: ToolExecutionBridge = {
    approve: async () => ({ allowed: true }),
    async runTool(_id, execute) {
      events.push("dispatched");
      try { return await execute(); }
      finally { events.push("finished"); }
    },
    approvalChanged() {},
  };
  const execution = new ToolExecution({ provision: async () => {}, call: f.client.call.bind(f.client) }, bridge);
  const command = { command: "echo $$ > pid; printf started > started; sleep 120; touch forbidden" };
  const running = execution.execute(tool, command, { callId: ToolCallId("cancelled-call"), signal: abort.signal, deferContext: () => {} })
    .then(() => undefined, (error: unknown) => error);
  try {
    await waitForStart(f.workdir);
    abort.abort(user ? { kind: "user" } : undefined);
    const error = await running;
    expect(error).toBeInstanceOf(Error);
    expect((error as Error).message).toContain(user ? "Tool call interrupted." : "outcome is unknown");
    expect(events).toEqual(["dispatched", "finished"]);
    const pid = Number(await readFile(join(f.workdir, "pid"), "utf8"));
    const deadline = Date.now() + 5_000;
    while (true) {
      try { kill(-pid, 0); } catch { break; }
      if (Date.now() > deadline) throw new Error("Bash process group survived cancellation");
      await Bun.sleep(10);
    }
    expect(await Bun.file(join(f.workdir, "forbidden")).exists()).toBe(false);

  } finally {
    abort.abort();
    await running;
    await f.close();
  }
}, 20_000);

test.skipIf(!binary)("ordinary tool calls return completed results", async () => {
  const f = await sandbox();
  try {
    const result = await f.client.call(tool, { command: "printf COMPLETED" }, new AbortController().signal);
    expect(result.structuredContent.stdout).toBe("COMPLETED");
    expect(result.structuredContent.status).toBe("completed");
    expect(f.methods.some((method) => method.startsWith("tasks/"))).toBe(false);
  } finally { await f.close(); }
}, 20_000);
