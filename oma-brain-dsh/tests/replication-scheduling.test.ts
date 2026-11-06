import { Context } from "@deepseek-ai/cordis";
import SessionStore, {
  SessionId,
  SessionSeq,
  type SessionEvent,
  type SessionHeader,
} from "@deepseek-ai/dsh-session";
import TranscriptPersistence from "../src/runtime/transcript-persistence.ts";
import { withDownloadedTranscript } from "../src/runtime/transcript-download.ts";
import { expect, test, spyOn } from "bun:test";
import {
  SessionReplication,
  type RemoteTranscriptStore,
} from "../src/runtime/replication.ts";
import { DsrError } from "../src/dsr/http.ts";
import { TranscriptUploadError } from "../src/runtime/transcript-batch.ts";
import type { DshLogData } from "../src/runtime/transcript-types.ts";

function event(seq: number): SessionEvent {
  return seq % 2 === 0
    ? {
        type: "turn/start",
        seq: SessionSeq(seq),
        time: 0,
        data: { turn: seq / 2 + 1 },
      }
    : {
        type: "turn/end",
        seq: SessionSeq(seq),
        time: 0,
        data: { turn: (seq + 1) / 2, reason: { kind: "completed" } },
      };
}

async function until(predicate: () => boolean) {
  const end = Date.now() + 2000;
  while (!predicate()) {
    if (Date.now() > end) throw new Error("Condition did not become true");
    await Bun.sleep(1);
  }
}

async function fixture(
  onWrite?: (
    id: string,
    records: readonly DshLogData[],
    signal?: AbortSignal,
  ) => Promise<void>,
  highWaterBytes = 32 * 1024 * 1024,
  lowWaterBytes = 16 * 1024 * 1024,
) {
  const ctx = new Context();
  await ctx.plugin(SessionStore);
  await ctx.plugin(TranscriptPersistence);
  const writes: { id: string; records: readonly DshLogData[] }[] = [];
  const remote: RemoteTranscriptStore = {
    read: async function* () {},
    write: async (_root, id, records, signal) => {
      writes.push({ id, records });
      await onWrite?.(id, records, signal);
    },
    fenced: (error) => error instanceof TranscriptUploadError,
  };
  await ctx.plugin(SessionReplication, {
    remote,
    highWaterBytes,
    lowWaterBytes,
  });
  return {
    ctx,
    writes,
    async seed(id: string, count: number, parent?: string) {
      const header: SessionHeader = {
        id: SessionId(id),
        version: 4,
        createdAt: 0,
        isSeeded: false,
        ...(parent ? { parentSession: SessionId(parent) } : {}),
      };
      if (parent) ctx.sessionReplication.bindThread(id, parent);
      await ctx.sessionReplication.ensure(id);
      await withDownloadedTranscript(
        header.id,
        (async function* () {
          yield [
            header,
            ...Array.from({ length: count }, (_, index) => event(index)),
          ];
        })(),
        () => {},
        async (remote) =>
          ctx.transcriptStore.replace(header.id, remote, () => {}),
      );
      return header;
    },
    async close() {
      ctx.sessionReplication.cancel("root");
      await ctx.fiber.dispose();
    },
  };
}

