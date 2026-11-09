import { expect, jest, spyOn, test } from "bun:test";
import { DsrClient } from "../src/dsr/client.ts";
import { DsrInputStream } from "../src/dsr/input-stream.ts";
import { errorOf, type HttpRequest, type Transport } from "../src/dsr/http.ts";

function client(transport: Transport) {
  return new DsrClient(transport, {
    code_session_id: "cse_root",
    worker_epoch: "1",
    session_token: "token",
    model_token: "model",
    claim_expires_at: new Date(Date.now() + 30000).toISOString(),
  });
}

for (const mode of ["eof", "network", "503"] as const) {
  test(`stream ${mode} consumes the retry budget without resetting at HTTP 200`, async () => {
    let calls = 0;
    const sleeps: number[] = [];
    const stream = new DsrInputStream(
      client({
        request: async () => {
          calls++;
          if (mode === "network") throw new TypeError("offline");
          return new Response("", { status: mode === "503" ? 503 : 200 });
        },
        sleep: async (ms) => {
          sleeps.push(ms);
        },
      }),
    );
    await expect(
      stream.read(new AbortController().signal).next(),
    ).rejects.toBeInstanceOf(Error);
    expect(calls).toBe(10);
    expect(sleeps).toHaveLength(9);
    expect(sleeps[0]).toBeGreaterThanOrEqual(750);
    expect(sleeps[0]).toBeLessThanOrEqual(1250);
    expect(sleeps[8]).toBeGreaterThanOrEqual(22500);
  });
}

for (const data of [
  "{",
  '{"id":"","lane":"control","kind":"user.interrupt"}',
  '"' + "x".repeat(4 * 1024 * 1024) + '"',
]) {
  test(`invalid protocol input terminates without reconnect (${data.length} bytes)`, async () => {
    let calls = 0;
    const stream = new DsrInputStream(
      client({
        request: async () => {
          calls++;
          return new Response(`event: input\ndata: ${data}\n\n`);
        },
        sleep: async () => {
          throw new Error("unexpected retry");
        },
      }),
    );
    await expect(
      stream.read(new AbortController().signal).next(),
    ).rejects.toMatchObject({ code: "invalid_input" });
    expect(calls).toBe(1);
  });
}

test("Retry-After is optional and SSE respects the server's minimum delay", async () => {
  expect(
    (await errorOf(new Response(null, { status: 503 }))).retryAfterMs,
  ).toBeUndefined();
  const abort = new AbortController();
  const waits: number[] = [];
  const stream = new DsrInputStream(
    client({
      request: async () =>
        new Response(null, { status: 429, headers: { "Retry-After": "7" } }),
      sleep: async (ms) => {
        waits.push(ms);
        abort.abort();
        abort.signal.throwIfAborted();
      },
    }),
  );
  expect((await stream.read(abort.signal).next()).done).toBe(true);
  expect(waits).toEqual([7000]);
});

test("waiting for response headers has a deadline", async () => {
  jest.useFakeTimers();
  try {
    let request: HttpRequest | undefined;
    const abort = new AbortController();
    const stream = new DsrInputStream(
      client({
        request: (options) => {
          request = options;
          return new Promise((_, reject) =>
            options.signal!.addEventListener(
              "abort",
              () => reject(options.signal!.reason),
              { once: true },
            ),
          );
        },
        sleep: async () => {
          abort.abort();
        },
      }),
    );
    const pending = stream.read(abort.signal).next();
    expect(request!.signal!.aborted).toBe(false);
    jest.advanceTimersByTime(15000);
    expect(request!.signal!.aborted).toBe(true);
    expect((await pending).done).toBe(true);
  } finally {
    jest.useRealTimers();
  }
});

test("control reconnect does not advertise an unsupported cursor", async () => {
  const calls: HttpRequest[] = [];
  const abort = new AbortController();
  const stream = new DsrInputStream(
    client({
      request: async (request) => {
        calls.push(request);
        return new Response(
          'id: 9007199254740993\nevent: input\ndata: {"id":"a","lane":"control","kind":"user.interrupt"}\n\n',
        );
      },
      sleep: async () => {},
    }),
  ).read(abort.signal);
  expect((await stream.next()).value).toMatchObject({ id: "a" });
  expect((await stream.next()).value).toMatchObject({ id: "a" });
  expect(calls[1]!.headers!["Last-Event-ID"]).toBeUndefined();
  expect(calls[1]!.path).toEndWith("/inputs/stream");
  abort.abort();
  await stream.return(undefined);
});

test("a stable stream resets the earlier failure budget", async () => {
  let now = 0;
  const clock = spyOn(Date, "now").mockImplementation(() => now);
  let calls = 0;
  try {
    const abort = new AbortController();
    const frame = (id: string) =>
      `event: input\ndata: {"id":"${id}","lane":"control","kind":"user.interrupt"}\n\n`;
    const stream = new DsrInputStream(
      client({
        request: async () => {
          calls++;
          return new Response(calls === 10 ? frame("a") + frame("b") : "");
        },
        sleep: async () => {},
      }),
    ).read(abort.signal);
    expect((await stream.next()).value).toMatchObject({ id: "a" });
    expect(calls).toBe(10);
    now = 60000;
    expect((await stream.next()).value).toMatchObject({ id: "b" });
    await expect(stream.next()).rejects.toThrow("DSR input stream ended");
    expect(calls).toBe(19);
  } finally {
    clock.mockRestore();
  }
});
