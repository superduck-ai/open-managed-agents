import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
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
  externalMcpFetch?: typeof fetch,
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
        {...omaRuntimeConfig(api, assignment), ...(externalMcpFetch ? {externalMcpFetch} : {})},
        { approvalWaitMs },
      );
      await session.open();
    },
  });
  return { ctx, session, client, api };
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


test("DSR retries failed discovery against the current session on a later input",async()=>{
 const backend=new DsrBackend();let healthy=false;let discoveries=0;
 const peer=Bun.serve({port:0,hostname:"127.0.0.1",async fetch(request){
  if(request.method!=="POST") return new Response(null,{status:405});
  const body=await request.json() as any;
  if(body.method==="server/discover") {discoveries++;if(!healthy)return new Response(null,{status:503});return Response.json({jsonrpc:"2.0",id:body.id,error:{code:-32601,message:"legacy"}},{status:404});}
  if(body.method==="initialize")return Response.json({jsonrpc:"2.0",id:body.id,result:{protocolVersion:"2025-03-26",capabilities:{tools:{}},serverInfo:{name:"recovery",version:"1"}}});
  if(body.method==="notifications/initialized")return new Response(null,{status:202});
  if(body.method==="tools/list")return Response.json({jsonrpc:"2.0",id:body.id,result:{tools:[{name:"recovered_read",inputSchema:{type:"object"}}]}});
  return new Response(null,{status:405});
 }});
 backend.a.external_mcp_config={mcpServers:{peer:{type:"http",url:peer.url.href}}};
 backend.onModel=async()=>textEvents("done");
 const root=await mkdtemp(join(tmpdir(),"dsr-mcp-rediscovery-"));
 const {ctx,session}=await fixture(backend,root,undefined,fetch);

 try {
  await session.receive({id:"discovery_first",kind:"user.message",lane:"task",content:[{type:"text",text:"first"}]});
  const failed=discoveries;expect(failed).toBe(1);
  healthy=true;
  await session.receive({id:"discovery_second",kind:"user.message",lane:"task",content:[{type:"text",text:"second"}]});
  expect(discoveries).toBeGreaterThan(failed);
  expect(backend.requests.at(-1)!.tools.some((tool:any)=>tool.name==="mcp__peer__recovered_read")).toBe(true);
 } finally {await session.close();await ctx.fiber.dispose();backend.stop();peer.stop(true);await rm(root,{recursive:true,force:true});}
});