test.each([
  "cancel",
  "fenced",
  "drain",
  "forget",
  "replace",
  "abort",
  "late-child",
  "forgotten-earlier-member",
])("capacity wait rejects %s while capturing live events", async (mode) => {
  const entered = Promise.withResolvers<void>();
  const resume = Promise.withResolvers<void>();
  const abort = new AbortController();
  const failure = new TranscriptUploadError("capacity owner rejected");
  const f = await fixture(undefined, mode === "abort" ? 1000 : 1, 0);
  let capacity:
    | Promise<{ kind: "resolved" } | { kind: "rejected"; error: unknown }>
    | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    const activeID =
      mode === "forgotten-earlier-member" ? "active-child" : "root";
    if (activeID !== "root") {
      await f.seed("root", 1);
      await f.seed("previous-child", 1, "root");
      f.ctx.sessionReplication.bindThread(activeID, "root");
    }
    f.ctx.sessions.create(SessionId(activeID), {
      seed: [event(0)],
      meta: {
        createdAt: 0,
        ...(activeID !== "root" ? { parentSession: SessionId("root") } : {}),
      },
    });
    const capture = TranscriptPersistence.prototype.capture;
    spyOn(TranscriptPersistence.prototype, "capture").mockImplementation(
      async function (this: TranscriptPersistence, session) {
        if (session.id === activeID) {
          entered.resolve();
          await resume.promise;
        }
        await capture.call(this, session);
      },
    );
    capacity = f.ctx.sessionReplication
      .waitForCapacity("root", abort.signal)
      .then(
        () => ({ kind: "resolved" as const }),
        (error) => ({ kind: "rejected" as const, error }),
      );
    await entered.promise;
    if (mode === "cancel") f.ctx.sessionReplication.cancel("root", failure);
    else if (mode === "fenced") {
      const probe = f.ctx.sessionReplication as unknown as {
        jobs: Map<
          string,
          import("../src/runtime/replication-state.ts").ReplicationState
        >;
        rejectLease(
          id: string,
          owner: import("../src/runtime/replication-state.ts").ReplicationState,
          error: Error,
        ): void;
      };
      probe.rejectLease("root", probe.jobs.get("root")!, failure);
    } else if (mode === "drain")
      await f.ctx.sessionReplication.drain("root", 5).catch(() => {});
    else if (mode === "abort") abort.abort(failure);
    else if (mode === "late-child") {
      f.ctx.sessionReplication.bindThread("late-child", "root");
      f.ctx.sessionReplication.status("late-child");
      f.ctx.sessionReplication.cancel("late-child", failure);
    } else if (mode === "forgotten-earlier-member")
      f.ctx.sessionReplication.forget("previous-child");
    else {
      f.ctx.sessionReplication.forget("root");
      if (mode === "replace") f.ctx.sessionReplication.status("root");
    }
    resume.resolve();
    const result = await Promise.race([
      capacity,
      new Promise<{ kind: "timeout" }>((resolve) => {
        timer = setTimeout(() => resolve({ kind: "timeout" }), 1000);
      }),
    ]);
    expect(result.kind).toBe("rejected");
    if (result.kind === "rejected" && (mode === "fenced" || mode === "abort"))
      expect(result.error).toBe(failure);
    const probe = f.ctx.sessionReplication as unknown as {
      capacityWaiters: Map<string, Set<unknown>>;
    };
    expect(probe.capacityWaiters.size).toBe(0);
  } finally {
    clearTimeout(timer);
    resume.resolve();
    abort.abort(new Error("capacity test cleanup"));
    await capacity;
    spyOn(TranscriptPersistence.prototype, "capture").mockRestore();
    await f.close();
  }
});

test("terminal upload errors reject flush and capacity waiters across the root", async () => {
  const gate = Promise.withResolvers<void>();
  const failure = new TranscriptUploadError("invalid event");
  const f = await fixture(
    async () => {
      await gate.promise;
      throw failure;
    },
    1,
    0,
  );
  try {
    await f.seed("root", 3);
    const flushed = f.ctx.sessionReplication
      .flush("root")
      .catch((error) => error);
    await until(() => f.writes.length === 1);
    const capacity = f.ctx.sessionReplication
      .waitForCapacity("root")
      .catch((error) => error);
    gate.resolve();
    expect(await flushed).toBe(failure);
    expect(await capacity).toBe(failure);
    await expect(f.ctx.sessionReplication.flush("root")).rejects.toThrow(
      "invalid event",
    );
    expect(f.writes).toHaveLength(1);
  } finally {
    gate.resolve();
    await f.close();
  }
});

test("drain deadline aborts in-flight HTTP and rejects pending flushes", async () => {
  let aborted = false;
  const f = await fixture(async (_id, _records, signal) => {
    await new Promise<void>((_resolve, reject) => {
      signal!.addEventListener(
        "abort",
        () => {
          aborted = true;
          reject(signal!.reason);
        },
        { once: true },
      );
    });
  });
  try {
    await f.seed("root", 1);
    const flushed = f.ctx.sessionReplication
      .flush("root")
      .catch((error) => error);
    await until(() => f.writes.length === 1);
    await expect(f.ctx.sessionReplication.drain("root", 10)).rejects.toThrow(
      "timed out",
    );
    expect((await flushed).message).toContain("timed out");
    expect(aborted).toBe(true);
  } finally {
    await f.close();
  }
});

