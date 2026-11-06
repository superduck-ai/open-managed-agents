import { expect, test } from "bun:test";
import { SessionSeq, type SessionEvent } from "@deepseek-ai/dsh-session";
import type { DsrClient } from "../src/dsr/client.ts";
import { DsrError } from "../src/dsr/http.ts";
import { createRemoteTranscriptStore } from "../src/dsr/replication.ts";

function event(seq: number, length = 0): SessionEvent {
  return {
    type: "turn/start",
    seq: SessionSeq(seq),
    time: 0,
    data: { turn: 1 },
    padding: "鸭".repeat(length),
  } as SessionEvent;
}

function fixture(send: (events: readonly SessionEvent[]) => Promise<string>) {
  const calls: SessionEvent[][] = [];
  const client = {
    appendEvents: async (_id: string, events: readonly SessionEvent[]) => {
      calls.push([...events]);
      return send(events);
    },
  } as unknown as DsrClient;
  return { calls, remote: createRemoteTranscriptStore(() => client) };
}

test("an insufficient or impossible upload watermark is terminal", async () => {
  for (const watermark of ["0", "3"]) {
    const f = fixture(async () => watermark);
    let failure: unknown;
    try {
      await f.remote.write("root", "root", [event(0)]);
    } catch (error) {
      failure = error;
    }
    expect(failure).toBeDefined();
    expect(f.remote.fenced(failure)).toBe(true);
  }
});

test("an oversized single event is rejected without issuing HTTP", async () => {
  const f = fixture(async () => "1");
  let failure: unknown;
  try {
    await f.remote.write("root", "root", [event(0, 6 * 1024 * 1024)]);
  } catch (error) {
    failure = error;
  }
  expect(f.calls).toHaveLength(0);
  expect(f.remote.fenced(failure)).toBe(true);
});

test("a single event rejected by the server does not remain retryable", async () => {
  const f = fixture(async () => {
    throw new DsrError(413, "payload_too_large");
  });
  let failure: unknown;
  try {
    await f.remote.write("root", "root", [event(0)]);
  } catch (error) {
    failure = error;
  }
  expect(f.calls).toHaveLength(1);
  expect(f.remote.fenced(failure)).toBe(true);
});

test("a batch rejected with 413 is terminal and is never split", async () => {
  const f = fixture(async () => {
    throw new DsrError(413, "payload_too_large");
  });
  let failure: unknown;
  try {
    await f.remote.write("root", "root", [event(0), event(1)]);
  } catch (error) {
    failure = error;
  }
  expect(f.calls.map((batch) => batch.map((item) => Number(item.seq)))).toEqual(
    [[0, 1]],
  );
  expect(f.remote.fenced(failure)).toBe(true);
});

test("uploads budget UTF-8 request bytes before sending and retain order", async () => {
  const f = fixture(async (events) => String(events.at(-1)!.seq + 1));
  await f.remote.write("root", "root", [
    event(0, 750_000),
    event(1, 750_000),
    event(2, 750_000),
  ]);
  expect(f.calls.map((batch) => batch.map((item) => Number(item.seq)))).toEqual(
    [[0], [1], [2]],
  );
  for (const events of f.calls)
    expect(Buffer.byteLength(JSON.stringify({ events }))).toBeLessThanOrEqual(
      4 * 1024 * 1024,
    );
});

test("small events fill count bounded batches and oversized targets travel alone", async () => {
  const f = fixture(async (events) => String(events.at(-1)!.seq + 1));
  await f.remote.write(
    "root",
    "root",
    Array.from({ length: 405 }, (_, i) => event(i)),
  );
  expect(f.calls.map((batch) => batch.length)).toEqual([200, 200, 5]);
  const large = fixture(async () => "1");
  await large.remote.write("root", "root", [event(0, 1_500_000)]);
  expect(large.calls).toHaveLength(1);
});
