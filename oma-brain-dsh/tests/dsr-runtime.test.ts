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
  const clients = await mountDsr(ctx, api);
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
  return { ctx, session, client };
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

test("DSR performs a native model turn, persists output, finishes without task ACKs without CCR worker calls", async () => {
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
    expect(backend.processed).toEqual([]);
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
    backend.deliver(input.id, input);
    await second.client.claimInput();
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
    backend.deliver(input.id, input);
    await second.client.claimInput();
    await second.session.receive(input);
    expect(backend.executions).toHaveLength(0);
    expect(backend.requests).toHaveLength(1);
    expect(backend.finishes.get(input.id)).toEqual({ result: "cancelled" });
    expect(backend.processed).toEqual([]);
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

test("DSR model requests use one checkpoint and one replication barrier", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-request-barrier-"));
  const { ctx, session } = await fixture(backend, root);
  const { SessionReplication } = await import("../src/runtime/replication.ts");
  const checkpoint = spyOn(session, "checkpoint");
  const flush = spyOn(SessionReplication.prototype, "flush");
  const counts: { checkpoints: number; barriers: number }[] = [];
  backend.onModel = async () => {
    counts.push({
      checkpoints: checkpoint.mock.calls.length,
      barriers: flush.mock.calls.length,
    });
    return textEvents("done");
  };
  try {
    await session.receive({
      id: "one_barrier",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "hello" }],
    });
    expect(counts).toEqual([{ checkpoints: 1, barriers: 1 }]);
    expect(backend.finishes.get("one_barrier")).toEqual({ result: "success" });
  } finally {
    checkpoint.mockRestore();
    flush.mockRestore();
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("DSR child requests settle events before a single durable checkpoint and flush notification", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-child-barrier-"));
  const { ctx, session } = await fixture(backend, root);
  const { SessionReplication } = await import("../src/runtime/replication.ts");
  const { appendPluginRecord } = await import("@deepseek-ai/dsh-session");
  const thread = {
    id: "child-barrier",
    name: "child",
    inline: true,
    systemPrompt: "",
    model: session.config.model,
    status: "idle" as const,
  };
  const factory = session.subagents as unknown as {
    createRuntime(
      thread: import("../src/runtime/contracts.ts").SubagentThread,
    ): Promise<import("../src/runtime/session.ts").SessionRuntime>;
  };
  ctx.sessionReplication.bindThread(thread.id, session.id);
  const unbind = session.config.bindThread?.(thread.id);
  const child = await factory.createRuntime(thread);
  await child.open();
  const checkpoint = spyOn(session, "checkpoint");
  const flush = spyOn(SessionReplication.prototype, "flush");
  const settle = spyOn(session.subagents, "settleEvents").mockImplementation(
    async () => {
      appendPluginRecord(child.agent.session, "plugin:oma/dsr-dispatched", {
        version: 1,
        callId: "settled-before-request",
      });
    },
  );
  let notifications = 0;
  const stopListening = ctx.on("session/flush", (value) => {
    if (value.id === thread.id) notifications++;
  });
  const counts: {
    checkpoints: number;
    barriers: number;
    settles: number;
    notifications: number;
    durable: boolean;
  }[] = [];
  backend.onModel = async () => {
    counts.push({
      checkpoints: checkpoint.mock.calls.length,
      barriers: flush.mock.calls.length,
      settles: settle.mock.calls.length,
      notifications,
      durable: backend.transcripts
        .get(thread.id)!
        .events.some(
          (event) =>
            event.type === "plugin:oma/dsr-dispatched" &&
            event.data.callId === "settled-before-request",
        ),
    });
    return textEvents("child done");
  };
  try {
    await child.run("hello", {
      beforeRun: async () => {},
      afterRun: async () => {},
    });
    expect(counts).toEqual([
      {
        checkpoints: 1,
        barriers: 1,
        settles: 1,
        notifications: 1,
        durable: true,
      },
    ]);
  } finally {
    checkpoint.mockRestore();
    flush.mockRestore();
    settle.mockRestore();
    stopListening();
    await child.close();
    unbind?.();
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("native tool calls outside the DSR executor retain one durability barrier", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-tool-barrier-"));
  const { ctx, session } = await fixture(backend, root);
  const { SessionReplication } = await import("../src/runtime/replication.ts");
  const { appendPluginRecord } = await import("@deepseek-ai/dsh-session");
  const { ToolCallId } = await import("@deepseek-ai/dsh-llm");
  const flush = spyOn(SessionReplication.prototype, "flush");
  let durable = false;
  let barriers = 0;
  backend.onTool = async () => {
    barriers = flush.mock.calls.length;
    durable = backend.transcripts
      .get(session.id)!
      .events.some(
        (event) =>
          event.type === "plugin:oma/dsr-dispatched" &&
          event.data.callId === "native-prefix",
      );
    return { content: [{ type: "text", text: "done" }] };
  };
  try {
    session.config.toolPermissions!.bash = "allow";
    appendPluginRecord(
      session.runtime.agent.session,
      "plugin:oma/dsr-dispatched",
      { version: 1, callId: "native-prefix" },
    );
    const result = await session.runtime.agent.ctx.tools.execute({
      agent: session.runtime.agent,
      signal: session.abort.signal,
      calls: [
        {
          type: "tool-call",
          id: ToolCallId("native-call"),
          name: "Bash",
          arguments: JSON.stringify({ command: "printf done" }),
        },
      ],
    });
    expect(result.results[0]!.result.isError).not.toBe(true);
    expect(durable).toBe(true);
    expect(barriers).toBe(1);
  } finally {
    flush.mockRestore();
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("DSR executes reused provider tool IDs independently across model requests", async () => {
  const backend = new (class extends DsrBackend {
    override async route(request: Request): Promise<Response> {
      const response = await super.route(request);
      if (new URL(request.url).pathname === "/v1/messages")
        response.headers.set("request-id", `model-${this.requests.length}`);
      return response;
    }
  })();
  backend.modelScript = [
    toolEvents("first", "reused"),
    toolEvents("second", "reused"),
    textEvents("finished"),
  ];
  const root = await mkdtemp(join(tmpdir(), "dsr-scoped-tools-"));
  const { ctx, session } = await fixture(backend, root);
  try {
    await session.receive({
      id: "input_reused",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "run both" }],
    });
    expect(backend.executions.map((call) => call.arguments.command)).toEqual([
      "first",
      "second",
    ]);
    expect(backend.permissions.size).toBe(2);
    const results = [...backend.publicLog.values()].filter(
      (event) => event.type === "tool.result",
    );
    expect(results).toHaveLength(2);
    expect(results[0]!.tool_use_id).not.toBe(results[1]!.tool_use_id);
    expect(JSON.stringify(backend.requests)).not.toContain(
      "TOOL_OUTCOME_UNKNOWN",
    );
    expect(backend.finishes.get("input_reused")).toEqual({ result: "success" });
  } finally {
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("DSR publication failure prevents finish and cold takeover repairs without another model request", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-publish-recovery-"));
  const input = {
    id: "recover_publish",
    kind: "user.message" as const,
    lane: "task" as const,
    content: [{ type: "text", text: "hello" }],
  };
  const first = await fixture(backend, root);
  const publish = first.client.publish.bind(first.client);
  const rejected = spyOn(first.client, "publish").mockImplementation(
    async (events, signal) => {
      if (events.some((event) => event.type === "agent.message")) {
        expect(
          backend.transcripts
            .get(first.session.id)!
            .events.some(
              (event) =>
                event.type === "assistant/message" ||
                event.type === "plugin:oma/assistant-block",
            ),
        ).toBe(true);
        throw new Error("publication unavailable");
      }
      await publish(events, signal);
    },
  );
  try {
    await expect(first.session.receive(input)).rejects.toThrow(
      "publication unavailable",
    );
    expect(backend.finishes.size).toBe(0);
    expect(backend.requests).toHaveLength(1);
  } finally {
    rejected.mockRestore();
    await first.session.close().catch(() => {});
    await first.ctx.fiber.dispose();
  }
  const second = await fixture(backend, root);
  try {
    await second.session.receive(input);
    expect(backend.requests).toHaveLength(1);
    expect(
      [...backend.publicLog.values()].filter(
        (event) => event.type === "agent.message",
      ),
    ).toHaveLength(1);
    expect(backend.finishes.get(input.id)).toEqual({ result: "success" });
  } finally {
    await second.session.close();
    await second.ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test.each(["replication", "publication"] as const)(
  "saved finish still waits for %s checkpoint",
  async (phase) => {
    const backend = new DsrBackend();
    const root = await mkdtemp(join(tmpdir(), "dsr-finish-barrier-"));
    const { ctx, session, client } = await fixture(backend, root);
    const { appendPluginRecord } = await import("@deepseek-ai/dsh-session");
    appendPluginRecord(session.runtime.agent.session, "plugin:oma/dsr-finish", {
      version: 2,
      inputId: "saved",
      completion: { result: "success" },
    });
    const { SessionReplication } =
      await import("../src/runtime/replication.ts");
    const barrier =
      phase === "replication"
        ? spyOn(SessionReplication.prototype, "flush").mockRejectedValue(
            new Error("checkpoint failed"),
          )
        : spyOn(client, "publish").mockRejectedValue(
            new Error("checkpoint failed"),
          );
    if (phase === "publication") {
      appendPluginRecord(
        session.runtime.agent.session,
        "plugin:oma/assistant-block",
        {
          output: {
            id: "saved_message",
            type: "agent.message",
            content: [{ type: "text", text: "saved" }],
          },
        } as never,
      );
      session.replay(session.runtime.agent.session);
    }
    try {
      await expect(
        session.receive({
          id: "saved",
          kind: "user.message",
          lane: "task",
          content: [],
        }),
      ).rejects.toThrow("checkpoint failed");
      expect(backend.finishes.size).toBe(0);
    } finally {
      barrier.mockRestore();
      await session.close().catch(() => {});
      await ctx.fiber.dispose();
      backend.stop();
      await rm(root, { recursive: true, force: true });
    }
  },
);
