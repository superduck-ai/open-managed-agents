import { Context } from "@deepseek-ai/cordis";
import {
  Session,
  SessionId,
  appendPluginRecord,
  type SessionHeader,
  type SessionEvent,
} from "@deepseek-ai/dsh-session";
import {
  createAssistantMessage,
  createUserMessage,
  createToolResultMessage,
  ToolCallId,
} from "@deepseek-ai/dsh-llm";
import { expect, test, spyOn } from "bun:test";
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
import { DsrBackend } from "./dsr-backend.ts";

class ReplayBackend extends DsrBackend {
  directoryRequests = 0;
  losePublicationResponses = false;
  rejectReceiptWrites = false;
  rejectPublicAfter?: number;
  beforePublish?: () => Promise<void>;
  batches: {
    count: number;
    bytes: number;
    accepted: number;
    duplicates: number;
  }[] = [];
  override async route(request: Request): Promise<Response> {
    const url = new URL(request.url);
    const path = url.pathname;
    if (request.method === "GET" && path.endsWith("/transcripts")) {
      this.directoryRequests++;
      const response = await super.route(request);
      const directory = await response.json();
      const after = url.searchParams.get("after");
      const remaining = directory.data.filter(
        (row: { dsh_session_id: string }) =>
          after === null || row.dsh_session_id > after,
      );
      const data = remaining.slice(
        0,
        Number(url.searchParams.get("limit") ?? "100"),
      );
      return Response.json({
        data,
        next_after:
          remaining.length > data.length ? data.at(-1).dsh_session_id : null,
      });
    }
    if (
      this.rejectReceiptWrites &&
      request.method === "POST" &&
      path.endsWith("/records")
    ) {
      const records = (await request.clone().text())
        .trimEnd()
        .split("\n")
        .map((line) => JSON.parse(line));
      if (
        records.some(
          (event: { type: string }) =>
            event.type === "plugin:oma/dsr-publication",
        )
      )
        return Response.json({}, { status: 503 });
    }
    if (!path.endsWith("/public-events")) return super.route(request);
    if (
      this.rejectPublicAfter !== undefined &&
      this.batches.length >= this.rejectPublicAfter
    )
      return Response.json({}, { status: 503 });
    await this.beforePublish?.();
    const raw = await request.clone().text();
    const response = await super.route(request);
    const result = await response.clone().json();
    this.batches.push({
      count: JSON.parse(raw).events.length,
      bytes: Buffer.byteLength(raw),
      accepted: result.accepted,
      duplicates: result.duplicates,
    });
    return this.losePublicationResponses
      ? Response.json({}, { status: 503 })
      : response;
  }
}

function history(
  backend: ReplayBackend,
  turns: number,
  tools = false,
  textBytes = 64,
) {
  const session = Session.create(SessionId(backend.a.session_id));
  appendHistory(session, 1, turns, tools, textBytes);
  return session;
}

function appendHistory(
  session: Session,
  start: number,
  turns: number,
  tools = false,
  textBytes = 64,
) {
  for (let turn = start; turn < start + turns; turn++) {
    session.append("turn/start", { turn });
    session.append(
      "user/message",
      createUserMessage({
        content: [{ type: "text", text: `input ${turn}` }],
        source: { kind: "user" },
      }),
      { surfaceOp: "append" },
    );
    session.append("step/start", { turn, step: 1 });
    const callId = ToolCallId(`call_${turn}`);
    session.append(
      "assistant/message",
      {
        turn,
        step: 1,
        message: createAssistantMessage({
          source: { provider: "test", model: "test-model" },
          content: [
            { type: "text", text: "x".repeat(textBytes) },
            ...(tools
              ? [
                  {
                    type: "tool-call" as const,
                    id: callId,
                    name: "Bash",
                    arguments: "{}",
                  },
                ]
              : []),
          ],
        }),
        stream: [],
      },
      { surfaceOp: "append" },
    );
    if (tools) {
      session.append("tool/call", {
        turn,
        step: 1,
        callId,
        name: "Bash",
        arguments: "{}",
      });
      session.append(
        "tool/result",
        {
          turn,
          step: 1,
          message: createToolResultMessage({
            callId,
            content: [{ type: "text", text: "r".repeat(textBytes) }],
            isError: false,
          }),
        },
        { surfaceOp: "append" },
      );
    }
    session.append("step/end", { turn, step: 1 });
    session.append("turn/end", { turn, reason: { kind: "completed" } });
  }
}