test("root and child batches alternate with one request in flight", async () => {
  const gate = Promise.withResolvers<void>();
  let active = 0,
    maximum = 0;
  const f = await fixture(async (id, records) => {
    active++;
    maximum = Math.max(maximum, active);
    try {
      if (id === "root" && "seq" in records[0]! && records[0].seq === 0)
        await gate.promise;
    } finally {
      active--;
    }
  });
  try {
    await f.seed("root", 450);
    await f.seed("child", 1, "root");
    const root = f.ctx.sessionReplication.flush("root");
    await until(() => f.writes.some((write) => "seq" in write.records[0]!));
    const duplicates = Array.from({ length: 20 }, () =>
      f.ctx.sessionReplication.flush("root"),
    );
    gate.resolve();
    await Promise.all([root, ...duplicates]);
    expect(maximum).toBe(1);
    const events = f.writes.filter((write) => "seq" in write.records[0]!);
    expect(
      events
        .filter((write) => write.id === "root")
        .map((write) => write.records.length),
    ).toEqual([200, 200, 50]);
    const childIndex = events.findIndex((write) => write.id === "child");
    expect(childIndex).toBeGreaterThan(0);
    expect(childIndex).toBeLessThan(events.length - 1);
  } finally {
    gate.resolve();
    await f.close();
  }
});

test("capacity waits include in-flight records and leave local append available", async () => {
  const gate = Promise.withResolvers<void>();
  const f = await fixture(
    async (_id, records) => {
      if ("seq" in records[0]!) await gate.promise;
    },
    100,
    0,
  );
  try {
    await f.seed("root", 3);
    const flushing = f.ctx.sessionReplication.flush("root");
    await until(() => f.writes.length === 2);
    const status = f.ctx.sessionReplication.status("root");
    expect(status.inFlight).toBe(true);
    expect(status.pendingEvents).toBe(3);
    expect(status.pendingBytes).toBeGreaterThan(100);
    let resumed = false;
    const capacity = f.ctx.sessionReplication
      .waitForCapacity("root")
      .then(() => {
        resumed = true;
      });
    const handle = await f.ctx.sessionPersistence.open(
      SessionId("root"),
      "write",
    );
    try {
      await handle.append([event(3)]);
      let confirmed = false;
      const persisted = handle.flush().then(() => {
        confirmed = true;
      });
      await Bun.sleep(5);
      expect(confirmed).toBe(false);
      expect(resumed).toBe(false);
      gate.resolve();
      await Promise.all([flushing, capacity, persisted]);
      expect(resumed).toBe(true);
      await f.ctx.sessionReplication.flush("root");
      expect(f.ctx.sessionReplication.status("root").pendingEvents).toBe(0);
    } finally {
      gate.resolve();
      await handle.close();
    }
  } finally {
    gate.resolve();
    await f.close();
  }
});

test("a flush completes at its captured target while later events are still uploading", async () => {
  const first = Promise.withResolvers<void>(),
    second = Promise.withResolvers<void>();
  const f = await fixture(async (_id, records) => {
    if (!("seq" in records[0]!)) return;
    if (records[0].seq === 0) await first.promise;
    else await second.promise;
  });
  try {
    await f.seed("root", 1);
    const early = f.ctx.sessionReplication.flush("root");
    await until(() => f.writes.length === 2);
    const handle = await f.ctx.sessionPersistence.open(
      SessionId("root"),
      "write",
    );
    try {
      await handle.append([event(1)]);
      first.resolve();
      await early;
      const later = f.ctx.sessionReplication.flush("root");
      await until(() => f.writes.length === 3);
      expect(f.ctx.sessionReplication.status("root").pendingEvents).toBe(1);
      second.resolve();
      await later;
    } finally {
      first.resolve();
      second.resolve();
      await handle.close();
    }
  } finally {
    first.resolve();
    second.resolve();
    await f.close();
  }
});

