import { expect, test } from "bun:test";
import { ToolCallId } from "@deepseek-ai/dsh-llm";
import { ToolExecution } from "../src/runtime/execution.ts";
import { SHUTDOWN_CANCEL } from "../src/runtime/contracts.ts";
import type {
  RemoteTool, SandboxExecutor, ToolExecutionBridge, ToolExecutionContext,
} from "../src/runtime/contracts.ts";

const tool: RemoteTool = {
  name: "Bash", remoteName: "bash", description: "Test execution",
  inputSchema: {
    type: "object", properties: { command: { type: "string" } },
    required: ["command"], additionalProperties: false,
  },
};

function fixture() {
  let provisions = 0;
  let executions = 0;
  let approvals = 0;
  let stopped = false;
  let blocked = false;
  let resultConfirmed = false;
  const deferred: unknown[] = [];
  const sandbox: SandboxExecutor = {
    async provision() { provisions++; },
    async call(_tool, args) {
      executions++;
      expect(args).toEqual({ command: "approved" });
      return { content: [{ type: "text", text: "stdout" }] };
    },
  };
  const bridge: ToolExecutionBridge = {
    blocked: () => blocked,
    async approve() {
      approvals++;
      return { allowed: true, input: { command: "approved" } };
    },
    async recordApproved() {},
    async flushLog() {},
    async recordDispatch() {},
    async recordUnknown() { blocked = true; },
    async recordResult() { resultConfirmed = true; },
    stop() { stopped = true; },
  };
  const exec: ToolExecutionContext = {
    callId: ToolCallId("call_1"), signal: new AbortController().signal,
    deferContext(context) {
      expect(resultConfirmed).toBe(true);
      deferred.push(context);
    },
  };
  return {
    sandbox, bridge, exec, deferred,
    execution: new ToolExecution(sandbox, bridge),
    get provisions() { return provisions; },
    get executions() { return executions; },
    get approvals() { return approvals; },
    get stopped() { return stopped; },
  };
}

test("runtime waits for durable dispatch before execution and confirmed results before model context", async () => {
  const f = fixture();
  const writing = Promise.withResolvers<void>();
  const confirmation = Promise.withResolvers<void>();
  f.bridge.recordDispatch = async () => {
    writing.resolve();
    await confirmation.promise;
  };
  const turn = f.execution.execute(tool, { command: "original" }, f.exec);
  await writing.promise;
  expect(f.provisions).toBe(1);
  expect(f.executions).toBe(0);
  expect(f.deferred).toHaveLength(0);
  confirmation.resolve();
  expect(await turn).toBe("stdout");
  expect(f.executions).toBe(1);
  expect(f.deferred).toHaveLength(1);
});

test("failed approval or dispatch checkpoints prevent sandbox side effects", async () => {
  for (const barrier of ["recordApproved", "recordDispatch"] as const) {
    const f = fixture();
    f.bridge[barrier] = async () => { throw new Error("Write not confirmed"); };
    await expect(f.execution.execute(tool, { command: "original" }, f.exec))
      .rejects.toThrow("Write not confirmed");
    expect(f.executions).toBe(0);
    expect(f.provisions).toBe(barrier === "recordApproved" ? 0 : 1);
  }
});

test("unknown sandbox outcome is journaled before stopping and cannot be replayed", async () => {
  const f = fixture();
  const originalCall = f.sandbox.call;
  f.sandbox.call = async (...args) => {
    await originalCall(...args);
    throw new Error("Sandbox response lost");
  };
  const writing = Promise.withResolvers<void>();
  const confirmation = Promise.withResolvers<void>();
  const recordUnknown = f.bridge.recordUnknown;
  f.bridge.recordUnknown = async () => {
    writing.resolve();
    await confirmation.promise;
    await recordUnknown(f.exec.callId);
  };
  const outcome = f.execution.execute(tool, { command: "original" }, f.exec)
    .then(() => undefined, (error: Error) => error);
  await writing.promise;
  expect(f.stopped).toBe(false);
  confirmation.resolve();
  expect((await outcome)!.message).toContain("outcome is unknown");
  expect(f.stopped).toBe(true);
  expect(f.deferred).toHaveLength(0);
  await expect(f.execution.execute(tool, { command: "original" }, f.exec))
    .rejects.toThrow("reconciliation");
  expect(f.executions).toBe(1);
  expect(f.approvals).toBe(1);
});

