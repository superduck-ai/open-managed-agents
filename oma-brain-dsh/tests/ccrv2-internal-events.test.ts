import { expect, spyOn, test } from "bun:test";
import { CcrBatchUploader } from "../src/ccrv2/batch-uploader.ts";
import { CcrClient, type CcrHttpRequest } from "../src/ccrv2/client.ts";
import type { WorkerEvent } from "../src/ccrv2/wire.ts";

type Body = { session_id: string; worker_epoch: number; events: WorkerEvent[] };
const settle = () => new Promise<void>((resolve) => setImmediate(resolve));

function fixture(request: (request: CcrHttpRequest) => Promise<Response>) {
  const credentials = { token: "token-7", workerEpoch: 7 };
  const client = new CcrClient({ request, credentials: () => credentials, sessionIds: () => ["s"],
    sleep: async () => {}, random: () => 0 });
  return { client, credentials };
}

test("successful internal-events cleanup failures confirm batches without retries or failure logs", async () => {
  const warn = spyOn(console, "warn").mockImplementation(() => {});
  try {
    for (const status of [200, 202]) {
      const requests: CcrHttpRequest[] = [];
      const waits: number[] = [];
      let cleanups = 0;
      const { client } = fixture(async (request) => {
        requests.push(request);
        return requests.length <= 2 ? new Response(new ReadableStream({
          cancel() { cleanups++; throw new Error("response cleanup failed"); },
        }), { status }) : new Response(null, { status: 204 });
      });
      client.config.sleep = async (ms) => { waits.push(ms); };
      const data = [{ log: "first" }, { revision: 2 }, { log: "child" }];
      const results = await Promise.allSettled([
        client.persist("s", "dsh_log", data[0]),
        client.persist("s", "test_record", data[1]),
        client.persist("s", "dsh_log", data[2], "child"),
      ]);
      for (const result of results) expect(result).toEqual({ status: "fulfilled", value: undefined });
      expect(requests.map((request) => (request.body as Body).events.length)).toEqual([1, 2]);
      expect(requests.flatMap((request) => (request.body as Body).events.map((event) => event.payload.data))).toEqual(data);
      expect(cleanups).toBe(2);
      expect(waits).toEqual([]);
      await client.persist("s", "test_record", { revision: 3 });
      expect(requests).toHaveLength(3);
    }
    expect(warn).not.toHaveBeenCalled();
  } finally { warn.mockRestore(); }
});

test("session cancellation during successful internal-events cleanup still rejects the batch and backlog", async () => {
  const cleaning = Promise.withResolvers<void>();
  const cleanup = Promise.withResolvers<void>();
  let requests = 0;
  const { client } = fixture(async () => {
    requests++;
    return requests === 1 ? new Response(new ReadableStream({
      cancel() { cleaning.resolve(); return cleanup.promise; },
    }), { status: 200 }) : new Response(null, { status: 204 });
  });
  const detach = client.bindLease("s").subscribe!(() => {});
  const completion = Promise.allSettled([
    client.persist("s", "dsh_log", { first: true }),
    client.persist("s", "test_record", { revision: 2 }),
  ]);
  try {
    await cleaning.promise;
    detach();
    cleanup.reject(new Error("response cleanup failed"));
    const results = await completion;
    for (const result of results) {
      expect(result.status).toBe("rejected");
      if (result.status === "rejected") expect(result.reason.message).toBe("CCRv2 session stopped");
    }
    expect(requests).toBe(1);
    await settle();
    const detachReplacement = client.bindLease("s").subscribe!(() => {});
    try {
      await client.persist("s", "dsh_log", { replacement: true });
      expect(requests).toBe(2);
    } finally { detachReplacement(); }
  } finally { detach(); cleanup.resolve(); await completion; }
});

