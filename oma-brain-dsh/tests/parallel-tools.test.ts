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
const approved = (callId: string, readOnly = false) => ({ callId, name: "Read", original: read.input, effective: read.input, readOnly });
const outcome = (callId: string) => ({ ...approved(callId), text: callId, failed: false });
const result = (text: string) => ({ content: [{ type: "text", text }] });

test("unapproved, duplicate and already finished calls cannot dispatch or overwrite results", async () => {
  const { session } = await fixture();
  await expect(session.recordDispatch("missing")).rejects.toThrow("Invalid or blocked tool dispatch");
  await expect(session.recordResult(outcome("missing"))).rejects.toThrow("Tool result does not match dispatch");
  await session.recordApproved(approved("first"));
  await expect(session.recordResult(outcome("first"))).rejects.toThrow("Tool result does not match dispatch");
  await session.recordDispatch("first");
  await expect(session.recordDispatch("first")).rejects.toThrow("Invalid or blocked tool dispatch");
  await session.recordResult(outcome("first"));
  await expect(session.recordDispatch("first")).rejects.toThrow("Invalid or blocked tool dispatch");
  await expect(session.recordResult({ ...outcome("first"), text: "overwrite" })).rejects.toThrow("Tool result does not match dispatch");
  expect(session.execution(session.id).toolOutcomes.first!.text).toBe("first");
});

test("read-only calls record one result without entering dispatch", async () => {
  const { session } = await fixture();
  await expect(session.recordResult({ ...outcome("missing"), readOnly: true })).rejects.toThrow("Tool result does not match dispatch");
  await session.recordApproved(approved("read", true));
  expect(session.execution(session.id).callPhases ?? {}).toEqual({});
  await session.recordResult({ ...outcome("read"), readOnly: true });
  await expect(session.recordResult({ ...outcome("read"), readOnly: true, text: "overwrite" })).rejects.toThrow("Tool result does not match dispatch");
  expect(session.execution(session.id).toolOutcomes.read!.text).toBe("read");
  expect(session.blocked()).toBe(false);
});

test("a confirmed sibling cannot clear an unknown call or allow further dispatch", async () => {
  const { session } = await fixture();
  await session.recordApproved(approved("unknown"));
  await session.recordDispatch("unknown");
  await session.recordApproved(approved("confirmed"));
  await session.recordDispatch("confirmed");
  await session.recordApproved(approved("waiting"));
  await session.recordUnknown("unknown");
  await session.recordResult(outcome("confirmed"));
  expect(session.blocked()).toBe(true);
  expect(session.execution(session.id).callPhases).toEqual({ unknown: "blocked", confirmed: "result", waiting: "approved" });
  await expect(session.recordDispatch("waiting")).rejects.toThrow("Invalid or blocked tool dispatch");
});

