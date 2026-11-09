import { expect, test } from "bun:test";
import { SessionSeq, type SessionEvent } from "@deepseek-ai/dsh-session";
import { DsrClient } from "../src/dsr/client.ts";
import { DsrError, type HttpRequest } from "../src/dsr/http.ts";
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
    appendRecords: async (
      _id: string,
      _from: string,
      events: readonly SessionEvent[],
    ) => {
      calls.push([...events]);
      return send(events);
    },
  } as unknown as DsrClient;
  return { calls, remote: createRemoteTranscriptStore(() => client) };
}

function httpFixture(nextSeq: string) {
  const calls: HttpRequest[] = [];
  const client = new DsrClient(
    {
      request: async (request) => {
        calls.push(request);
        return Response.json({ next_offset: nextSeq });
      },
    },
    {
      code_session_id: "root",
      worker_epoch: "1",
      session_token: "session-token",
      model_token: "model-token",
      claim_expires_at: "2026-11-06T00:00:30Z",
    },
  );
  return { calls, remote: createRemoteTranscriptStore(() => client) };
}

test("an insufficient or malformed HTTP upload watermark is terminal", async () => {
  for (const watermark of ["0", "1", "-1", "02", "invalid"]) {
    const f = httpFixture(watermark);
    let failure: unknown;
    try {
      await f.remote.write("root", "root", [event(0), event(1)]);
    } catch (error) {
      failure = error;
    }
    expect(failure).toBeDefined();
    expect(f.remote.fenced(failure)).toBe(true);
  }
});

test("write accepts an acknowledged older prefix when the server watermark is ahead", async () => {
  for (const { nextSeq, events } of [
    { nextSeq: "3", events: [event(0), event(1)] },
    { nextSeq: "10", events: [event(8)] },
    { nextSeq: "9007199254740993", events: [event(0)] },
  ]) {
    const f = httpFixture(nextSeq);
    await f.remote.write("root", "root", events);
    expect(f.calls).toHaveLength(1);
    expect(f.calls[0]!.body).toEqual(
      events.map((event) => JSON.stringify(event) + "\n").join(""),
    );
  }
});

test("write accepts a decimal HTTP watermark for a fresh append and exact replay", async () => {
  const f = httpFixture("3");
  const events = [event(0), event(1)];
  await f.remote.write("root", "root", events);
  await f.remote.write("root", "root", events);
  expect(f.calls).toHaveLength(2);
  for (const request of f.calls) {
    expect(request.method).toBe("POST");
    expect(request.path).toBe(
      "/v1/dsr/sessions/root/transcripts/root/records?from_offset=1",
    );
    expect(request.body).toEqual(
      events.map((event) => JSON.stringify(event) + "\n").join(""),
    );
  }
});

test("write resends every batch when the server watermark already covers the full transcript", async () => {
  const f = httpFixture("406");
  const events = Array.from({ length: 405 }, (_, index) => event(index));
  await f.remote.write("root", "root", events);
  expect(f.calls.map((request) => request.body)).toEqual(
    [events.slice(0, 200), events.slice(200, 400), events.slice(400)].map(
      (batch) => batch.map((event) => JSON.stringify(event) + "\n").join(""),
    ),
  );
});

test("an oversized single event is rejected without issuing HTTP", async () => {
  const f = fixture(async () => "2");
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
  const f = fixture(async (events) => String(events.at(-1)!.seq + 2));
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
  const f = fixture(async (events) => String(events.at(-1)!.seq + 2));
  await f.remote.write(
    "root",
    "root",
    Array.from({ length: 405 }, (_, i) => event(i)),
  );
  expect(f.calls.map((batch) => batch.length)).toEqual([200, 200, 5]);
  const large = fixture(async () => "2");
  await large.remote.write("root", "root", [event(0, 1_500_000)]);
  expect(large.calls).toHaveLength(1);
});
