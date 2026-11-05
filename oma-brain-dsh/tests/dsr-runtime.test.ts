import { Context } from "@deepseek-ai/cordis";
import { expect, test, spyOn } from "bun:test";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { OmaApi } from "../src/api.ts";
import { dsrAssignment } from "../src/dsr/assignment.ts";
import { DsrClient } from "../src/dsr/client.ts";
import { DsrSession } from "../src/dsr/session.ts";
import { createDsrTransport } from "../src/dsr/transport.ts";
import { dsrRuntimeServices, mountDsr } from "../src/dsr/worker.ts";
import { omaRuntimeConfig } from "../src/oma-session.ts";
import { eventually, textEvents, toolEvents } from "./backend.ts";
import { DsrBackend } from "./dsr-backend.ts";

async function fixture(
  backend: DsrBackend,
  root: string,
  approvalWaitMs?: number,
) {
  await backend.ready;
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  const clients = await mountDsr(ctx, api, root);
  const client = new DsrClient(createDsrTransport(api), {
    code_session_id: backend.a.session_id,
    session_token: backend.a.ingress_token,
    model_token: backend.a.model_token,
    worker_epoch: String(backend.a.worker_epoch),
    claim_expires_at: new Date(Date.now() + 30000).toISOString(),
  });
  clients.set(backend.a.session_id, client);
  const assignment = dsrAssignment(api, client.claim, await client.register());
  api.assignments.set(assignment.session_id, assignment);
  let session!: DsrSession;
  await ctx.plugin({
    inject: dsrRuntimeServices,
    apply: async (scope: Context) => {
      session = new DsrSession(
        scope,
        client,
        omaRuntimeConfig(api, assignment),
        { approvalWaitMs },
      );
      await session.open();
    },
  });
  return { ctx, session };
}

