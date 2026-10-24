import { completedInputs } from "./session-log.ts";
import { modelSystemText } from "./backend.ts";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import type { CcrSession } from "../src/ccrv2/index.ts";
import { Backend, eventually, toolEvents, textEvents } from "./backend.ts";

test("SIGKILL during sandbox execution restores logs, never replays and continues on new input", async () => {
  const backend = new Backend();
  backend.modelScript = [toolEvents()];
  backend.hangTool = true;
  await backend.ready;
  const child = spawn(process.execPath, ["tests/crash-worker.ts"], {
    env: {
      ...process.env,
      TEST_API_URL: backend.url,
      TEST_ASSIGNMENT: JSON.stringify(backend.a),
    },
    stdio: ["pipe", "pipe", "pipe"],
  });
  const exited = once(child, "exit");
  backend.onApproval = async (p) => {
    child.stdin!.write(
      JSON.stringify({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: p.request_id,
          response: {
            behavior: "allow",
            updatedInput: { command: "approved-before-crash" },
          },
        },
      }) + "\n",
    );
  };
  let ctx: Context | undefined;
  let resumed: CcrSession | undefined;
  try {
    await eventually(() => backend.executions.length === 1);
    child.kill("SIGKILL");
    await exited;
    backend.a.worker_epoch++;
    ctx = new Context();
    const api = new OmaApi(backend.url);
    await mountOma(ctx, api);
    resumed = createOmaSession(ctx, api, backend.a);
    await resumed.open();
    expect(completedInputs(resumed)).toContain("crash_input");
    await resumed.receive("crash_input", {
      message: { content: "execute a command" },
    });
    expect(resumed.abort.signal.aborted).toBe(false);
    expect(backend.requests).toHaveLength(1);
    backend.hangTool = false;
    backend.modelScript = [textEvents("checked state")];
    await resumed.receive("new_input", { message: { content: "what happened?" } });
    expect(resumed.abort.signal.aborted).toBe(false);
    expect(backend.executions).toHaveLength(1);
    expect(backend.requests).toHaveLength(2);
    expect(JSON.stringify(backend.requests[1]!.messages)).toContain("Do not retry blindly");
    expect(backend.publicLog.get("new_input_result")?.is_error).toBe(false);
    expect(
      [...backend.publicLog.values()].some((p) => p.type === "result" && p.is_error === true),
    ).toBe(true);
    const closers = [...backend.privateLog.values()]
      .filter((p) => p.type === "dsh_log" && "seq" in p.data)
      .map((p) => p.data);
    expect(
      closers.some((e) => e.type === "tool/result" && e.data.message.isError),
    ).toBe(true);
  } finally {
    child.kill("SIGKILL");
    await exited;
    await resumed?.close();
    await ctx?.fiber.dispose();
    backend.stop();
  }
});

function delegates(): import("../src/json.ts").Payload[] {
  const blocks = ["alpha", "beta"].flatMap((name, index) => toolEvents("", `delegate_${name}`).slice(1, 4).map((event) => ({
    ...event, index,
    ...(event.type === "content_block_start" ? { content_block: { ...event.content_block, name: "send_to_agent" } } : {}),
    ...(event.type === "content_block_delta" ? { delta: { type: "input_json_delta", partial_json: JSON.stringify({ name, system_prompt: name, message: `work ${name}` }) } } : {}),
  })));
  return [toolEvents()[0]!, ...blocks, ...toolEvents().slice(4)];
}

for (const duringDispatch of [true, false]) {
  test(`SIGKILL with parallel children ${duringDispatch ? "repairs an unknown call alongside a confirmed sibling" : "repairs approval waits without replay and allows a fresh input"}`, async () => {
    const backend = new Backend();
    const counts = new Map<string, number>();
    backend.onModel = async (request) => {
      const name = modelSystemText(request).includes("You are subagent alpha.") ? "alpha" : modelSystemText(request).includes("You are subagent beta.") ? "beta" : "primary";
      const count = counts.get(name) ?? 0; counts.set(name, count + 1);
      return name === "primary" ? (count === 0 ? delegates() : textEvents("new turn"))
        : count === 0 ? toolEvents(name, "same_call_id") : textEvents(`${name} report`);
    };
    backend.onTool = async (params) => {
      if (params.arguments.command === "alpha") return await new Promise(() => {});
      return { content: [{ type: "text", text: "confirmed beta output" }] };
    };
    await backend.ready;
    const child = spawn(process.execPath, ["tests/crash-worker.ts"], {
      env: { ...process.env, TEST_API_URL: backend.url, TEST_ASSIGNMENT: JSON.stringify(backend.a) },
      stdio: ["pipe", "pipe", "pipe"],
    });
    const exited = once(child, "exit");
    const approvals: import("../src/json.ts").Payload[] = [];
    backend.onApproval = async (request) => {
      if (["list_agents", "send_to_agent", "archive_agent"].includes(request.request.tool_name)) {
        child.stdin!.write(JSON.stringify({ type: "control_response", response: {
          subtype: "success", request_id: request.request_id, response: { behavior: "allow" },
        } }) + "\n");
        return;
      }
      approvals.push(request);
      if (duringDispatch) child.stdin!.write(JSON.stringify({ type: "control_response", response: {
        subtype: "success", request_id: request.request_id, response: { behavior: "allow" },
      } }) + "\n");
    };
    let ctx: Context | undefined;
    let resumed: CcrSession | undefined;
    try {
      const created = () => [...backend.publicLog.values()].filter((event) => event.type === "session.thread_created");
      if (duringDispatch) await eventually(() => backend.executions.length === 2 && [...backend.publicLog.values()]
        .some((event) => event.type === "agent.thread_message_received" && event.from_agent_name === "beta"));
      else await eventually(() => approvals.length === 2);
      const alphaId = created().find((event) => event.agent_name === "alpha")!.session_thread_id;
      const betaId = created().find((event) => event.agent_name === "beta")!.session_thread_id;
      child.kill("SIGKILL"); await exited;
      backend.a.worker_epoch++;
      ctx = new Context();
      const api = new OmaApi(backend.url);
      await mountOma(ctx, api);
      resumed = createOmaSession(ctx, api, backend.a);
      const requestCount = backend.requests.length;
      await resumed.open();
      expect(backend.requests).toHaveLength(requestCount);
      expect(completedInputs(resumed)).toContain("crash_input");
      expect(resumed.subagents.threads!.every((thread) => thread.status === "idle")).toBe(true);
      expect(resumed.abort.signal.aborted).toBe(false);
      const native = (threadId: string) => [...backend.privateLog.values()].filter((entry) => entry.type === "dsh_log"
        && backend.privateAgentIds.get(entry.uuid) === threadId && "seq" in entry.data).map((entry) => entry.data);
      const alpha = native(alphaId);
      expect(alpha.some((event) => event.type === "tool/result" && event.data.message.isError)).toBe(true);
      expect(alpha.some((event) => event.type === "turn/end" && event.data.reason.kind === "interrupted")).toBe(true);
      if (duringDispatch) expect(native(betaId).some((event) => event.type === "tool/result"
        && JSON.stringify(event.data.message).includes("confirmed beta output"))).toBe(true);
      await resumed.receive("fresh", { content: "new independent task" });
      expect(backend.requests).toHaveLength(requestCount + 1);
      expect(backend.executions).toHaveLength(duringDispatch ? 2 : 0);
      expect(resumed).not.toHaveProperty("delivery");
    } finally {
      child.kill("SIGKILL"); await exited;
      await resumed?.close(); await ctx?.fiber.dispose(); backend.stop();
    }
  });
}