test("restoration blocks each unfinished dispatch and preserves confirmed siblings without replay", async () => {
  const { session, b } = await fixture();
  session.state.inputId = "interrupted-read-batch";
  for (const id of ["unfinished", "confirmed", "waiting"]) await session.recordApproved(approved(id));
  await session.recordDispatch("unfinished");
  await session.recordDispatch("confirmed");
  await session.recordResult(outcome("confirmed"));
  await session.close();
  const ctx = new Context();
  const api = new OmaApi(b.url);
  await mountOma(ctx, api);
  const resumed = createOmaSession(ctx, api, b.a);
  try {
    await resumed.open();
    expect(resumed.blocked()).toBe(true);
    expect(resumed.execution(resumed.id).callPhases).toEqual({ unfinished: "blocked", confirmed: "result", waiting: "interrupted" });
    expect(resumed.execution(resumed.id).toolOutcomes.confirmed!.text).toBe("confirmed");
    await expect(resumed.receive("new-input", { content: "continue" })).rejects.toThrow("reconciliation");
    expect(b.executions).toHaveLength(0);
    expect(b.requests).toHaveLength(0);
  } finally { await resumed.close(); await ctx.fiber.dispose(); }
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
    expect(resumed.execution(resumed.id).callPhases ?? {}).toEqual({});
    expect(resumed.blocked()).toBe(false);
    b.modelScript = [textEvents("continued")];
    await resumed.receive("new-turn", { content: "retry" });
    expect(resumed.state.phase).toBe("completed");
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
    expect(session.execution(session.id).callPhases ?? {}).toEqual({});
    expect(session.execution(session.id).toolOutcomes.call_1).toBeDefined();
    expect(session.blocked()).toBe(false);
    expect(session.state.phase).toBe("completed");
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
    expect(session.execution(session.id).callPhases ?? {}).toEqual({});
    for (const index of [2, 1, 0]) {
      gates[index]!.resolve(result(`output_${index}`));
      await eventually(() => !!session.execution(session.id).toolOutcomes[`call_${index}`]);
      if (index > 0) {
        expect(session.blocked()).toBe(false);
        expect(b.requests).toHaveLength(1);
      }
    }
    await turn;
    expect(session.state.phase).toBe("completed");
    for (const index of [0, 1, 2]) {
      expect(session.execution(session.id).toolOutcomes[`call_${index}`]!.text).toBe(`output_${index}`);
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
      expect(session.execution(session.id).callPhases ?? {}).toEqual({});
      gates[0]!.resolve(result("read"));
      await eventually(() => b.executions.length === 2);
      expect(session.execution(session.id).callPhases).toEqual({ call_1: "dispatch" });
      gates[1]!.resolve(result("written"));
      await eventually(() => b.executions.length === 3);
      expect(session.execution(session.id).callPhases).toEqual({ call_1: "result" });
      gates[2]!.resolve(result("read after write"));
      await turn;
      expect(session.state.phase).toBe("completed");
    } finally {
      for (const gate of gates) gate.resolve(result("cleanup"));
      session.stop(); await settled;
    }
  });
}

test("always_allow tools skip the approval request while ask tools still wait for it", async () => {
  const { session, b } = await fixture((backend) => {
    backend.a.launch.tool_permissions = { read: "allow", bash: "ask" };
  });
  b.modelScript = [batch([read]), toolEvents("echo ok", "call_bash"), textEvents("done")];
  await session.receive("allow-read", { content: "Read then run" });
  expect(b.executions.map((call) => call.name)).toEqual(["read", "bash"]);
  expect(b.timeline.filter((e) => e.type === "control_request").map((e) => e.request.tool_name)).toEqual(["Bash"]);
  expect(b.statuses.filter((status) => status === "requires_action")).toHaveLength(1);
  expect(session.state.phase).toBe("completed");
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
  expect(session.blocked()).toBe(false);
  expect(session.execution(session.id).toolOutcomes.call_0).toEqual({ text: "read after retry", failed: false });
  expect(JSON.stringify(b.requests.at(-1)!.messages)).toContain("read after retry");
});

test("a read-only call that fails twice returns an error result to the model without blocking", async () => {
  const { session, b } = await fixture();
  b.onTool = async () => { throw new Error("sandbox unavailable"); };
  b.modelScript = [batch([read]), textEvents("recovered")];
  await session.receive("failed-read", { content: "Read it" });
  expect(b.executions).toHaveLength(2);
  expect(session.blocked()).toBe(false);
  expect(session.state.phase).toBe("completed");
  expect(session.execution(session.id).toolOutcomes.call_0).toEqual({ text: "Read-only tool outcome is unknown", failed: true });
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
  expect(session.blocked()).toBe(false);
  expect(session.state.phase).toBe("completed");
  expect(session.execution(session.id).callPhases).toEqual({ call_bash: "result" });
  expect(session.execution(session.id).toolOutcomes.call_bash).toEqual({
    text: "Sandbox tool call was not executed: Sandbox MCP request exceeds 1 MiB", failed: true,
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
  expect(session.blocked()).toBe(false);
  expect(session.state.phase).toBe("completed");
  expect(session.execution(session.id).toolOutcomes.call_bash).toEqual({
    text: "Sandbox tool returned an unsupported result: Only text MCP results are supported", failed: true,
  });
  expect(JSON.stringify(b.requests.at(-1)!.messages)).toContain('"is_error":true');
});