test("internal logs, state and child logs share a FIFO queue and confirm only their HTTP batch", async () => {
  const first = Promise.withResolvers<void>();
  const second = Promise.withResolvers<void>();
  const sendingSecond = Promise.withResolvers<void>();
  const requests: CcrHttpRequest[] = [];
  const confirmed: number[] = [];
  let active = 0;
  let maxActive = 0;
  const { client } = fixture(async (request) => {
    requests.push(request);
    const attempt = requests.length;
    maxActive = Math.max(maxActive, ++active);
    try {
      if (attempt === 1) await first.promise;
      if (attempt === 2) { sendingSecond.resolve(); await second.promise; }
      return new Response(null, { status: 204 });
    } finally { active--; }
  });
  const data = Array.from({ length: 302 }, (_, index) => ({ index }));
  const pending = Promise.allSettled(data.map((value, index) => client.persist("s",
    index % 3 === 1 ? "test_record" : "dsh_log", value, index % 3 === 2 ? "child" : undefined,
  ).then(() => { confirmed.push(index); })));
  try {
    expect(requests).toHaveLength(1);
    expect(confirmed).toEqual([]);
    first.resolve();
    await sendingSecond.promise; await settle();
    expect(confirmed).toEqual([0]);
    expect(requests).toHaveLength(2);
    second.resolve();
    expect((await pending).every((result) => result.status === "fulfilled")).toBe(true);
    const bodies = requests.map((request) => request.body as Body);
    expect(maxActive).toBe(1);
    expect(bodies.map((body) => body.events.length)).toEqual([1, 100, 100, 100, 1]);
    const events = bodies.flatMap((body) => body.events);
    expect(events.map((event) => event.payload.data)).toEqual(data);
    expect(confirmed).toEqual(data.map((value) => value.index));
    events.forEach((event, index) => {
      expect(event.agent_id).toBe(index % 3 === 2 ? "child" : undefined);
      expect(event.is_compaction).toBe(false);
      expect(event.payload).toMatchObject({ type: "system", schema_version: 1,
        subtype: index % 3 === 1 ? "test_record" : "dsh_log" });
    });
    for (const request of requests) {
      expect(request).toMatchObject({ method: "POST", path: "/v1/code/sessions/s/worker/internal-events", timeout: 10_000 });
      expect(request.body).toMatchObject({ session_id: "s", worker_epoch: 7 });
    }
  } finally { first.resolve(); second.resolve(); await pending; }
});

test("internal batches count UTF-8 bytes and send an oversized first event alone", async () => {
  const release = Promise.withResolvers<void>();
  const batches: WorkerEvent[][] = [];
  const { client } = fixture(async (request) => {
    batches.push((request.body as Body).events);
    if (batches.length === 1) await release.promise;
    return new Response(null, { status: 204 });
  });
  const texts = ["seed", "汉".repeat(2 * 1024 * 1024), "汉".repeat(2 * 1024 * 1024),
    "x".repeat(11 * 1024 * 1024), "tail"];
  const pending = Promise.allSettled(texts.map((text, index) => client.persist("s", "dsh_log", { index, text })));
  release.resolve();
  expect((await pending).every((result) => result.status === "fulfilled")).toBe(true);
  expect(batches.map((batch) => batch.length)).toEqual([1, 1, 1, 1, 1]);
  const bytes = batches.map((batch) => batch.reduce((size, event) => size + Buffer.byteLength(JSON.stringify(event)), 0));
  expect(bytes[1]! + bytes[2]!).toBeGreaterThan(10 * 1024 * 1024);
  expect(bytes[3]!).toBeGreaterThan(10 * 1024 * 1024);
  expect(batches.flatMap((batch) => batch.map((event) => event.payload.data)))
    .toEqual(texts.map((text, index) => ({ index, text })));
});

test("internal uploader backpressure caps pending items at 200 and cancellation releases every caller", async () => {
  let signal: AbortSignal | undefined;
  let sends = 0;
  let closes = 0;
  const queue = new CcrBatchUploader<number>(async (_batch, requestSignal) => {
    sends++; signal = requestSignal; await new Promise<void>(() => {});
  }, () => { closes++; }, undefined, { maxBatchSize: 100, maxQueueSize: 200, maxBatchBytes: 10 * 1024 * 1024 });
  const pending = Promise.allSettled(Array.from({ length: 240 }, (_, index) => queue.enqueue(index)));
  expect(sends).toBe(1);
  expect(queue.pendingCount).toBe(200);
  const stopped = new Error("Claim cancelled");
  await queue.close(stopped);
  expect((await pending).every((result) => result.status === "rejected" && result.reason === stopped)).toBe(true);
  expect(queue.pendingCount).toBe(0);
  expect(signal?.aborted).toBe(true);
  expect(closes).toBe(1);
  await expect(queue.enqueue(241)).rejects.toThrow(stopped.message);
});