async function openFixture(backend: ReplayBackend) {
  await backend.ready;
  const directory = await mkdtemp(join(tmpdir(), "dsr-replay-probe-"));
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
      );
    },
  });
  return {
    ctx,
    session,
    async close() {
      try {
        await session.close();
      } finally {
        try {
          await ctx.fiber.dispose();
        } finally {
          await rm(directory, { recursive: true, force: true });
        }
      }
    },
  };
}

interface ReplayMeasurement {
  turns: number;
  nativeEvents: number;
  projectedEvents: number;
  publishRequests: number;
  uploadedBytes: number;
  accepted: number;
  duplicates: number;
  recordCalls: number;
  snapshotCallsInRecord: number;
  historyEntriesVisited: number;
}

function totals(backend: ReplayBackend) {
  return backend.batches.reduce(
    (sum, batch) => ({
      projectedEvents: sum.projectedEvents + batch.count,
      publishRequests: sum.publishRequests + 1,
      uploadedBytes: sum.uploadedBytes + batch.bytes,
      accepted: sum.accepted + batch.accepted,
      duplicates: sum.duplicates + batch.duplicates,
    }),
    {
      projectedEvents: 0,
      publishRequests: 0,
      uploadedBytes: 0,
      accepted: 0,
      duplicates: 0,
    },
  );
}

function instrumentReplay(owner: DsrSession) {
  let recordCalls = 0,
    snapshotCallsInRecord = 0,
    historyEntriesVisited = 0;
  const originalRecord = owner.record.bind(owner);
  const histories = new WeakMap<
    readonly SessionEvent[],
    readonly SessionEvent[]
  >();
  const sessions = new WeakMap<Session, Session>();
  const recordSpy = spyOn(owner, "record").mockImplementation(
    (event, session) => {
      recordCalls++;
      let observed = sessions.get(session);
      if (!observed) {
        observed = new Proxy(session, {
          get(target, key) {
            if (key !== "snapshotEvents")
              return Reflect.get(target, key, target);
            return () => {
              snapshotCallsInRecord++;
              const events = target.snapshotEvents();
              let history = histories.get(events);
              if (!history) {
                history = new Proxy(events, {
                  get(items, property, receiver) {
                    if (
                      typeof property === "string" &&
                      /^(0|[1-9]\d*)$/.test(property)
                    )
                      historyEntriesVisited++;
                    return Reflect.get(items, property, receiver);
                  },
                });
                histories.set(events, history);
              }
              return history;
            };
          },
        });
        sessions.set(session, observed);
      }
      originalRecord(event, observed);
    },
  );
  return {
    counts: () => ({
      recordCalls,
      snapshotCallsInRecord,
      historyEntriesVisited,
    }),
    restore() {
      recordSpy.mockRestore();
    },
  };
}

async function measureReplay(
  turns: number,
  tools: boolean,
): Promise<ReplayMeasurement> {
  const backend = new ReplayBackend();
  const seed = history(backend, turns, tools);
  backend.transcripts.set(seed.id, {
    header: { ...seed.header },
    events: seed.snapshotEvents().map((event) => ({ ...event })),
  });
  const first = await openFixture(backend);
  try {
    await first.session.open();
  } finally {
    await first.close();
  }
  backend.batches = [];
  const second = await openFixture(backend);
  const instrumentation = instrumentReplay(second.session);
  try {
    await second.session.open();
    return {
      turns,
      nativeEvents: seed.snapshotEvents().length,
      ...totals(backend),
      ...instrumentation.counts(),
    };
  } finally {
    instrumentation.restore();
    try {
      await second.close();
    } finally {
      backend.stop();
    }
  }
}

