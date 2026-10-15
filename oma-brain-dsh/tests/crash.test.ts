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

test("SIGKILL during sandbox execution restores logs and blocks replay", async () => {
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
    expect(resumed.state.phase).toBe("blocked");
    expect(resumed.state.completed).toContain("crash_input");
    expect(resumed.state.approvedCalls.call_1!.effective).toEqual({
      command: "approved-before-crash",
    });
    await resumed.receive("crash_input", {
      message: { content: "execute a command" },
    });
    await expect(
      resumed.receive("new_input", { message: { content: "run again" } }),
    ).rejects.toThrow("reconciliation");
    expect(backend.executions).toHaveLength(1);
    expect(backend.requests).toHaveLength(1);
    expect(
      [...backend.publicLog.values()].some((p) => p.type === "result" && p.is_error === true),
    ).toBe(true);
    const closers = [...backend.privateLog.values()]
      .filter((p) => p.subtype === "dsh_log")
      .flatMap((p) => p.data.events);
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
  test(`SIGKILL with parallel children ${duringDispatch ? "keeps an unknown dispatch blocked alongside a confirmed sibling" : "repairs approval waits without replay and allows a fresh input"}`, async () => {
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
      approvals.push(request);
      if (duringDispatch) child.stdin!.write(JSON.stringify({ type: "control_response", response: {
        subtype: "success", request_id: request.request_id, response: { behavior: "allow" },
      } }) + "\n");
    };
    let ctx: Context | undefined;
    let resumed: CcrSession | undefined;
    try {
      const latest = () => [...backend.privateLog.values()].filter((entry) => entry.subtype === "dsh_brain_state").at(-1)?.data;
      if (duringDispatch) await eventually(() => {
        const snapshot = latest();
        const beta = snapshot?.threads?.find((thread: { name: string }) => thread.name === "beta");
        return beta?.status === "idle" && backend.executions.length === 2;
      });
      else await eventually(() => approvals.length === 2);
      const snapshot = latest()!;
      const alphaId = snapshot.threads.find((thread: { name: string }) => thread.name === "alpha").id;
      const betaId = snapshot.threads.find((thread: { name: string }) => thread.name === "beta").id;
      if (duringDispatch) {
        expect(snapshot.executions[alphaId].phase).toBe("dispatch");
        expect(snapshot.executions[betaId].phase).toBe("completed");
        expect(snapshot.executions[betaId].toolOutcomes.same_call_id.text).toBe("confirmed beta output");
      }
      child.kill("SIGKILL"); await exited;
      backend.a.worker_epoch++;
      ctx = new Context();
      const api = new OmaApi(backend.url);
      await mountOma(ctx, api);
      resumed = createOmaSession(ctx, api, backend.a);
      const requestCount = backend.requests.length;
      await resumed.open();
      expect(backend.requests).toHaveLength(requestCount);
      expect(resumed.state.completed).toContain("crash_input");
      expect(resumed.state.threads!.every((thread) => thread.status === "idle")).toBe(true);
      if (duringDispatch) {
        expect(resumed.blocked()).toBe(true);
        expect(resumed.state.executions![alphaId]!.phase).toBe("blocked");
        expect(resumed.state.executions![betaId]!.phase).toBe("completed");
        await expect(resumed.receive("retry", { content: "retry command" })).rejects.toThrow("reconciliation");
        const repairs = [...backend.privateLog.values()].filter((entry) => entry.subtype === "dsh_log" && entry.data.header.id === alphaId).flatMap((entry) => entry.data.events);
        expect(repairs.some((event) => event.type === "tool/result" && event.data.message.isError)).toBe(true);
        expect(backend.executions).toHaveLength(2);
      } else {
        expect(resumed.blocked()).toBe(false);
        expect(resumed.state.executions![alphaId]!.phase).toBe("interrupted");
        expect(resumed.state.executions![betaId]!.phase).toBe("interrupted");
        await resumed.receive("fresh", { content: "new independent task" });
        expect(backend.requests).toHaveLength(requestCount + 1);
        expect(backend.executions).toHaveLength(0);
      }
    } finally {
      child.kill("SIGKILL"); await exited;
      await resumed?.close(); await ctx?.fiber.dispose(); backend.stop();
    }
  });
}