test("a lost response retries the same sequence without advancing confirmation", async () => {
  let lost = true;
  const accepted = new Map<number, DshLogData>();
  const f = await fixture(async (_id, records) => {
    if (!("seq" in records[0]!)) return;
    for (const record of records)
      if ("seq" in record) accepted.set(record.seq, record);
    if (lost) {
      lost = false;
      throw new Error("response lost after commit");
    }
  });
  try {
    await f.seed("root", 3);
    await expect(f.ctx.sessionReplication.flush("root")).rejects.toThrow(
      "response lost",
    );
    expect(f.ctx.sessionReplication.status("root").pendingEvents).toBe(3);
    await f.ctx.sessionReplication.flush("root");
    expect(accepted.size).toBe(3);
    expect(
      f.writes
        .filter((write) => "seq" in write.records[0]!)
        .map((write) => write.records),
    ).toEqual([
      [event(0), event(1), event(2)],
      [event(0), event(1), event(2)],
    ]);
    expect(f.ctx.sessionReplication.status("root").pendingEvents).toBe(0);
  } finally {
    await f.close();
  }
});

test("a failed later batch preserves earlier batch confirmations", async () => {
  let fail = true;
  const f = await fixture(async (_id, records) => {
    if ("seq" in records[0]! && records[0].seq === 200 && fail) {
      fail = false;
      throw new Error("temporary outage");
    }
  });
  try {
    await f.seed("root", 450);
    await expect(f.ctx.sessionReplication.flush("root")).rejects.toThrow(
      "temporary outage",
    );
    expect(f.ctx.sessionReplication.status("root").pendingEvents).toBe(250);
    await f.ctx.sessionReplication.flush("root");
    const starts = f.writes.flatMap((write) =>
      "seq" in write.records[0]! ? [Number(write.records[0].seq)] : [],
    );
    expect(starts).toEqual([0, 200, 200, 400]);
  } finally {
    await f.close();
  }
});

test("capacity resumes at the low watermark before the entire transcript drains", async () => {
  const first = Promise.withResolvers<void>(),
    last = Promise.withResolvers<void>();
  const f = await fixture(
    async (_id, records) => {
      if (!("seq" in records[0]!)) return;
      if (records[0].seq === 0) await first.promise;
      if (records[0].seq === 400) await last.promise;
    },
    20_000,
    10_000,
  );
  try {
    await f.seed("root", 450);
    const flushed = f.ctx.sessionReplication.flush("root");
    await until(() => f.writes.length === 2);
    const capacity = f.ctx.sessionReplication.waitForCapacity("root");
    first.resolve();
    await capacity;
    await until(() => f.writes.length === 4);
    const status = f.ctx.sessionReplication.status("root");
    expect(status.pendingEvents).toBe(50);
    expect(status.pendingBytes).toBeLessThanOrEqual(10_000);
    expect(status.inFlight).toBe(true);
    last.resolve();
    await flushed;
  } finally {
    first.resolve();
    last.resolve();
    await f.close();
  }
});

test("a transient parent header failure does not spin or starve child recovery", async () => {
  let fail = true;
  const f = await fixture(async (id, records) => {
    if (id === "root" && !("seq" in records[0]!) && fail) {
      fail = false;
      throw new Error("header unavailable");
    }
  });
  try {
    await f.seed("root", 2);
    await f.seed("child", 2, "root");
    await expect(f.ctx.sessionReplication.flush("root")).rejects.toThrow(
      "header unavailable",
    );
    await f.ctx.sessionReplication.flush("root");
    expect(
      f.writes.some(
        (write) => write.id === "child" && "seq" in write.records[0]!,
      ),
    ).toBe(true);
  } finally {
    await f.close();
  }
});

test("a transcript gap stops the owner instead of comparing local and remote prefixes", async () => {
  const f = await fixture(async () => {
    throw new DsrError(409, "transcript_gap", "1");
  });
  try {
    await f.seed("root", 3);
    const remote = f.ctx.sessionReplication.config.remote;
    const fenced = remote.fenced;
    remote.fenced = (error) =>
      fenced(error) ||
      (error instanceof DsrError && error.code === "transcript_gap");
    let reads = 0;
    remote.read = async function* () {
      reads++;
    };
    await expect(f.ctx.sessionReplication.flush("root")).rejects.toThrow(
      "transcript_gap",
    );
    await expect(f.ctx.sessionReplication.flush("root")).rejects.toThrow(
      "transcript_gap",
    );
    expect(reads).toBe(0);
  } finally {
    await f.close();
  }
});
