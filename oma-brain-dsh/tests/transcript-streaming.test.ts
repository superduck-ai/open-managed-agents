import { Context } from "@deepseek-ai/cordis";
import SessionStore, {
  SessionId,
  SessionSeq,
  type SessionEvent,
  type SessionHeader,
} from "@deepseek-ai/dsh-session";
import * as JsonlPersistence from "@deepseek-ai/dsh-session-persistence-jsonl";
import { expect, test } from "bun:test";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { DsrClient } from "../src/dsr/client.ts";
import { createRemoteTranscriptStore } from "../src/dsr/replication.ts";
import {
  SessionReplication,
  type RemoteTranscriptStore,
} from "../src/runtime/replication.ts";
import { withDownloadedTranscript } from "../src/runtime/transcript-download.ts";
import type { DshLogData } from "../src/runtime/transcript-types.ts";

const header: SessionHeader = {
  id: SessionId("streaming-test"),
  version: 4,
  createdAt: 0,
  isSeeded: false,
};
const event = (seq: number): SessionEvent =>
  seq % 2 === 0
    ? {
        type: "turn/start",
        seq: SessionSeq(seq),
        time: 1,
        data: { turn: seq / 2 + 1 },
      }
    : {
        type: "turn/end",
        seq: SessionSeq(seq),
        time: 1,
        data: { turn: (seq + 1) / 2, reason: { kind: "completed" } },
      };
async function* pages(...batches: DshLogData[][]) {
  yield* batches;
}
async function fixture(read: RemoteTranscriptStore["read"]) {
  const root = await mkdtemp(join(tmpdir(), "oma-streaming-test-"));
  const ctx = new Context();
  await ctx.plugin(SessionStore);
  await ctx.plugin(JsonlPersistence, { root, compression: "none" });
  const writes: DshLogData[][] = [];
  await ctx.plugin(SessionReplication, {
    remote: {
      read,
      write: async (_root, _id, records) => {
        writes.push([...records]);
      },
      fenced: () => false,
    },
  });
  return {
    ctx,
    writes,
    async close() {
      await ctx.fiber.dispose();
      await rm(root, { recursive: true, force: true });
    },
  };
}
async function seed(ctx: Context, events: SessionEvent[]) {
  const handle = await ctx.sessionPersistence.create(header);
  try {
    await handle.append(events);
    await handle.flush();
  } finally {
    await handle.close();
  }
}
async function stored(ctx: Context) {
  const handle = await ctx.sessionPersistence.open(header.id, "read");
  try {
    return [...(await handle.read()).events];
  } finally {
    await handle.close();
  }
}

test("invalid and conflicting streamed records never reach the consumer", async () => {
  const invalid: DshLogData[][][] = [
    [[event(0)]],
    [[{ ...header, id: SessionId("other") }]],
    [[{ ...header, isSeeded: true }]],
    [[header, event(1)]],
    [[header, event(0)], [{ ...event(2), seq: SessionSeq(1) }]],
    [[header, event(0)], [{ ...event(0), time: 2 }]],
    [[header, event(0)], [{ ...header, createdAt: 2 }]],
    [[header], [{ ...event(0), type: "unknown-required" } as never]],
  ];
  for (const batches of invalid) {
    let consumed = false;
    await expect(
      withDownloadedTranscript(
        header.id,
        pages(...batches),
        () => {},
        async () => {
          consumed = true;
        },
      ),
    ).rejects.toThrow();
    expect(consumed).toBe(false);
  }
});
test("a failed later page preserves the local transcript and can be retried", async () => {
  let fail = true;
  const f = await fixture(async function* () {
    yield [header, event(0), event(1)];
    if (fail) throw new Error("download failed");
    yield [event(2)];
  });
  try {
    await seed(f.ctx, [event(0)]);
    await expect(f.ctx.sessionReplication.ensure(header.id)).rejects.toThrow(
      "download failed",
    );
    expect(await stored(f.ctx)).toEqual([event(0)]);
    expect(f.writes).toEqual([]);
    fail = false;
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(true);
    expect(await stored(f.ctx)).toEqual([event(0), event(1), event(2)]);
  } finally {
    await f.close();
  }
});
test("a conflicting local prefix is preserved without appending the remote suffix", async () => {
  const f = await fixture(() =>
    pages([header, event(0)], [event(1), event(2)]),
  );
  const local = [event(0), { ...event(1), time: 99 }];
  try {
    await seed(f.ctx, local);
    await expect(f.ctx.sessionReplication.ensure(header.id)).rejects.toThrow(
      "histories diverged",
    );
    expect(await stored(f.ctx)).toEqual(local);
    expect(f.writes).toEqual([]);
  } finally {
    await f.close();
  }
});
test("ownership loss closes the source and prevents hydration", async () => {
  let active = true,
    closed = false,
    consumed = false;
  const source = (async function* () {
    try {
      yield [header, event(0)];
      active = false;
      yield [event(1)];
      throw new Error("must not request next page");
    } finally {
      closed = true;
    }
  })();
  await expect(
    withDownloadedTranscript(
      header.id,
      source,
      () => {
        if (!active) throw new Error("owner lost");
      },
      async () => {
        consumed = true;
      },
    ),
  ).rejects.toThrow("owner lost");
  expect(closed).toBe(true);
  expect(consumed).toBe(false);
});
test("failed downloads and failed consumers remove their temporary files", async () => {
  for (const failDownload of [true, false]) {
    const before = new Set(await readdir(tmpdir()));
    let root: string | undefined;
    const source = (async function* () {
      yield [header, event(0)];
      const names = (await readdir(tmpdir())).filter(
        (name) => name.startsWith("oma-brain-transcript-") && !before.has(name),
      );
      expect(names).toHaveLength(1);
      root = join(tmpdir(), names[0]!);
      if (failDownload) throw new Error("download failure");
    })();
    await expect(
      withDownloadedTranscript(
        header.id,
        source,
        () => {},
        async () => {
          throw new Error("consumer failure");
        },
      ),
    ).rejects.toThrow(failDownload ? "download failure" : "consumer failure");
    expect(root).toBeDefined();
    expect(await readdir(root!).catch(() => undefined)).toBeUndefined();
  }
});