test("a cold open replays assistant and tool outputs with server deduplication", async () => {
  const result = await measureReplay(125, true);
  expect(result.nativeEvents).toBe(1000);
  expect(result.projectedEvents).toBe(250);
  expect(result.publishRequests).toBe(2);
  expect(result.accepted).toBe(0);
  expect(result.duplicates).toBe(250);
  expect(result.recordCalls).toBeGreaterThanOrEqual(result.nativeEvents);
  console.log("DSR_OPEN_REPLAY", JSON.stringify(result));
});

test("text history is indexed once without repeated record scans", async () => {
  const samples: ReplayMeasurement[] = [];
  for (const turns of [100, 200, 400])
    samples.push(await measureReplay(turns, false));
  for (let index = 1; index < samples.length; index++) {
    const previous = samples[index - 1]!,
      current = samples[index]!;
    expect(current.projectedEvents).toBe(previous.projectedEvents * 2);
    expect(current.duplicates).toBe(current.projectedEvents);
    expect(current.accepted).toBe(0);
    expect(current.historyEntriesVisited).toBeLessThanOrEqual(
      current.nativeEvents * 3,
    );
    expect(current.projectedEvents).toBeGreaterThan(0);
  }
  console.log("DSR_OPEN_TEXT_SCALING", JSON.stringify(samples));
});

test("tool result identity lookup uses an index during cold open", async () => {
  const samples: ReplayMeasurement[] = [];
  for (const turns of [100, 200, 400])
    samples.push(await measureReplay(turns, true));
  for (let index = 1; index < samples.length; index++) {
    const previous = samples[index - 1]!,
      current = samples[index]!;
    expect(current.projectedEvents).toBe(previous.projectedEvents * 2);
    expect(current.snapshotCallsInRecord).toBeLessThanOrEqual(2);
    expect(current.historyEntriesVisited).toBeLessThanOrEqual(
      current.nativeEvents * 3,
    );
    expect(current.projectedEvents).toBeGreaterThan(0);
  }
  console.log("DSR_OPEN_TOOL_SCALING", JSON.stringify(samples));
});

test("5000 native events replay in bounded batches without duplicate history", async () => {
  const backend = new ReplayBackend();
  const seed = history(backend, 625, true, 16 * 1024);
  backend.transcripts.set(seed.id, {
    header: { ...seed.header },
    events: seed.snapshotEvents().map((event) => ({ ...event })),
  });
  const first = await openFixture(backend);
  try {
    await first.session.open();
  } finally {
    await first.close();
  }
  expect(backend.publicLog.size).toBe(1250);
  backend.batches = [];
  const second = await openFixture(backend);
  try {
    await second.session.open();
    const result = totals(backend);
    expect(seed.snapshotEvents()).toHaveLength(5000);
    expect(result.projectedEvents).toBe(1250);
    expect(result.publishRequests).toBe(7);
    expect(result.accepted).toBe(0);
    expect(result.duplicates).toBe(1250);
    expect(result.uploadedBytes).toBeGreaterThan(0);
    expect(backend.publicLog.size).toBe(1250);
    expect(backend.requests).toHaveLength(0);
    console.log(
      "DSR_OPEN_5000_EVENTS",
      JSON.stringify({
        nativeEvents: 5000,
        contentBytesPerOutput: 16 * 1024,
        ...result,
      }),
    );
  } finally {
    try {
      await second.close();
    } finally {
      backend.stop();
    }
  }
}, 30000);

test("cold takeover replays the prefix and publishes the missing tail", async () => {
  const backend = new ReplayBackend();
  const seed = history(backend, 2, true);
  backend.transcripts.set(seed.id, {
    header: { ...seed.header },
    events: seed.snapshotEvents().map((event) => ({ ...event })),
  });
  const first = await openFixture(backend);
  try {
    await first.session.open();
  } finally {
    await first.close();
  }
  const stored = backend.transcripts.get(seed.id)!;
  const expanded = Session.create(
    seed.id,
    stored.events as SessionEvent[],
    stored.header as SessionHeader,
  );
  appendHistory(expanded, 3, 2, true);
  stored.events = expanded.snapshotEvents().map((event) => ({ ...event }));
  backend.batches = [];
  const second = await openFixture(backend);
  try {
    await second.session.open();
    expect(totals(backend)).toMatchObject({
      projectedEvents: 8,
      accepted: 4,
      duplicates: 4,
    });
    expect(backend.publicLog.size).toBe(8);
  } finally {
    try {
      await second.close();
    } finally {
      backend.stop();
    }
  }
});

