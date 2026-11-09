import { expect, test } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import type { CcrSession } from "../src/ccrv2/session.ts";
import { Backend, eventually, modelSystemText, textEvents, toolEvents } from "./backend.ts";

test("SIGTERM cannot complete a parent successfully while its child is still running", async () => {
  const backend = new Backend();
  await backend.ready;
  const directory = await mkdtemp(join(tmpdir(), "oma-subagent-shutdown-"));
  let childStarted = false;
  let parentRequests = 0;
  backend.onModel = async request => {
    if (modelSystemText(request).includes("# Subagent rules")) {
      childStarted = true;
      return new Promise<never>(() => {});
    }
    if (parentRequests++ > 0) return textEvents("Root turn completed; child still working");
    return toolEvents("", "delegate-shutdown").map(event => event.type === "content_block_start"
      ? { ...event, content_block: { ...event.content_block, name: "subagent" } }
      : event.type === "content_block_delta"
        ? { ...event, delta: { type: "input_json_delta", partial_json: JSON.stringify({ description: "Slow child", prompt: "Keep working" }) } }
        : event);
  };
  backend.onDelegationApproval = async payload => {
    backend.deliver(`approval_${crypto.randomUUID()}`, { type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  const child = Bun.spawn([process.execPath, process.env.BRAIN_TEST_ENTRY ?? "src/main.ts"], {
    env: { ...process.env, OMA_BRAIN_TRANSCRIPT_DIR: directory, OMA_API_URL: backend.url,
      OMA_BRAIN_CLAIM_TOKEN: backend.claimToken, OMA_BRAIN_SHUTDOWN_DRAIN_MS: "0" },
    stdin: "ignore", stdout: "pipe", stderr: "pipe",
  });
  const stdout = new Response(child.stdout).text();
  const stderr = new Response(child.stderr).text();
  const inputId = `task_${crypto.randomUUID()}`;
  let ctx: Context | undefined;
  let resumed: CcrSession | undefined;
  try {
    backend.queueClaim();
    await eventually(() => backend.connections === 1);
    backend.deliver(inputId, { type: "user", message: { content: "Delegate, then finish your own turn" } });
    await eventually(() => childStarted && [...backend.privateLog.values()].some(payload =>
      !backend.privateAgentIds.has(payload.uuid) && payload.data?.type === "turn/end"
      && payload.data.data.reason.kind === "completed"));
    expect(backend.executions).toHaveLength(0);
    expect([...backend.publicLog.values()].some(event => event.type === "result")).toBe(false);
    child.kill("SIGTERM");
    expect(await child.exited).toBe(0);
    expect(await stderr).toBe("");
    const completions = [...backend.privateLog.values()].filter(payload =>
      !backend.privateAgentIds.has(payload.uuid) && payload.data?.type === "plugin:oma/input-completion");
    expect(completions.some(payload => payload.data.data.outcome.kind === "success")).toBe(false);
    expect([...backend.publicLog.values()].some(event => event.type === "result" && event.is_error === false)).toBe(false);
    expect(backend.processed).not.toContain(inputId);
    expect(backend.releases).toHaveLength(1);
    expect(backend.releases[0]!.reason).toBe("shutdown");
    const requests = backend.requests.length;
    backend.a.worker_epoch++;
    ctx = new Context();
    const api = new OmaApi(backend.url);
    await mountOma(ctx, api);
    resumed = createOmaSession(ctx, api, backend.a);
    await resumed.open();
    await resumed.receive(inputId, { content: "Duplicate input after shutdown" });
    const results = [...backend.publicLog.values()].filter(event => event.type === "result");
    expect(results).toHaveLength(1);
    expect(results[0]!.is_error).toBe(true);
    expect(results[0]!.uuid).toBe(`${inputId}_recovery_result`);
    expect(backend.requests).toHaveLength(requests);
    expect(backend.processed).toContain(inputId);
  } finally {
    if (child.exitCode === null) child.kill("SIGKILL");
    await child.exited;
    await stdout;
    await stderr;
    await resumed?.close();
    await ctx?.fiber.dispose();
    backend.stop();
    await rm(directory, { recursive: true, force: true });
  }
}, 15_000);