test("non-user cancellation still blocks unconfirmed remote outcomes", async () => {
  for (const reason of [{ kind: "disposed" }, { kind: "hook", reason: "other" }, new Error("timeout"), undefined]) {
    const f = fixture();
    const abort = new AbortController();
    f.sandbox.call = async () => { abort.abort(reason); throw new Error("response lost"); };
    await expect(f.execution.execute(tool, { command: "original" }, { ...f.exec, signal: abort.signal }))
      .rejects.toThrow("outcome is unknown");
    expect(f.bridge.blocked()).toBe(true);
    expect(f.stopped).toBe(true);
  }
});

test("user interruption journals a local error without claiming a remote terminal result", async () => {
  const f = fixture();
  const abort = new AbortController();
  let outcome: Parameters<ToolExecutionBridge["recordResult"]>[0] | undefined;
  const recordResult = f.bridge.recordResult;
  f.bridge.recordResult = async (result) => { outcome = result; await recordResult(result); };
  f.sandbox.call = async () => { abort.abort({ kind: "user" }); throw new Error("request aborted"); };
  await expect(f.execution.execute(tool, { command: "original" }, { ...f.exec, signal: abort.signal }))
    .rejects.toThrow("Tool call interrupted.");
  expect(outcome).toMatchObject({ failed: true, text: "Tool call interrupted.", remoteOutcome: "unknown" });
  expect(f.bridge.blocked()).toBe(false);
  expect(f.stopped).toBe(false);
});

test("worker shutdown cancellation journals an interrupted unknown outcome without blocking", async () => {
  const f = fixture();
  const abort = new AbortController();
  let outcome: Parameters<ToolExecutionBridge["recordResult"]>[0] | undefined;
  const recordResult = f.bridge.recordResult;
  f.bridge.recordResult = async (result) => { outcome = result; await recordResult(result); };
  f.sandbox.call = async () => { abort.abort(SHUTDOWN_CANCEL); throw new Error("request aborted"); };
  await expect(f.execution.execute(tool, { command: "original" }, { ...f.exec, signal: abort.signal }))
    .rejects.toThrow("Brain worker shut down");
  expect(outcome).toMatchObject({ failed: true, remoteOutcome: "unknown" });
  expect(f.bridge.blocked()).toBe(false);
  expect(f.stopped).toBe(false);
});

test("a completed remote result wins a user cancellation race", async () => {
  const f = fixture();
  const abort = new AbortController();
  f.sandbox.call = async () => { abort.abort({ kind: "user" }); return { content: [{ type: "text", text: "completed" }] }; };
  let outcome: Parameters<ToolExecutionBridge["recordResult"]>[0] | undefined;
  const recordResult = f.bridge.recordResult;
  f.bridge.recordResult = async (result) => { outcome = result; await recordResult(result); };
  expect(await f.execution.execute(tool, { command: "original" }, { ...f.exec, signal: abort.signal })).toBe("completed");
  expect(outcome?.remoteOutcome).toBeUndefined();
  expect(f.bridge.blocked()).toBe(false);
});

test("failed interruption persistence remains fatal", async () => {
  const f = fixture();
  const abort = new AbortController();
  f.sandbox.call = async () => { abort.abort({ kind: "user" }); throw new Error("request aborted"); };
  f.bridge.recordResult = async () => { throw new Error("Write not confirmed"); };
  await expect(f.execution.execute(tool, { command: "original" }, { ...f.exec, signal: abort.signal }))
    .rejects.toThrow("Write not confirmed");
});

test("user interruption cannot clear an earlier unknown outcome", async () => {
  const f = fixture();
  f.sandbox.call = async () => { throw new Error("response lost"); };
  await expect(f.execution.execute(tool, { command: "original" }, f.exec)).rejects.toThrow("outcome is unknown");
  const abort = new AbortController();
  abort.abort({ kind: "user" });
  await expect(f.execution.execute(tool, { command: "original" }, { ...f.exec, signal: abort.signal }))
    .rejects.toThrow("reconciliation");
  expect(f.bridge.blocked()).toBe(true);
  expect(f.approvals).toBe(1);
});

test("runtime denial preserves the supplied reason without sandbox side effects", async () => {
  for (const reason of ["Use staging instead", undefined]) {
    const f = fixture();
    f.bridge.approve = async () => ({ allowed: false, reason });
    await expect(f.execution.execute(tool, { command: "original" }, f.exec))
      .rejects.toThrow(reason ?? "Tool use denied by OMA");
    expect(f.provisions).toBe(0);
    expect(f.executions).toBe(0);
    expect(f.stopped).toBe(false);
  }
});