test("DSR refuses a v1 completion checkpoint before replay or acknowledgement", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-old-finish-"));
  const { ctx, session } = await fixture(backend, root);
  try {
    const { appendPluginRecord } = await import("@deepseek-ai/dsh-session");
    appendPluginRecord(session.runtime.agent.session, "plugin:oma/dsr-finish", {
      version: 1,
      inputId: "old",
      completion: { result: "recovered", reason: "interrupted_recovery" },
    } as never);
    await expect(
      session.receive({
        id: "old",
        kind: "user.message",
        lane: "task",
        content: [{ type: "text", text: "old" }],
      }),
    ).rejects.toMatchObject({ code: "unsupported_finish_version" });
    expect(backend.requests).toHaveLength(0);
    expect(backend.processed).toHaveLength(0);
    expect(backend.finishes.size).toBe(0);
  } finally {
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("DSR performs a native model turn, persists output, finishes and ACKs without CCR worker calls", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-runtime-"));
  const { ctx, session } = await fixture(backend, root);
  try {
    await session.receive({
      id: "input_1",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "hello" }],
    });
    expect(backend.finishes.get("input_1")).toEqual({ result: "success" });
    expect(backend.processed).toEqual(["input_1"]);
    expect(
      [...backend.publicLog.values()].filter(
        (event) => event.type === "agent.message",
      ),
    ).toHaveLength(1);
    expect(
      backend.transcripts
        .get(session.id)!
        .events.some((event) => event.type === "turn/end"),
    ).toBe(true);
    await session.receive({
      id: "input_1",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "hello" }],
    });
    expect(backend.requests).toHaveLength(1);
  } finally {
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("DSR confirms the dispatch marker before a real tool body and durably settles before the next model call", async () => {
  const backend = new DsrBackend();
  backend.modelScript = [toolEvents("printf dsr"), textEvents("finished")];
  backend.onTool = async () => {
    const log = backend.transcripts.get(backend.a.session_id)!.events;
    expect(
      log.some((event) => event.type === "plugin:oma/dsr-dispatched"),
    ).toBe(true);
    return { content: [{ type: "text", text: "dsr" }] };
  };
  const root = await mkdtemp(join(tmpdir(), "dsr-tool-"));
  const { ctx, session } = await fixture(backend, root);
  try {
    await session.receive({
      id: "input_tool",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "run a command" }],
    });
    expect(backend.executions).toHaveLength(1);
    expect(backend.finishes.get("input_tool")).toEqual({ result: "success" });
    expect(
      [...backend.publicLog.values()].some(
        (event) => event.type === "tool.result",
      ),
    ).toBe(true);
    expect(backend.permissions.size).toBe(1);
  } finally {
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("DSR suspends an approval without closing its native turn, then continues after cold takeover", async () => {
  const backend = new DsrBackend();
  backend.permissionPending = true;
  backend.modelScript = [
    toolEvents("printf continued"),
    textEvents("continued"),
  ];
  const firstRoot = await mkdtemp(join(tmpdir(), "dsr-suspend-"));
  const secondRoot = await mkdtemp(join(tmpdir(), "dsr-continue-"));
  const input = {
    id: "input_suspend",
    kind: "user.message" as const,
    lane: "task" as const,
    content: [{ type: "text", text: "run after approval" }],
  };
  const first = await fixture(backend, firstRoot, 0);
  try {
    await first.session.receive(input);
    expect(first.session.suspended).toBe(true);
    expect(backend.executions).toHaveLength(0);
    expect(backend.finishes.size).toBe(0);
    expect(
      backend.transcripts
        .get(first.session.id)!
        .events.some((event) => event.type === "turn/end"),
    ).toBe(false);
  } finally {
    await first.session.close();
    await first.ctx.fiber.dispose();
  }
  backend.permissionPending = false;
  backend.a.worker_epoch = String(BigInt(backend.a.worker_epoch) + 1n);
  const second = await fixture(backend, secondRoot);
  try {
    await second.session.receive(input);
    expect(backend.executions).toHaveLength(1);
    expect(backend.requests).toHaveLength(2);
    expect(backend.finishes.get(input.id)).toEqual({ result: "success" });
    expect(
      backend.transcripts
        .get(second.session.id)!
        .events.filter((event) => event.type === "turn/start"),
    ).toHaveLength(1);
  } finally {
    await second.session.close();
    await second.ctx.fiber.dispose();
    backend.stop();
    await rm(firstRoot, { recursive: true, force: true });
    await rm(secondRoot, { recursive: true, force: true });
  }
});

test("DSR SIGKILL resumes the entire tool batch, never replays a dispatched call and executes the remaining call once", async () => {
  const backend = new DsrBackend();
  await backend.ready;
  backend.hangTool = true;
  const first = toolEvents("first", "call_first");
  const second = toolEvents("second", "call_second");
  backend.modelScript = [
    [
      ...first.slice(0, 4),
      ...second.slice(1, 4).map((event) => ({ ...event, index: 1 })),
      ...first.slice(4),
    ],
    textEvents("recovered"),
  ];
  const firstRoot = await mkdtemp(join(tmpdir(), "dsr-crash-before-"));
  const secondRoot = await mkdtemp(join(tmpdir(), "dsr-crash-after-"));
  const claim = {
    code_session_id: backend.a.session_id,
    session_token: backend.a.ingress_token,
    model_token: backend.a.model_token,
    worker_epoch: String(backend.a.worker_epoch),
    claim_expires_at: new Date(Date.now() + 30000).toISOString(),
  };
  const child = spawn(process.execPath, ["tests/dsr-runtime-worker.ts"], {
    env: {
      ...process.env,
      TEST_API_URL: backend.url,
      TEST_DSR_ROOT: firstRoot,
      TEST_DSR_CLAIM: JSON.stringify(claim),
    },
    stdio: ["ignore", "ignore", "pipe"],
  });
  let output = "";
  child.stderr.on("data", (data) => {
    output += String(data);
  });
  const exit = once(child, "exit");
  let resumed: Awaited<ReturnType<typeof fixture>> | undefined;
  try {
    await eventually(() => backend.executions.length === 1);
    expect(output).not.toContain("error:");
    child.kill("SIGKILL");
    await exit;
    backend.hangTool = false;
    backend.a.worker_epoch = String(BigInt(backend.a.worker_epoch) + 1n);
    resumed = await fixture(backend, secondRoot);
    await resumed.session.receive({
      id: "input_crash",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "run both tools" }],
    });
    expect(backend.executions.map((call) => call.arguments.command)).toEqual([
      "first",
      "second",
    ]);
    expect(backend.requests).toHaveLength(2);
    expect(JSON.stringify(backend.requests[1])).toContain(
      "TOOL_OUTCOME_UNKNOWN",
    );
    expect(backend.finishes.get("input_crash")).toEqual({ result: "success" });
    expect(
      backend.transcripts
        .get(backend.a.session_id)!
        .events.filter((event) => event.type === "turn/start"),
    ).toHaveLength(1);
  } finally {
    child.kill("SIGKILL");
    await exit;
    await resumed?.session.close();
    await resumed?.ctx.fiber.dispose();
    backend.stop();
    await rm(firstRoot, { recursive: true, force: true });
    await rm(secondRoot, { recursive: true, force: true });
  }
});