test("DSR model schema retains native send_message alongside delegation", async () => {
  const backend = new DsrBackend();
  backend.a.launch.subagents = { inlineAgents: false, agents: [{name:"worker", description:"worker", systemPrompt:"Return ready", tools:[], agentSnapshot:{type:"agent",id:"worker",version:1,name:"worker",tools:[]}}] };
  backend.a.launch.tools = [];
  backend.modelScript = [textEvents("ready")];
  const root = await mkdtemp(join(tmpdir(), "dsr-message-schema-"));
  const { ctx, session } = await fixture(backend, root);
  try {
    await session.receive({ id: "schema-input", kind: "user.message", lane: "task", content: [{ type: "text", text: "reply ready" }] });
    const names = backend.requests[0]!.tools.map((tool: any) => tool.name);
    expect(names).toContain("list_agents");
    expect(names).toContain("subagent");
    expect(names).toContain("send_message");
    expect(names).toContain("interrupt_agent");
  } finally {
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test.each([false, true])(
  "DSR interrupted prefix cold replay after publication failure=%s",
  async (failPublication) => {
    const backend = new DsrBackend();
    const root = await mkdtemp(join(tmpdir(), "dsr-interrupted-prefix-"));
    const first = await fixture(backend, root);
    const { createAssistantMessage } = await import("@deepseek-ai/dsh-llm");
    const { pluginRecordOf } = await import("@deepseek-ai/dsh-session");
    first.api.modelResponses.set(first.session.id, "declared-response");
    first.api.modelRequestIDs.set(first.session.id, "declared-request");
    const source = first.session.runtime.agent.session;
    source.append("turn/start", { turn: 1 });
    source.append("step/start", { turn: 1, step: 1 });
    first.session.inputId = "interrupted-input";
    for (const frame of [
      { type: "start", attemptId: "attempt", revision: 1, turn: 1, step: 1 },
      {
        type: "chunk",
        attemptId: "attempt",
        revision: 2,
        index: 0,
        time: 0,
        chunk: { type: "block-start", index: 2, blockType: "text" },
      },
      {
        type: "chunk",
        attemptId: "attempt",
        revision: 3,
        index: 1,
        time: 1,
        chunk: { type: "text-delta", index: 2, text: "actual prefix" },
      },
    ] as AssistantStreamFrame[])
      first.session.stream(frame);
    const message = createAssistantMessage({
      source: { provider: "test", model: "test-model" },
      content: [{ type: "text", text: "actual prefix" }],
    });
    source.append(
      "assistant/message",
      {
        turn: 1,
        step: 1,
        message,
        stream: [
          {
            type: "chunk",
            time: 0,
            chunk: { type: "block-start", index: 2, blockType: "text" },
          },
          {
            type: "chunk",
            time: 1,
            chunk: { type: "text-delta", index: 2, text: "actual prefix" },
          },
        ],
        interrupted: true,
      },
      { surfaceOp: "append" },
    );
    const event = source.snapshotEvents().at(-1)!;
    first.session.record(event, source);
    first.session.stream({
      type: "end",
      attemptId: "attempt",
      revision: 4,
      index: 2,
      outcome: {
        kind: "committed",
        eventType: "assistant/message",
        seq: event.seq,
      },
    } as AssistantStreamFrame);
    expect(
      source
        .snapshotEvents()
        .map(pluginRecordOf)
        .filter((record) => record?.type === "plugin:oma/assistant-block"),
    ).toHaveLength(0);
    const output = (
      first.session as unknown as {
        pending: { id: string; type: string; content: unknown }[];
      }
    ).pending.find((output) => output.type === "agent.message")!;
    expect(output.content).toEqual([{ type: "text", text: "actual prefix" }]);
    const gate = Promise.withResolvers<void>();
    let publishing = false;
    const publish = spyOn(first.client, "publish").mockImplementation(
      async (events) => {
        if (
          events.some(
            (event) =>
              event.type === "preview.start" || event.type === "preview.delta",
          )
        )
          expect(
            backend.transcripts
              .get(first.session.id)!
              .events.some(
                (event) => event.type === "plugin:oma/assistant-identity",
              ),
          ).toBe(true);
        if (events.some((event) => event.id === output.id)) {
          publishing = true;
          expect(
            backend.transcripts
              .get(first.session.id)!
              .events.some(
                (event) => event.type === "plugin:oma/assistant-identity",
              ),
          ).toBe(true);
          await gate.promise;
          if (failPublication)
            throw new Error("interrupted publication unavailable");
        }
      },
    );
    try {
      const ending = (
        first.session as unknown as {
          completeCancelled(id: string): Promise<void>;
        }
      ).completeCancelled("interrupted-input");
      await eventually(() => publishing);
      expect(backend.finishes.has("interrupted-input")).toBe(false);
      gate.resolve();
      if (failPublication) {
        await expect(ending).rejects.toThrow(
          "interrupted publication unavailable",
        );
        expect(backend.finishes.has("interrupted-input")).toBe(false);
      } else {
        await ending;
        expect(backend.finishes.get("interrupted-input")!.result).toBe(
          "cancelled",
        );
      }
    } finally {
      gate.resolve();
      publish.mockRestore();
      await first.session.close().catch(() => {});
      await first.ctx.fiber.dispose();
    }
    const second = await fixture(backend, root);
    try {
      await second.session.receive({
        id: "interrupted-input",
        kind: "user.message",
        lane: "task",
        content: [{ type: "text", text: "must not execute again" }],
      });
      expect(backend.requests).toHaveLength(0);
      expect(backend.finishes.get("interrupted-input")!.result).toBe(
        "cancelled",
      );
      expect(backend.publicLog.get(output.id)!.content).toEqual([
        { type: "text", text: "actual prefix" },
      ]);
    } finally {
      await second.session.close();
      await second.ctx.fiber.dispose();
      backend.stop();
      await rm(root, { recursive: true, force: true });
    }
  },
);

test("DSR aborted child settlement does not wake parent while normal and non-DSR settlements do", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-aborted-settlement-"));
  const { ctx, session } = await fixture(backend, root);
  const service = session.runtime.agent.ctx.get("subagents") as unknown as {
    manager: {
      notifySettlement(activation: unknown, terminal: unknown): void;
      sendWaking(...args: unknown[]): unknown;
    };
  };
  const manager = service.manager;
  const waking = spyOn(manager, "sendWaking").mockImplementation(
    () => undefined,
  );
  const activation = {
    announced: true,
    delivery: "parent",
    parent: session.runtime.agent,
    childId: "child",
    kind: "local",
    omitClosingText: false,
  };
  try {
    manager.notifySettlement(activation, { stopReason: "aborted", output: [] });
    manager.notifySettlement(activation, { stopReason: "aborted", output: [] });
    expect(waking).not.toHaveBeenCalled();
    manager.notifySettlement(activation, {
      stopReason: "completed",
      output: [{ type: "text", text: "actual report" }],
    });
    expect(waking).toHaveBeenCalledTimes(1);
    const calls: unknown[][] = [];
    manager.notifySettlement.call(
      {
        ctx: {
          get: () => false,
          agents: { get: () => activation.parent },
          logger: { warn: () => {} },
        },
        closingTeardownFor: () => undefined,
        sendWaking: (...args: unknown[]) => calls.push(args),
      },
      activation,
      { stopReason: "aborted", output: [] },
    );
    expect(calls).toHaveLength(1);
  } finally {
    waking.mockRestore();
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("legacy interrupted record without durable identity fails instead of inventing its public ID", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-interrupted-legacy-"));
  const { ctx, session } = await fixture(backend, root);
  const { createAssistantMessage } = await import("@deepseek-ai/dsh-llm");
  const source = session.runtime.agent.session;
  source.append("turn/start", { turn: 1 });
  source.append("step/start", { turn: 1, step: 1 });
  source.append(
    "assistant/message",
    {
      turn: 1,
      step: 1,
      message: createAssistantMessage({
        source: { provider: "test", model: "test" },
        content: [{ type: "text", text: "legacy prefix" }],
      }),
      stream: [],
      interrupted: true,
    },
    { surfaceOp: "append" },
  );
  try {
    await expect(session.checkpoint()).rejects.toThrow("no durable public identity");
    expect(backend.publicLog.size).toBe(0);
    expect(backend.finishes.size).toBe(0);
  } finally {
    await session.close().catch(() => {});
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("DSR publishes actual SDK successful compaction and prune after durable native records", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-native-compaction-"));
  const { ctx, session } = await fixture(backend, root);
  backend.onModel = async () =>
    textEvents("owned public conversation ".repeat(3000));
  try {
    await session.receive({
      id: "compaction-input",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "owned input ".repeat(3000) }],
    });
    backend.onModel = async () => textEvents("short summary");
    const result = await ctx.compaction.compactNow(
      session.runtime.agent,
      session.abort.signal,
    );
    expect(result).not.toBeNull();
    const native = session.runtime.agent.session.snapshotEvents();
    expect(native.some((event) => event.type === "compaction/start")).toBe(
      true,
    );
    expect(
      native.some(
        (event) =>
          event.type === "compaction/end" && event.data.error === undefined,
      ),
    ).toBe(true);
    await session.checkpoint();
    expect(
      [...backend.publicLog.values()].filter(
        (event) => event.type === "agent.thread_context_compacted",
      ),
    ).toHaveLength(1);
    expect(
      backend.transcripts
        .get(session.id)!
        .events.some((event) => event.type === "compaction/end"),
    ).toBe(true);
    const { projectCompaction } = await import("../src/dsr/compaction.ts");
    const source = session.runtime.agent.session;
    const end = native.findLast((event) => event.type === "compaction/end")!;
    for (const fault of [
      "missing-start",
      "wrong-start",
      "missing-summary",
      "failed-end",
    ]) {
      const events = structuredClone(native);
      const negative = {
        id: source.id,
        snapshotEvents: () =>
          events
            .filter(
              (event) =>
                fault !== "missing-start" || event.type !== "compaction/start",
            )
            .filter(
              (event) =>
                fault !== "missing-summary" ||
                event.type !== "compaction/summary",
            ),
      } as unknown as typeof source;
      for (const event of events)
        if (fault === "wrong-start" && event.type === "compaction/start")
          event.data.compactionId = "foreign" as typeof event.data.compactionId;
      const altered = structuredClone(end);
      if (fault === "failed-end" && altered.type === "compaction/end")
        altered.data.error = "controlled failure";
      expect(projectCompaction(altered, negative, session.id)).toBeUndefined();
    }

    backend.onTool = async () => ({
      content: [{ type: "text", text: "owned tool output ".repeat(1000) }],
    });
    backend.modelScript.splice(
      0,
      backend.modelScript.length,
      toolEvents("echo owned", "large-result"),
      textEvents("tool done"),
    );
    backend.onModel = undefined;
    await session.receive({
      id: "prune-input",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "run owned tool" }],
    });
    ctx.toolResultPruner.pruneSession(session.runtime.agent.session);
    expect(
      session.runtime.agent.session
        .snapshotEvents()
        .some((event) => event.type === "compaction/prune"),
    ).toBe(true);
    await session.checkpoint();
    expect(
      [...backend.publicLog.values()].filter(
        (event) => event.type === "agent.thread_context_compacted",
      ).length,
    ).toBeGreaterThan(1);
  } finally {
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("DSR failed SDK compaction and unmatched starts never publish a success notification", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-compaction-failure-"));
  const { ctx, session } = await fixture(backend, root);
  backend.onModel = async () => textEvents("owned source ".repeat(3000));
  try {
    await session.receive({
      id: "failure-source",
      kind: "user.message",
      lane: "task",
      content: [{ type: "text", text: "owned prompt ".repeat(3000) }],
    });
    backend.onModel = async () =>
      Response.json(
        {
          error: {
            type: "invalid_request_error",
            message: "controlled local compaction rejection",
          },
        },
        { status: 400 },
      );
    await expect(
      ctx.compaction.compactNow(session.runtime.agent, session.abort.signal),
    ).rejects.toThrow();
    expect(
      session.runtime.agent.session
        .snapshotEvents()
        .some(
          (event) =>
            event.type === "compaction/end" && event.data.error !== undefined,
        ),
    ).toBe(true);
    await session.checkpoint();
    expect(
      [...backend.publicLog.values()].filter(
        (event) => event.type === "agent.thread_context_compacted",
      ),
    ).toHaveLength(0);
    const { CompactionId } = await import("@deepseek-ai/dsh-compaction");
    session.runtime.agent.session.append("compaction/start", {
      compactionId: CompactionId("unmatched"),
      turn: null,
    });
    await session.checkpoint();
    expect(
      [...backend.publicLog.values()].filter(
        (event) => event.type === "agent.thread_context_compacted",
      ),
    ).toHaveLength(0);
  } finally {
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test.each([false, true])(
  "DSR compaction waits for replication and cold replays stable receipts after publication failure=%s",
  async (failPublish) => {
    const backend = new DsrBackend();
    const root = await mkdtemp(join(tmpdir(), "dsr-compaction-replay-"));
    const first = await fixture(backend, root);
    backend.onModel = async () =>
      textEvents("durable owned conversation ".repeat(3000));
    try {
      await first.session.receive({
        id: "replay-source",
        kind: "user.message",
        lane: "task",
        content: [{ type: "text", text: "owned history ".repeat(3000) }],
      });
      backend.onModel = async () => textEvents("short durable summary");
      await first.ctx.compaction.compactNow(
        first.session.runtime.agent,
        first.session.abort.signal,
      );
      const { SessionReplication } =
        await import("../src/runtime/replication.ts");
      let release!: () => void;
      const gate = new Promise<void>((resolve) => {
        release = resolve;
      });
      let entered = false;
      const original = SessionReplication.prototype.flush;
      const flush = spyOn(
        SessionReplication.prototype,
        "flush",
      ).mockImplementation(async function (
        this: InstanceType<typeof SessionReplication>,
        ...args: Parameters<typeof original>
      ) {
        entered = true;
        await gate;
        return original.apply(this, args);
      });
      backend.failDsrPublication = failPublish;
      const checkpoint = first.session.checkpoint();
      void checkpoint.catch(() => {});
      try {
        await eventually(() => entered);
        expect(
          [...backend.publicLog.values()].filter(
            (event) => event.type === "agent.thread_context_compacted",
          ),
        ).toHaveLength(0);
        release();
        if (failPublish) await expect(checkpoint).rejects.toThrow();
        else await checkpoint;
      } finally {
        release();
        flush.mockRestore();
      }
      const native = backend.transcripts.get(first.session.id)!.events;
      expect(
        native.some(
          (event) =>
            event.type === "compaction/end" && event.data.error === undefined,
        ),
      ).toBe(true);
      const { projectCompaction } = await import("../src/dsr/compaction.ts");
      const source = first.session.runtime.agent.session;
      const end = source
        .snapshotEvents()
        .findLast((event) => event.type === "compaction/end")!;
      const expected = projectCompaction(end, source, first.session.id)!;
      await first.session.close().catch(() => {});
      await first.ctx.fiber.dispose();
      backend.failDsrPublication = false;
      const second = await fixture(backend, root + "-cold");
      try {
        expect(backend.publicLog.get(expected.id)).toEqual(expected);
        second.session.replay(second.session.runtime.agent.session);
        await second.session.checkpoint();
        expect(
          [...backend.publicLog.values()].filter(
            (event) => event.type === "agent.thread_context_compacted",
          ),
        ).toEqual([expected]);
        expect(backend.requests).toHaveLength(2);
      } finally {
        await second.session.close();
        await second.ctx.fiber.dispose();
      }
    } finally {
      await first.session.close().catch(() => {});
      await first.ctx.fiber.dispose();
      backend.stop();
      await rm(root, { recursive: true, force: true });
      await rm(root + "-cold", { recursive: true, force: true });
    }
  },
);

test("DSR actual SDK child compaction publishes only its owning thread", async () => {
  const backend = new DsrBackend();
  const root = await mkdtemp(join(tmpdir(), "dsr-child-compaction-"));
  const { ctx, session } = await fixture(backend, root);
  const thread = {
    id: "owned-compaction-child",
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
  backend.onModel = async () => textEvents("child conversation ".repeat(3000));
  try {
    await child.run("child input ".repeat(3000), {
      beforeRun: async () => {},
      afterRun: async () => {},
    });
    backend.onModel = async () => textEvents("short child summary");
    await ctx.compaction.compactNow(child.agent, session.abort.signal);
    await session.checkpoint(child.agent.session);
    const notifications = [...backend.publicLog.values()].filter(
      (event) => event.type === "agent.thread_context_compacted",
    );
    expect(notifications).toHaveLength(1);
    expect(notifications[0]!.session_thread_id).toBe(thread.id);
    expect(
      backend.transcripts
        .get(thread.id)!
        .events.some((event) => event.type === "compaction/end"),
    ).toBe(true);
    expect(
      backend.transcripts
        .get(session.id)!
        .events.some((event) => event.type === "compaction/end"),
    ).toBe(false);
  } finally {
    await child.close();
    unbind?.();
    await session.close();
    await ctx.fiber.dispose();
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});
