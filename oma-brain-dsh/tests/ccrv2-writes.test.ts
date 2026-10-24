import { afterEach, expect, test } from "bun:test";
import { CcrClient, type CcrClientConfig, type CcrHttpRequest } from "../src/ccrv2/client.ts";
import { CcrHeartbeat, type CcrLeaseFailure } from "../src/ccrv2/heartbeat.ts";
import { sleep } from "../src/ccrv2/retry.ts";

const heartbeats: CcrHeartbeat[] = [];
afterEach(async () => { await Promise.all(heartbeats.splice(0).map((heartbeat) => heartbeat.close())); });

function claimed(config: Pick<CcrClientConfig, "request"> & Partial<CcrClientConfig>) {
  const client = new CcrClient({
    credentials: () => ({ token: "token", workerEpoch: 7 }),
    sessionIds: () => ["a"], sleep: async () => {}, random: () => 0,
    ...config,
  });
  const lost: CcrLeaseFailure[] = [];
  const heartbeat = new CcrHeartbeat({
    check: client.bindLease("a"), onLeaseLost: (failure) => { lost.push(failure); },
  }, new AbortController().signal, { schedule: () => () => {}, random: () => 0.5 });
  heartbeats.push(heartbeat);
  return { client, heartbeat, lost };
}

test("claimed writes keep waiting beyond three failures, reuse payloads and refresh only the token", async () => {
  for (const operation of ["persist", "events", "status", "ack"] as const) {
    const requests: CcrHttpRequest[] = [];
    const waits: number[] = [];
    const paused = Promise.withResolvers<void>();
    const resume = Promise.withResolvers<void>();
    let tokenVersion = 0;
    const { client } = claimed({
      credentials: () => ({ token: `token-${tokenVersion}`, workerEpoch: 7 }),
      request: async (request) => {
        requests.push({ ...request, body: structuredClone(request.body) });
        tokenVersion++;
        const status = [401, 503, 408, 429, 403, 200][requests.length - 1]!;
        return Response.json({ applied: 1 }, { status, headers: { "Retry-After": "99" } });
      },
      sleep: async (ms) => {
        waits.push(ms);
        if (waits.length === 4) { paused.resolve(); await resume.promise; }
      },
    });
    const data = { revision: 1 };
    const payloads = [{ type: "agent.message", uuid: "fixed", data }];
    const writing = operation === "persist" ? client.persist("a", "test_record", data)
      : operation === "events" ? client.events("a", payloads)
      : operation === "status" ? client.setStatus("a", "running") : client.acknowledge("a", "input");
    let confirmed = false;
    void writing.then(() => { confirmed = true; });
    await paused.promise;
    expect(requests).toHaveLength(4);
    expect(confirmed).toBe(false);
    data.revision = 99;
    resume.resolve();
    await writing;
    expect(requests).toHaveLength(6);
    expect(waits).toEqual([500, 1000, 2000, 30000, 8000]);
    expect(requests.map((request) => request.token)).toEqual(Array.from({ length: 6 }, (_, i) => `token-${i}`));
    for (const request of requests) {
      expect(request.body).toEqual(requests[0]!.body);
      expect(request.timeout).toBe(10_000);
    }
  }
});

test("one claimed write exhausts the shared auth budget and terminal lease errors never retry", async () => {
  for (const status of [401, 403, 409, 410]) {
    let calls = 0;
    const { client, lost } = claimed({ request: async () => { calls++; return new Response(null, { status }); } });
    await expect(client.persist("a", "dsh_log", {})).rejects.toThrow(String(status));
    expect(calls).toBe(status < 409 ? 10 : 1);
    expect(lost).toHaveLength(1);
    expect(lost[0]?.consecutiveFailures).toBe(calls);
  }
});

test("claimed writes fail without retrying permanent request errors or losing the lease", async () => {
  for (const status of [400, 404, 413, 422]) {
    for (const operation of ["persist", "events", "status", "ack"] as const) {
      const requests: CcrHttpRequest[] = [];
      const { client, lost } = claimed({
        request: async (request) => { requests.push(request); return new Response(null, { status }); },
      });
      const writing = operation === "persist" ? client.persist("a", "test_record", { revision: 1 })
        : operation === "events" ? client.events("a", [{ type: "agent.message", uuid: "fixed" }])
        : operation === "status" ? client.setStatus("a", "running") : client.acknowledge("a", "input");
      await expect(writing).rejects.toThrow(String(status));
      expect(requests).toHaveLength(1);
      expect(lost).toEqual([]);
    }
  }
});

test("a write cannot retry into a replacement epoch or leak its credentials", async () => {
  let epoch = 7;
  const requests: CcrHttpRequest[] = [];
  const { client, lost } = claimed({
    credentials: () => ({ token: `token-${epoch}`, workerEpoch: epoch }),
    request: async (request) => { requests.push(request); return new Response(null, { status: 503 }); },
    sleep: async () => { epoch = 8; },
  });
  await expect(client.events("a", [{ type: "agent.message" }])).rejects.toThrow("409");
  expect(requests).toHaveLength(1);
  expect(requests[0]!.token).toBe("token-7");
  expect(lost).toEqual([{
    status: "rejected", httpStatus: 409, reason: "epoch_mismatch", workerEpoch: 7, consecutiveFailures: 1,
  }]);
});

test("stopping cancels a long Retry-After sleep and in-flight writes without reporting success", async () => {
  for (const pendingRequest of [false, true]) {
    let calls = 0;
    let requestSignal: AbortSignal | undefined;
    const waiting = Promise.withResolvers<void>();
    const { client, heartbeat, lost } = claimed({
      request: async (request) => {
        calls++;
        requestSignal = request.signal;
        if (!pendingRequest) return new Response(null, { status: 429, headers: { "Retry-After": "3600" } });
        return new Promise<Response>((_resolve, reject) => {
          request.signal!.addEventListener("abort", () => reject(request.signal!.reason), { once: true });
          waiting.resolve();
        });
      },
      sleep: (ms, signal) => { const pending = sleep(ms, signal); waiting.resolve(); return pending; },
    });
    const writing = client.setStatus("a", "running");
    writing.catch(() => {});
    await waiting.promise;
    await heartbeat.close();
    await expect(writing).rejects.toThrow("CCRv2 session stopped");
    expect(requestSignal?.aborted).toBe(true);
    expect(calls).toBe(1);
    expect(lost).toEqual([]);
  }
});

test("ignored delivery is confirmed while unserializable event data is rejected", async () => {
  let calls = 0;
  const { client } = claimed({ request: async () => { calls++; return Response.json({ applied: 0 }); } });
  await client.acknowledge("a", "input");
  expect(calls).toBe(1);
  await expect(client.events("a", [{ type: "agent.message", value: 1n }])).rejects.toThrow();
  expect(calls).toBe(1);
});