test("lost public responses leave no receipt and remain safely replayable", async () => {
  const backend = new ReplayBackend();
  const seed = history(backend, 1, true);
  backend.transcripts.set(seed.id, {
    header: { ...seed.header },
    events: seed.snapshotEvents().map((event) => ({ ...event })),
  });
  backend.losePublicationResponses = true;
  const first = await openFixture(backend);
  try {
    await expect(first.session.open()).rejects.toThrow();
  } finally {
    await first.close();
  }
  expect(backend.publicLog.size).toBe(2);
  expect(
    backend.transcripts
      .get(seed.id)!
      .events.some((event) => event.type === "plugin:oma/dsr-publication"),
  ).toBe(false);
  backend.losePublicationResponses = false;
  backend.batches = [];
  const second = await openFixture(backend);
  try {
    await second.session.open();
    expect(totals(backend)).toMatchObject({
      projectedEvents: 2,
      accepted: 0,
      duplicates: 2,
    });
    expect(backend.publicLog.size).toBe(2);
  } finally {
    try {
      await second.close();
    } finally {
      backend.stop();
    }
  }
});

test("a failed later publish batch cannot acknowledge an unpublished tail", async () => {
  const backend = new ReplayBackend();
  const seed = history(backend, 125, true);
  backend.transcripts.set(seed.id, {
    header: { ...seed.header },
    events: seed.snapshotEvents().map((event) => ({ ...event })),
  });
  backend.rejectPublicAfter = 1;
  const first = await openFixture(backend);
  try {
    await expect(first.session.open()).rejects.toThrow();
  } finally {
    await first.close();
  }
  expect(backend.publicLog.size).toBe(200);
  expect(
    backend.transcripts
      .get(seed.id)!
      .events.some((event) => event.type === "plugin:oma/dsr-publication"),
  ).toBe(false);
  backend.rejectPublicAfter = undefined;
  backend.batches = [];
  const second = await openFixture(backend);
  try {
    await second.session.open();
    expect(totals(backend)).toMatchObject({
      projectedEvents: 250,
      accepted: 50,
      duplicates: 200,
    });
    expect(backend.publicLog.size).toBe(250);
  } finally {
    try {
      await second.close();
    } finally {
      backend.stop();
    }
  }
});

test("publication succeeds without writing transcript receipts", async () => {
  const backend = new ReplayBackend();
  const seed = history(backend, 1, true);
  backend.transcripts.set(seed.id, {
    header: { ...seed.header },
    events: seed.snapshotEvents().map((event) => ({ ...event })),
  });
  backend.rejectReceiptWrites = true;
  const first = await openFixture(backend);
  try {
    await first.session.open();
    expect(backend.publicLog.size).toBe(2);
  } finally {
    await first.close().catch(() => {});
  }
  expect(
    backend.transcripts
      .get(seed.id)!
      .events.some((event) => event.type === "plugin:oma/dsr-publication"),
  ).toBe(false);
  backend.rejectReceiptWrites = false;
  backend.batches = [];
  const second = await openFixture(backend);
  try {
    await second.session.open();
    expect(totals(backend)).toMatchObject({
      projectedEvents: 2,
      accepted: 0,
      duplicates: 2,
    });
    expect(
      backend.transcripts
        .get(seed.id)!
        .events.some((event) => event.type === "plugin:oma/dsr-publication"),
    ).toBe(false);
  } finally {
    try {
      await second.close();
    } finally {
      backend.stop();
    }
  }
});

