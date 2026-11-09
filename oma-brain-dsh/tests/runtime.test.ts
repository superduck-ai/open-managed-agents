import { expect, test } from "bun:test";
import { HarnessError, ToolCallId } from "@deepseek-ai/dsh-llm";
import { ToolExecution } from "../src/runtime/execution.ts";
import { RetryableInputError, SHUTDOWN_CANCEL, ToolNotForwardedError } from "../src/runtime/contracts.ts";
import type { RemoteTool, SandboxExecutor, ToolExecutionBridge, ToolExecutionContext } from "../src/runtime/contracts.ts";

const tool: RemoteTool = {
  name: "Bash", remoteName: "bash", description: "Test execution",
  inputSchema: { type: "object", properties: { command: { type: "string" } }, required: ["command"], additionalProperties: false },
};

function fixture() {
  let executions = 0;
  let provisions = 0;
  const changes: unknown[] = [];
  const deferred: unknown[] = [];
  const sandbox: SandboxExecutor = {
    async provision() { provisions++; },
    async call(_tool, args) { executions++; expect(args).toEqual({ command: "approved" }); return { content: [{ type: "text", text: "stdout" }] }; },
  };
  const bridge: ToolExecutionBridge = {
    async approve() { return { allowed: true, input: { command: "approved" } }; },
    runTool: (_id, execute) => execute(),
    approvalChanged: (...args) => { changes.push(args); },
  };
  const exec: ToolExecutionContext = { callId: ToolCallId("call_1"), signal: new AbortController().signal, deferContext: (message) => { deferred.push(message); } };
  return { sandbox, bridge, exec, changes, deferred, execution: new ToolExecution(sandbox, bridge), get executions() { return executions; }, get provisions() { return provisions; } };
}

test("denied approval prevents sandbox effects", async () => {
  for (const reason of ["Use staging instead", undefined]) {
    const f = fixture();
    f.bridge.approve = async () => ({ allowed: false, reason });
    await expect(f.execution.execute(tool, { command: "original" }, f.exec)).rejects.toThrow(reason ?? "Tool use denied by OMA");
    expect(f.executions).toBe(0);
    expect(f.provisions).toBe(0);
  }
});

test("drain admission rejection prevents remote execution", async () => {
  const f = fixture();
  f.bridge.runTool = async () => { throw new ToolNotForwardedError("shutting down"); };
  await expect(f.execution.execute(tool, { command: "original" }, f.exec)).rejects.toThrow("shutting down");
  expect(f.executions).toBe(0);
});

test("unknown non-read-only outcomes are not retried and return conservative errors to DSH", async () => {
  const f = fixture();
  let attempts = 0;
  f.sandbox.call = async () => { attempts++; throw new Error("response lost"); };
  await expect(f.execution.execute(tool, { command: "original" }, f.exec)).rejects.toThrow("Do not retry blindly");
  expect(attempts).toBe(1);
  expect(f.deferred).toEqual([]);
  expect(f.bridge).not.toHaveProperty("recordDispatch");
  expect(f.bridge).not.toHaveProperty("recordResult");
});

test("read-only failures retry once before returning a tool error", async () => {
  const f = fixture();
  let attempts = 0;
  f.sandbox.call = async () => { attempts++; throw new Error("unavailable"); };
  await expect(f.execution.execute({ ...tool, readOnly: true }, { command: "original" }, f.exec)).rejects.toThrow("Read-only tool outcome is unknown");
  expect(attempts).toBe(2);
});

test("user and shutdown cancellation preserve uncertainty without a second journal", async () => {
  for (const [reason, text] of [[{ kind: "user" }, "Tool call interrupted."], [SHUTDOWN_CANCEL, "outcome is unknown"]] as const) {
    const f = fixture();
    const abort = new AbortController();
    f.sandbox.call = async () => { abort.abort(reason); throw new Error("aborted"); };
    await expect(f.execution.execute(tool, { command: "original" }, { ...f.exec, signal: abort.signal })).rejects.toThrow(text);
    expect(f.changes).toHaveLength(1);
  }
});

test("a completed remote result wins a cancellation race", async () => {
  const f = fixture();
  const abort = new AbortController();
  f.sandbox.call = async () => { abort.abort({ kind: "user" }); return { content: [{ type: "text", text: "completed" }] }; };
  expect(await f.execution.execute(tool, { command: "original" }, { ...f.exec, signal: abort.signal })).toBe("completed");
});

test("approved argument changes remain visible without duplicating tool results", async () => {
  const f = fixture();
  expect(await f.execution.execute(tool, { command: "original" }, f.exec)).toBe("stdout");
  expect(f.changes).toEqual([["Bash", "call_1", { command: "approved" }]]);
  expect(f.deferred).toHaveLength(1);
  expect(f.executions).toBe(1);
});

test("read-only retry preserves its originating child thread identity", async () => {
  const f = fixture();
  const threads: (string | undefined)[] = [];
  f.sandbox.call = async (_tool, _args, _signal, threadId) => {
    threads.push(threadId);
    if (threads.length === 1) throw new Error("transient read failure");
    return { content: [{ type: "text", text: "confirmed" }] };
  };
  const execution = new ToolExecution(f.sandbox, f.bridge, undefined, "child-scope");
  expect(await execution.execute({ ...tool, readOnly: true }, { command: "original" }, f.exec)).toBe("confirmed");
  expect(threads).toEqual(["child-scope", "child-scope"]);
});


test("content conversion preserves retryable errors and their structured tool code", async () => {
  const f = fixture();
  const failure = new RetryableInputError("Storage temporarily unavailable; retry Read");
  await expect(f.execution.execute({ ...tool, readOnly: true }, { command: "original" }, f.exec,
    async () => { throw failure; })).rejects.toBe(failure);
  expect(failure).toBeInstanceOf(HarnessError);
  expect(failure.code).toBe("RETRYABLE_INPUT");
  expect(f.executions).toBe(1);
});