test("a retried internal batch retains UUIDs and queued snapshots while refreshing credentials", async () => {
  const first = Promise.withResolvers<void>();
  const waiting = Promise.withResolvers<void>();
  const resume = Promise.withResolvers<void>();
  const requests: CcrHttpRequest[] = [];
  const waits: number[] = [];
  const { client, credentials } = fixture(async (request) => {
    requests.push(request); credentials.token = `token-${requests.length + 7}`;
    if (requests.length === 1) await first.promise;
    return new Response(null, { status: requests.length === 2 || requests.length === 3 ? 503 : 204 });
  });
  const detach = client.bindLease("s").subscribe!(() => {});
  client.config.sleep = async (ms) => {
    waits.push(ms);
    if (waits.length === 2) { waiting.resolve(); await resume.promise; }
  };
  const data = { revision: 1 };
  let confirmed = false;
  const pending = Promise.allSettled([
    client.persist("s", "dsh_log", { seed: true }),
    client.persist("s", "test_record", data).then(() => { confirmed = true; }),
    client.persist("s", "dsh_log", { child: true }, "child"),
  ]);
  try {
    data.revision = 99;
    first.resolve(); await waiting.promise;
    expect(confirmed).toBe(false);
    expect(requests).toHaveLength(3);
    expect((requests[1]!.body as Body).events).toHaveLength(2);
    resume.resolve();
    expect((await pending).every((result) => result.status === "fulfilled")).toBe(true);
    expect(requests).toHaveLength(4);
    expect(requests[2]!.body).toEqual(requests[1]!.body);
    expect(requests[3]!.body).toEqual(requests[1]!.body);
    expect((requests[1]!.body as Body).events[0]!.payload.data).toEqual({ revision: 1 });
    expect(requests.map((request) => request.token)).toEqual(["token-7", "token-8", "token-9", "token-10"]);
    expect(waits).toEqual([500, 1000]);
  } finally { first.resolve(); resume.resolve(); detach(); await pending; }
});

test("replacement epochs and same-epoch registration fence all queued internal writes", async () => {
  for (const replacement of ["epoch", "registration"] as const) {
    const late = Promise.withResolvers<Response>();
    const requests: CcrHttpRequest[] = [];
    const { client, credentials } = fixture(async (request) => {
      if (request.path.endsWith("/register")) return Response.json({ worker_epoch: credentials.workerEpoch });
      requests.push(request);
      return requests.length === 1 ? late.promise : new Response(null, { status: 204 });
    });
    const old = Promise.allSettled(Array.from({ length: 240 }, (_, index) => client.persist("s", "dsh_log", { index })));
    try {
      if (replacement === "epoch") { credentials.workerEpoch++; credentials.token = "replacement-token"; }
      else await client.register("s", "token-7");
      await client.persist("s", "dsh_log", { replacement });
      expect((await old).every((result) => result.status === "rejected")).toBe(true);
      expect(requests[0]!.signal?.aborted).toBe(true);
      expect(requests).toHaveLength(2);
      expect((requests[1]!.body as Body).events.map((event) => event.payload.data)).toEqual([{ replacement }]);
      expect((requests[1]!.body as Body).worker_epoch).toBe(credentials.workerEpoch);
      late.resolve(new Response(null, { status: 409 })); await settle();
      await client.persist("s", "dsh_log", { current: true });
      expect(requests).toHaveLength(3);
    } finally { late.resolve(new Response(null, { status: 204 })); await old; }
  }
});

test("session stop cancels an internal retry backlog while a final bounded drain remains possible", async () => {
  const waiting = Promise.withResolvers<void>();
  let stopping = false;
  let attempts = 0;
  const { client } = fixture(async () => {
    attempts++; return new Response(null, { status: stopping ? 204 : 503 });
  });
  const detach = client.bindLease("s").subscribe!(() => {});
  client.config.sleep = async () => { waiting.resolve(); await new Promise<void>(() => {}); };
  const pending = Promise.allSettled(Array.from({ length: 240 }, (_, index) => client.persist("s", "dsh_log", { index })));
  await waiting.promise;
  stopping = true; detach();
  expect((await pending).every((result) => result.status === "rejected")).toBe(true);
  expect(attempts).toBe(1);
  await client.persist("s", "dsh_log", { final: true });
  expect(attempts).toBe(2);
});

test("a terminal internal batch failure rejects its backlog instead of sending later checkpoints", async () => {
  for (const status of [409, 410, 503]) {
    let attempts = 0;
    const { client } = fixture(async () => { attempts++; return new Response(null, { status }); });
    const results = await Promise.allSettled(Array.from({ length: 240 }, (_, index) =>
      client.persist("s", "test_record", { revision: index })));
    expect(results.every((result) => result.status === "rejected")).toBe(true);
    expect(attempts).toBe(status === 503 ? 3 : 1);
  }
});

test("invalid internal JSON rejects before enqueue without poisoning other writes or sessions", async () => {
  const release = Promise.withResolvers<void>();
  const requests: CcrHttpRequest[] = [];
  const { client } = fixture(async (request) => {
    requests.push(request);
    if (request.path.includes("/s/")) await release.promise;
    return new Response(null, { status: 204 });
  });
  let confirmed = false;
  const first = client.persist("s", "dsh_log", {}).then(() => { confirmed = true; });
  try {
    await expect(client.persist("s", "dsh_log", { invalid: 1n })).rejects.toThrow();
    await client.persist("other", "dsh_log", {});
    expect(confirmed).toBe(false);
    expect(requests).toHaveLength(2);
    release.resolve(); await first;
    await client.persist("s", "dsh_log", { next: true });
    expect(requests).toHaveLength(3);
  } finally { release.resolve(); await first; }
});