test("publishing refuses a remote log that is longer than the local prefix", async () => {
  const f = await fixture(() =>
    pages([header, event(0)], [event(1), event(2)]),
  );
  try {
    await seed(f.ctx, [event(0)]);
    await expect(f.ctx.sessionReplication.flush(header.id)).rejects.toThrow(
      "histories diverged",
    );
    expect(f.writes).toEqual([]);
    expect(await stored(f.ctx)).toEqual([event(0)]);
  } finally {
    await f.close();
  }
});

test("a page is on disk before the source produces its next page and temporary files are cleaned", async () => {
  const before = new Set(await readdir(tmpdir()));
  let path: string | undefined;
  const source = (async function* () {
    yield [header, event(0)];
    const names = (await readdir(tmpdir())).filter(
      (name) => name.startsWith("oma-brain-transcript-") && !before.has(name),
    );
    expect(names).toHaveLength(1);
    path = join(tmpdir(), names[0]!, "events.jsonl");
    expect(
      (await readFile(path, "utf8"))
        .trim()
        .split("\n")
        .map((line) => JSON.parse(line)),
    ).toEqual([event(0)]);
    yield [event(1)];
  })();
  await withDownloadedTranscript(
    header.id,
    source,
    () => {},
    async (transcript) => {
      expect(transcript!.count).toBe(2);
      const events: SessionEvent[] = [];
      for await (const batch of transcript!.batches()) events.push(...batch);
      expect(events).toEqual([event(0), event(1)]);
    },
  );
  expect(path).toBeDefined();
  expect(await readFile(path!, "utf8").catch(() => undefined)).toBeUndefined();
});
test("fresh hydration appends bounded batches without fetching again after acquisition", async () => {
  let reads = 0;
  const events = Array.from({ length: 650 }, (_, index) => event(index));
  const f = await fixture(async function* () {
    reads++;
    yield [header, ...events.slice(0, 250)];
    yield events.slice(250);
  });
  try {
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(true);
    await withDownloadedTranscript(
      header.id,
      pages([header, ...events]),
      () => {},
      async (transcript) => {
        const sizes = [];
        for await (const batch of transcript!.batches())
          sizes.push(batch.length);
        expect(sizes).toEqual([200, 200, 200, 50]);
      },
    );
    expect(await stored(f.ctx)).toEqual(events);
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(false);
    expect(reads).toBe(1);
  } finally {
    await f.close();
  }
});
test("a longer local prefix uploads only its unconfirmed tail", async () => {
  const f = await fixture(() => pages([header, event(0)]));
  try {
    await seed(f.ctx, [event(0), event(1), event(2)]);
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(false);
    await f.ctx.sessionReplication.flush(header.id);
    expect(f.writes).toEqual([[event(1), event(2)]]);
  } finally {
    await f.close();
  }
});
test("empty and header-only streams retain distinct persistence semantics", async () => {
  for (const exists of [false, true]) {
    const f = await fixture(() => (exists ? pages([header]) : pages()));
    try {
      expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(exists);
      expect(!!(await f.ctx.sessionPersistence.stat(header.id))).toBe(exists);
    } finally {
      await f.close();
    }
  }
});
test("identical overlap on and between pages does not duplicate events", async () => {
  await withDownloadedTranscript(
    header.id,
    pages([header, event(0), event(0)], [event(0), event(1)]),
    () => {},
    async (transcript) => {
      expect(transcript!.count).toBe(2);
      const batches = [];
      for await (const batch of transcript!.batches()) batches.push(batch);
      expect(batches).toEqual([[event(0), event(1)]]);
    },
  );
});
test("the DSR adapter fetches another page only when its consumer advances", async () => {
  const calls: string[] = [];
  const client = {
    async listTranscripts() {
      return [{ dsh_session_id: header.id }];
    },
    async readTranscript(_id: string, cursor: string) {
      calls.push(cursor);
      return {
        header: cursor === "0" ? header : null,
        events: [event(Number(cursor))],
        next_from_seq: cursor === "0" ? "1" : null,
      };
    },
  } as unknown as DsrClient;
  const iterator = createRemoteTranscriptStore(() => client)
    .read(header.id, header.id)
    [Symbol.asyncIterator]();
  expect(calls).toEqual([]);
  expect((await iterator.next()).value).toEqual([header, event(0)]);
  expect(calls).toEqual(["0"]);
  await iterator.return?.();
  expect(calls).toEqual(["0"]);
});