test("DSR applies a durable interrupt before continuing a recovered approval", async () => {
  const backend = new DsrBackend();
  backend.permissionPending = true;
  backend.modelScript = [
    toolEvents("must not execute"),
    textEvents("must not request"),
  ];
  const root = await mkdtemp(join(tmpdir(), "dsr-interrupt-before-"));
  const cold = await mkdtemp(join(tmpdir(), "dsr-interrupt-after-"));
  const input = {
    id: "interrupted_input",
    kind: "user.message" as const,
    lane: "task" as const,
    content: [{ type: "text", text: "wait for approval" }],
  };
  const first = await fixture(backend, root, 0);
  await first.session.receive(input);
  await first.session.close();
  await first.ctx.fiber.dispose();
  backend.cancelRequested = true;
  backend.a.worker_epoch = String(BigInt(backend.a.worker_epoch) + 1n);
  const second = await fixture(backend, cold);
  try {
    await second.session.receive(input);
    expect(backend.executions).toHaveLength(0);
    expect(backend.requests).toHaveLength(1);
    expect(backend.finishes.get(input.id)).toEqual({ result: "cancelled" });
    expect(backend.processed).toContain(input.id);
  } finally {
    await second.session.close();
    await second.ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
    await rm(cold, { recursive: true, force: true });
  }
});

test("registration configuration remains fixed across turns", async () => {
  const backend = new DsrBackend();
  backend.modelScript = [textEvents("first"), textEvents("second")];
  const root = await mkdtemp(join(tmpdir(), "dsr-frozen-"));
  const { ctx, session } = await fixture(backend, root);
  try {
    const delivery = spyOn(session.client, "delivery");
    await session.control({
      id: "unknown-control",
      kind: "session.set_model",
      lane: "control",
      model: "changed",
    } as never);
    expect(
      delivery.mock.calls.some(([updates]) =>
        updates.some(
          (update) =>
            update.id === "unknown-control" &&
            update.error === "unsupported_input",
        ),
      ),
    ).toBe(true);
    backend.a.launch.models = {
      changed: { id: "changed", api_backend: "messages" },
    };
    backend.a.launch.tools = [];
    backend.a.launch.tool_permissions = { bash: "deny" };
    await session.receive({
      id: "first",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "first" }],
    });
    await session.receive({
      id: "second",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "second" }],
    });
    expect(backend.requests).toHaveLength(2);
    expect(session.config.tools).toContain("Bash");
    expect(session.runtime.config.model).toBe("test-model");
    expect(
      [...backend.routes].some((route) => route.endsWith("/worker-state")),
    ).toBe(false);
    expect(
      [...backend.routes].some((route) => route.endsWith("/context")),
    ).toBe(false);
  } finally {
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});
