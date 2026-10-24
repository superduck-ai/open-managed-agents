import { nativeOutcomes } from "./session-log.ts";
import { afterEach, expect, test } from "bun:test";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, eventually, textEvents, toolEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });

async function fixture(prepare?: (b: Backend) => void) {
  const b = new Backend();
  await b.ready;
  prepare?.(b);
  const ctx = new Context();
  const api = new OmaApi(b.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  cleanups.push(async () => { await session.close(); await ctx.fiber.dispose(); b.stop(); });
  await session.open();
  b.onApproval = async (request) => {
    await session.control({ type: "control_response", response: {
      subtype: "success", request_id: request.request_id, response: { behavior: "allow" },
    } });
  };
  return { b, ctx, session };
}

function batch(calls: { name: string; input: Payload }[]): Payload[] {
  const blocks = calls.flatMap((call, index) => toolEvents("", `call_${index}`).slice(1, 4).map((event) => ({
    ...event, index,
    ...(event.type === "content_block_start" ? { content_block: { ...event.content_block, name: call.name } } : {}),
    ...(event.type === "content_block_delta" ? { delta: { type: "input_json_delta", partial_json: JSON.stringify(call.input) } } : {}),
  })));
  return [toolEvents()[0]!, ...blocks, ...toolEvents().slice(4)];
}

const read = { name: "Read", input: { file_path: "/tmp/read.txt" } };
const result = (text: string) => ({ content: [{ type: "text", text }] });

test("a failed permission refresh cannot reuse a previous allow", async () => {
  const { session, b } = await fixture((backend) => {
    backend.a.launch.tool_permissions = { bash: "allow" };
  });
  b.modelScript = [toolEvents("echo first", "first"), textEvents("first done")];
  await session.receive("first-turn", { content: "Run first" });
  const route = b.route.bind(b);
  b.route = async (request) => new URL(request.url).pathname === `/v2/sessions/${b.a.session_id}`
    ? new Response("unavailable", { status: 503 }) : route(request);
  b.modelScript = [toolEvents("echo second", "second"), textEvents("second done")];
  await session.receive("second-turn", { content: "Run second" });
  expect(b.requests).toHaveLength(2);
  expect(b.executions).toHaveLength(1);
});

test("a missing refreshed permission table clears a previous allow", async () => {
  const { session, b } = await fixture((backend) => {
    backend.a.launch.tool_permissions = { bash: "allow" };
  });
  b.modelScript = [toolEvents("echo first", "first"), textEvents("first done")];
  await session.receive("first-turn", { content: "Run first" });
  delete b.a.launch.tool_permissions;
  b.onApproval = async (request) => {
    await session.control({ type: "control_response", response: {
      subtype: "success", request_id: request.request_id, response: { behavior: "deny" },
    } });
  };
  b.modelScript = [toolEvents("echo second", "second"), textEvents("second done")];
  await session.receive("second-turn", { content: "Run second" });
  expect(b.timeline.filter((event) => event.type === "control_request")).toHaveLength(1);
  expect(b.executions).toHaveLength(1);
  expect(session.abort.signal.aborted).toBe(false);
});

for (const policy of ["ask", "deny"] as const) {
  test(`changing allow to ${policy} takes effect in the same session`, async () => {
    const { session, b } = await fixture((backend) => {
      backend.a.launch.tool_permissions = { bash: "allow" };
    });
    b.modelScript = [toolEvents("echo first", "first"), textEvents("first done")];
    await session.receive("first-turn", { content: "Run first" });
    expect(b.executions).toHaveLength(1);
    expect(b.timeline.filter((event) => event.type === "control_request")).toHaveLength(0);
    b.a.launch.tool_permissions = { bash: policy };
    const approvals: Payload[] = [];
    b.onApproval = async (request) => { approvals.push(request); };
    b.modelScript = [toolEvents("echo second", "second"), textEvents("second done")];
    const turn = session.receive("second-turn", { content: "Run second" });
    const settled = turn.catch(() => {});
    try {
      await eventually(() => approvals.length === 1 || b.executions.length > 1);
      expect(approvals).toHaveLength(1);
      expect(b.executions).toHaveLength(1);
      expect(approvals[0]!.request.tool_name).toBe("Bash");
      await session.control({ type: "control_response", response: {
        subtype: "success", request_id: approvals[0]!.request_id,
        response: { behavior: policy === "ask" ? "allow" : "deny" },
      } });
      await turn;
      expect(b.executions).toHaveLength(policy === "ask" ? 2 : 1);
      expect(session.abort.signal.aborted).toBe(false);
    } finally { session.stop(); await settled; }
  });
}

test("drain waits for every active tool and rejects new work without a persistent execution journal", async () => {
  const { session, b } = await fixture();
  const first = Promise.withResolvers<string>();
  const second = Promise.withResolvers<string>();
  b.modelScript = [toolEvents("first", "first"), toolEvents("second", "second"), textEvents("first done"), textEvents("second done")];
  b.onTool = async (params) => result(await (params.arguments.command === "first" ? first.promise : second.promise));
  const calls = ["alpha", "beta"].map((name) => session.subagents.send({
    name, system_prompt: name, message: "Run a command",
  }, new AbortController().signal));
  try {
    await eventually(() => b.executions.length === 2);
    expect(await session.drain(0)).toBe(false);
    await expect(session.runTool(session.id, "later", async () => "must not execute")).rejects.toThrow("not executed");
    first.resolve("first");
    await calls[0];
    expect(session.dispatching()).toBe(true);
    second.resolve("second");
    await calls[1];
    await eventually(() => !session.dispatching());
    expect(await session.drain(100)).toBe(true);
    expect(session.delivery).not.toHaveProperty("executions");
    expect(b.executions).toHaveLength(2);
  } finally {
    first.resolve("first"); second.resolve("second");
    await Promise.allSettled(calls);
  }
});

test.each([
  read,
  { name: "Glob", input: { pattern: "**/*.ts" } },
  { name: "Grep", input: { pattern: "needle", path: "/tmp" } },
])("drain allows $name to reach the sandbox and persist its native result", async (call) => {
  const { session, b } = await fixture();
  b.modelScript = [batch([call]), textEvents("read complete")];
  expect(await session.drain(0)).toBe(true);
  await session.receive("read-during-drain", { content: "Inspect files" });
  expect(b.executions).toHaveLength(1);
  expect(b.executions[0]!.name).toBe(call.name.toLowerCase());
  expect(nativeOutcomes(session).call_0?.failed).toBe(false);
  expect(session.dispatching()).toBe(false);
});

test("active reads do not extend the side-effect drain budget", async () => {
  const { session, b } = await fixture();
  const release = Promise.withResolvers<Payload>();
  b.modelScript = [batch([read]), textEvents("done")];
  b.onTool = () => release.promise;
  const turn = session.receive("active-read", { content: "Inspect" });
  const settled = turn.catch(() => {});
  try {
    await eventually(() => b.executions.length === 1);
    expect(await session.drain(0)).toBe(true);
    release.resolve(result("read completed"));
    await turn;
    expect(nativeOutcomes(session).call_0?.failed).toBe(false);
  } finally { release.resolve(result("cleanup")); session.stop(); await settled; }
});

test("busy sessions reject new input before accepting another delivery", async () => {
  const { session } = await fixture();
  session.runtime.busy = true;
  try { await expect(session.receive("concurrent", { content: "continue" })).rejects.toThrow("busy"); }
  finally { session.runtime.busy = false; }
  expect(session.delivery.completed).not.toContain("concurrent");
});

test("SIGKILL during parallel reads interrupts the turn without blocking or replay", async () => {
  const b = new Backend();
  await b.ready;
  b.modelScript = [batch([read, { ...read, input: { file_path: "/tmp/finished" } }])];
  b.onTool = async (params) => params.arguments.file_path === "/tmp/finished"
    ? result("confirmed before crash") : await new Promise(() => {});
  const child = spawn(process.execPath, ["tests/crash-worker.ts"], {
    env: { ...process.env, TEST_API_URL: b.url, TEST_ASSIGNMENT: JSON.stringify(b.a) },
    stdio: ["pipe", "ignore", "ignore"],
  });
  const exited = once(child, "exit");
  b.onApproval = async (request) => { child.stdin!.write(JSON.stringify({ type: "control_response", response: {
    subtype: "success", request_id: request.request_id, response: { behavior: "allow" },
  } }) + "\n"); };
  const ctx = new Context();
  let resumed: ReturnType<typeof createOmaSession> | undefined;
  try {
    await eventually(() => b.executions.length === 2);
    child.kill("SIGKILL"); await exited;
    b.a.worker_epoch++;
    const api = new OmaApi(b.url);
    await mountOma(ctx, api);
    resumed = createOmaSession(ctx, api, b.a);
    await resumed.open();
    expect(resumed.abort.signal.aborted).toBe(false);
    b.modelScript = [textEvents("continued")];
    await resumed.receive("new-turn", { content: "retry" });
    expect(resumed.delivery.completed).toContain(resumed.delivery.inputId);
    expect(b.requests).toHaveLength(2);
    expect(b.executions).toHaveLength(2);
  } finally {
    child.kill("SIGKILL"); await exited;
    await resumed?.close(); await ctx.fiber.dispose(); b.stop();
  }
});

test("denying one parallel read leaves its sibling executable", async () => {
  const { session, b } = await fixture();
  b.modelScript = [batch([read, read]), textEvents("done")];
  const requests: Payload[] = [];
  b.onApproval = async (request) => { requests.push(request); };
  const turn = session.receive("deny-one-read", { content: "Read independently" });
  const settled = turn.catch(() => {});
  try {
    await eventually(() => requests.length === 2);
    for (const request of requests) await session.control({ type: "control_response", response: {
      subtype: "success", request_id: request.request_id,
      response: { behavior: request.request.tool_use_id === "call_0" ? "deny" : "allow" },
    } });
    await turn;
    expect(b.executions).toHaveLength(1);
    expect(nativeOutcomes(session, session.id).call_1).toBeDefined();
    expect(session.abort.signal.aborted).toBe(false);
    expect(session.delivery.completed).toContain(session.delivery.inputId);
  } finally { session.stop(); await settled; }
});

test("Read, Glob and Grep overlap in the native loop and accept results in reverse order", async () => {
  const { session, b } = await fixture();
  const gates = [Promise.withResolvers<Payload>(), Promise.withResolvers<Payload>(), Promise.withResolvers<Payload>()];
  b.modelScript = [batch([read, { name: "Glob", input: { pattern: "*.txt" } }, { name: "Grep", input: { pattern: "hello" } }]), textEvents("done")];
  b.onTool = (params) => gates[["read", "glob", "grep"].indexOf(params.name)]!.promise;
  const turn = session.receive("parallel-reads", { content: "Read and search independently" });
  const settled = turn.catch(() => {});
  try {
    await eventually(() => b.executions.length === 3);
    for (const index of [2, 1, 0]) {
      gates[index]!.resolve(result(`output_${index}`));
      await new Promise<void>((resolve) => setImmediate(resolve));
      if (index > 0) {
        expect(session.abort.signal.aborted).toBe(false);
        expect(b.requests).toHaveLength(1);
      }
    }
    await turn;
    expect(session.delivery.completed).toContain(session.delivery.inputId);
    for (const index of [0, 1, 2]) {
      expect(nativeOutcomes(session, session.id)[`call_${index}`]!.text).toBe(`output_${index}`);
      expect(JSON.stringify(b.requests.at(-1)!.messages)).toContain(`output_${index}`);
    }
  } finally {
    for (const gate of gates) gate.resolve(result("cleanup"));
    session.stop(); await settled;
  }
});

for (const exclusive of [
  { name: "Bash", input: { command: "echo ok" } },
  { name: "Write", input: { file_path: "/tmp/output.txt", content: "ok" } },
  { name: "Edit", input: { file_path: "/tmp/output.txt", old_string: "old", new_string: "new" } },
]) {
  test(`${exclusive.name} waits for preceding reads and blocks following reads`, async () => {
    const { session, b } = await fixture();
    const gates = [Promise.withResolvers<Payload>(), Promise.withResolvers<Payload>(), Promise.withResolvers<Payload>()];
    b.modelScript = [batch([read, exclusive, read]), textEvents("done")];
    b.onTool = () => gates[b.executions.length - 1]!.promise;
    const turn = session.receive(`serial-${exclusive.name}`, { content: "Execute the batch" });
    const settled = turn.catch(() => {});
    try {
      await eventually(() => b.executions.length === 1);
      gates[0]!.resolve(result("read"));
      await eventually(() => b.executions.length === 2);
      gates[1]!.resolve(result("written"));
      await eventually(() => b.executions.length === 3);
      gates[2]!.resolve(result("read after write"));
      await turn;
      expect(session.delivery.completed).toContain(session.delivery.inputId);
    } finally {
      for (const gate of gates) gate.resolve(result("cleanup"));
      session.stop(); await settled;
    }
  });
}

test("always_allow tools skip approval while immediately approved ask tools keep running", async () => {
  const { session, b } = await fixture((backend) => {
    backend.a.launch.tool_permissions = { read: "allow", bash: "ask" };
  });
  b.modelScript = [batch([read]), toolEvents("echo ok", "call_bash"), textEvents("done")];
  await session.receive("allow-read", { content: "Read then run" });
  expect(b.executions.map((call) => call.name)).toEqual(["read", "bash"]);
  expect(b.timeline.filter((e) => e.type === "control_request").map((e) => e.request.tool_name)).toEqual(["Bash"]);
  expect(b.statuses.filter((status) => status === "requires_action")).toHaveLength(0);
  expect(session.delivery.completed).toContain(session.delivery.inputId);
});

test("a failed read-only call is retried once without blocking", async () => {
  const { session, b } = await fixture();
  let attempts = 0;
  b.onTool = async () => {
    if (++attempts === 1) throw new Error("transient sandbox failure");
    return result("read after retry");
  };
  b.modelScript = [batch([read]), textEvents("done")];
  await session.receive("retry-read", { content: "Read it" });
  expect(attempts).toBe(2);
  expect(session.abort.signal.aborted).toBe(false);
  expect(nativeOutcomes(session, session.id).call_0).toEqual({ text: "read after retry", failed: false });
  expect(JSON.stringify(b.requests.at(-1)!.messages)).toContain("read after retry");
});

test("a read-only call that fails twice returns an error result to the model without blocking", async () => {
  const { session, b } = await fixture();
  b.onTool = async () => { throw new Error("sandbox unavailable"); };
  b.modelScript = [batch([read]), textEvents("recovered")];
  await session.receive("failed-read", { content: "Read it" });
  expect(b.executions).toHaveLength(2);
  expect(session.abort.signal.aborted).toBe(false);
  expect(session.delivery.completed).toContain(session.delivery.inputId);
  expect(nativeOutcomes(session, session.id).call_0).toEqual({ text: "Error: Read-only tool outcome is unknown", failed: true });
  const toolResult = JSON.stringify(b.requests.at(-1)!.messages);
  expect(toolResult).toContain('"is_error":true');
  expect(toolResult).toContain("Read-only tool outcome is unknown");
});

test("a gateway rejection before forwarding becomes a failed result instead of an unknown outcome", async () => {
  const { session, b } = await fixture();
  b.rejectTool = (params) => params.name === "bash";
  b.modelScript = [toolEvents("too large", "call_bash"), textEvents("recovered")];
  await session.receive("rejected-bash", { content: "Run it" });
  expect(b.executions).toHaveLength(0);
  expect(session.abort.signal.aborted).toBe(false);
  expect(session.delivery.completed).toContain(session.delivery.inputId);
  expect(nativeOutcomes(session, session.id).call_bash).toEqual({
    text: "Error: Sandbox tool call was not executed: Sandbox MCP request exceeds 1 MiB", failed: true,
  });
  const messages = JSON.stringify(b.requests.at(-1)!.messages);
  expect(messages).toContain('"is_error":true');
  expect(messages).toContain("exceeds 1 MiB");
});

test("an executed call with unsupported content becomes a failed result instead of an unknown outcome", async () => {
  const { session, b } = await fixture();
  b.onTool = async () => ({ content: [{ type: "image", data: "AA==", mimeType: "image/png" }] });
  b.modelScript = [toolEvents("render", "call_bash"), textEvents("recovered")];
  await session.receive("image-bash", { content: "Run it" });
  expect(b.executions).toHaveLength(1);
  expect(session.abort.signal.aborted).toBe(false);
  expect(session.delivery.completed).toContain(session.delivery.inputId);
  expect(nativeOutcomes(session, session.id).call_bash).toEqual({
    text: "Error: Sandbox tool returned an unsupported result: Only text MCP results are supported", failed: true,
  });
  expect(JSON.stringify(b.requests.at(-1)!.messages)).toContain('"is_error":true');
});