test("events arriving during a publish are included on the next cold open", async () => {
  const backend = new ReplayBackend();
  const seed = history(backend, 1, true);
  backend.transcripts.set(seed.id, {
    header: { ...seed.header },
    events: seed.snapshotEvents().map((event) => ({ ...event })),
  });
  const reached = Promise.withResolvers<void>(),
    release = Promise.withResolvers<void>();
  backend.beforePublish = async () => {
    reached.resolve();
    await release.promise;
  };
  const first = await openFixture(backend);
  try {
    const opening = first.session.open();
    await reached.promise;
    appendHistory(first.session.runtime.agent.session, 2, 1, true);
    release.resolve();
    await opening;
    expect(backend.publicLog.size).toBe(2);
  } finally {
    release.resolve();
    await first.close();
  }
  backend.beforePublish = undefined;
  backend.batches = [];
  const second = await openFixture(backend);
  try {
    await second.session.open();
    expect(totals(backend)).toMatchObject({
      projectedEvents: 4,
      accepted: 2,
      duplicates: 2,
    });
    expect(backend.publicLog.size).toBe(4);
  } finally {
    try {
      await second.close();
    } finally {
      backend.stop();
    }
  }
});

test("session open shares one paginated directory snapshot and refreshes it for the next owner", async () => {
  const backend = new ReplayBackend();
  const root = history(backend, 0);
  backend.transcripts.set(root.id, { header: root.header, events: [] });
  for (let index = 0; index < 250; index++) {
    const id = SessionId(`directory_child_${String(index).padStart(4, "0")}`);
    const child = Session.create(id, [], {
      id,
      version: 4,
      createdAt: 0,
      isSeeded: false,
      parentSession: root.id,
      origin: "subagent",
      delegationDepth: 1,
    });
    backend.transcripts.set(id, { header: child.header, events: [] });
  }
  let current = await openFixture(backend);
  try {
    await current.session.open();
    expect(backend.directoryRequests).toBe(2);
    const id = SessionId("directory_child_new");
    const child = Session.create(id, [], {
      id,
      version: 4,
      createdAt: 0,
      isSeeded: false,
      parentSession: root.id,
      origin: "subagent",
      delegationDepth: 1,
    });
    await current.close();
    backend.transcripts.set(id, { header: child.header, events: [] });
    current = await openFixture(backend);
    await current.session.open();
    expect(backend.directoryRequests).toBe(4);
    expect(await current.ctx.sessionPersistence.stat(id)).toBeDefined();
  } finally {
    try {
      await current.close();
    } finally {
      backend.stop();
    }
  }
});

test.each(["missing", "changed"] as const)(
  "legacy transcript receipts cannot hide %s public state after a simulated rollback",
  async (state) => {
    const backend = new ReplayBackend();
    const seed = history(backend, 1, true);
    appendPluginRecord(seed, "plugin:oma/dsr-publication", {
      version: 1,
      through: seed.snapshotEvents().length,
    });
    backend.transcripts.set(seed.id, {
      header: { ...seed.header },
      events: seed.snapshotEvents().map((event) => ({ ...event })),
    });
    try {
      const first = await openFixture(backend);
      try {
        await first.session.open();
      } finally {
        await first.close();
      }
      expect(backend.publicLog.size).toBe(2);
      expect(
        backend.transcripts
          .get(seed.id)!
          .events.some((event) => event.type === "plugin:oma/dsr-publication"),
      ).toBe(true);
      const message = [...backend.publicLog.values()].find(
        (event) => event.type === "agent.message",
      )!;
      expect(message).toBeDefined();
      if (state === "missing") backend.publicLog.clear();
      else
        backend.publicLog.set(message.id, {
          ...message,
          content: [{ type: "text", text: "different persisted history" }],
        });
      backend.batches = [];
      const second = await openFixture(backend);
      try {
        if (state === "missing") {
          await second.session.open();
          expect(totals(backend).accepted).toBe(2);
          expect(backend.publicLog.size).toBe(2);
        } else {
          await expect(second.session.open()).rejects.toThrow();
          expect(backend.publicLog.get(message.id)!.content).toEqual([
            { type: "text", text: "different persisted history" },
          ]);
        }
      } finally {
        await second.close().catch(() => {});
      }
    } finally {
      backend.stop();
    }
  },
);
