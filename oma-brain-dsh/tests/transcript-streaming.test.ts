import { Context } from "@deepseek-ai/cordis";
import SessionStore, {
  SessionId,
  SessionSeq,
  type SessionEvent,
  type SessionHeader,
} from "@deepseek-ai/dsh-session";
import TranscriptPersistence from "../src/runtime/transcript-persistence.ts";
import { expect, test } from "bun:test";
import type { DsrClient } from "../src/dsr/client.ts";
import { DsrError } from "../src/dsr/http.ts";
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
  const ctx = new Context();
  await ctx.plugin(SessionStore);
  await ctx.plugin(TranscriptPersistence);
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
    },
  };
}
async function seed(ctx: Context, events: SessionEvent[], value = header) {
  await withDownloadedTranscript(
    value.id,
    pages([value, ...events]),
    () => {},
    async (remote) => {
      ctx.transcriptStore.replace(value.id, remote, () => {});
    },
  );
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
test("the restore boundary rejects an incomplete custom source before replacing cache", async () => {
  const f = await fixture(() => pages([header, event(0)]));
  try {
    await seed(f.ctx, [event(0), event(1)]);
    await expect(
      f.ctx.sessionReplication.ensure(header.id, { nextOffset: "3" }),
    ).rejects.toThrow("directory watermark");
    expect(await stored(f.ctx)).toEqual([event(0), event(1)]);
  } finally {
    await f.close();
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
test("a conflicting local prefix is replaced with the validated remote transcript", async () => {
  const f = await fixture(() =>
    pages([header, event(0)], [event(1), event(2)]),
  );
  const local = [event(0), { ...event(1), time: 99 }];
  try {
    await seed(f.ctx, local);
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(true);
    expect(await stored(f.ctx)).toEqual([event(0), event(1), event(2)]);
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
test("a failed consumer can retry the download without filesystem state", async () => {
  await expect(
    withDownloadedTranscript(
      header.id,
      pages([header, event(0)]),
      () => {},
      async () => {
        throw new Error("consumer failure");
      },
    ),
  ).rejects.toThrow("consumer failure");
  await withDownloadedTranscript(
    header.id,
    pages([header, event(0)]),
    () => {},
    async (remote) => {
      expect(remote?.events).toEqual([event(0)]);
    },
  );
});

test("publishing refuses a remote log that is longer than the local prefix", async () => {
  const f = await fixture(() =>
    pages([header, event(0)], [event(1), event(2)]),
  );
  try {
    await seed(f.ctx, [event(0)]);
    await expect(f.ctx.sessionReplication.flush(header.id)).rejects.toThrow(
      "Transcript must be restored before uploading",
    );
    expect(f.writes).toEqual([]);
    expect(await stored(f.ctx)).toEqual([event(0)]);
  } finally {
    await f.close();
  }
});

test("pages are consumed incrementally and retained as validated events without files", async () => {
  let pagesRead = 0;
  const source = (async function* () {
    pagesRead++;
    yield [header, event(0)];
    pagesRead++;
    yield [event(1)];
  })();
  await withDownloadedTranscript(
    header.id,
    source,
    () => {},
    async (remote) => {
      expect(pagesRead).toBe(2);
      expect(remote!.count).toBe(2);
      expect(remote!.events).toEqual([event(0), event(1)]);
      expect(Object.isFrozen(remote!.events[0])).toBe(true);
    },
  );
});
test("hydration consumes pages once and reuses the acquired transcript", async () => {
  let reads = 0;
  const events = Array.from({ length: 650 }, (_, index) => event(index));
  const f = await fixture(async function* () {
    reads++;
    yield [header, ...events.slice(0, 250)];
    yield events.slice(250);
  });
  try {
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(true);
    expect(await stored(f.ctx)).toEqual(events);
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(false);
    expect(reads).toBe(1);
  } finally {
    await f.close();
  }
});
test("a longer local prefix is discarded without uploading its unconfirmed tail", async () => {
  const f = await fixture(() => pages([header, event(0)]));
  try {
    await seed(f.ctx, [event(0), event(1), event(2)]);
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(true);
    expect(await stored(f.ctx)).toEqual([event(0)]);
    await f.ctx.sessionReplication.flush(header.id);
    expect(f.writes).toEqual([]);
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
      expect(transcript!.events).toEqual([event(0), event(1)]);
    },
  );
});
test("the DSR adapter fetches another page only when its consumer advances", async () => {
  const calls: string[] = [];
  const client = {
    async listTranscripts() {
      return [{ dsh_session_id: header.id, next_offset: "3" }];
    },
    async readTranscript(_id: string, cursor: string) {
      calls.push(cursor);
      return {
        records: cursor === "0" ? [header, event(0)] : [event(1)],
        next_offset: "3",
        next_from_offset: cursor === "0" ? "2" : null,
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

test("invalid remote history preserves an existing local cache", async () => {
  const f = await fixture(() => pages([header, event(1)]));
  try {
    await seed(f.ctx, [event(0)]);
    await expect(f.ctx.sessionReplication.ensure(header.id)).rejects.toThrow();
    expect(await stored(f.ctx)).toEqual([event(0)]);
    expect(f.writes).toEqual([]);
  } finally {
    await f.close();
  }
});

test("an active writer prevents cache replacement", async () => {
  const f = await fixture(() => pages([header, event(0), event(1)]));
  try {
    await seed(f.ctx, [event(0)]);
    const handle = await f.ctx.sessionPersistence.open(header.id, "write");
    try {
      await expect(
        f.ctx.sessionReplication.ensure(header.id),
      ).rejects.toThrow();
      expect(await stored(f.ctx)).toEqual([event(0)]);
    } finally {
      f.ctx.transcriptStore.release(header.id, handle);
      await handle.close();
    }
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(true);
    expect(await stored(f.ctx)).toEqual([event(0), event(1)]);
  } finally {
    await f.close();
  }
});

test("replacement invalidates previously open read handles", async () => {
  const f = await fixture(() => pages([header, event(0), event(1)]));
  try {
    await seed(f.ctx, [event(0)]);
    const previous = await f.ctx.sessionPersistence.open(header.id, "read");
    await f.ctx.sessionReplication.ensure(header.id);
    await expect(previous.read()).rejects.toThrow();
    await previous.close();
    expect(await stored(f.ctx)).toEqual([event(0), event(1)]);
  } finally {
    await f.close();
  }
});

test("remote header and empty event history replace local metadata and events", async () => {
  const remoteHeader = { ...header, cwd: "/remote/project", createdAt: 123 };
  const f = await fixture(() => pages([remoteHeader]));
  try {
    await seed(f.ctx, [event(0)]);
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(true);
    expect(await stored(f.ctx)).toEqual([]);
    expect(
      (await f.ctx.sessionPersistence.stat(header.id))!.header,
    ).toMatchObject(remoteHeader);
    expect((await f.ctx.sessionPersistence.list()).length).toBe(1);
  } finally {
    await f.close();
  }
});

test("a missing remote transcript removes only the matching local cache", async () => {
  const f = await fixture(() => pages());
  try {
    await seed(f.ctx, [event(0)]);
    const other = { ...header, id: SessionId("unrelated") };
    await seed(f.ctx, [], other);
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(false);
    expect(await f.ctx.sessionPersistence.stat(header.id)).toBeUndefined();
    expect(
      (await f.ctx.sessionPersistence.stat(other.id))!.header,
    ).toMatchObject(other);
    await f.ctx.sessionReplication.flush(header.id);
    expect(f.writes).toEqual([]);
  } finally {
    await f.close();
  }
});

test("a newly created writer prevents remote cache replacement", async () => {
  const f = await fixture(() => pages([header, event(0), event(1)]));
  try {
    const handle = await f.ctx.sessionPersistence.create(header);
    try {
      await expect(
        f.ctx.sessionReplication.ensure(header.id),
      ).rejects.toThrow();
    } finally {
      f.ctx.transcriptStore.release(header.id, handle);
      await handle.close();
    }
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(true);
    expect(await stored(f.ctx)).toEqual([event(0), event(1)]);
  } finally {
    await f.close();
  }
});

test.each(["ses/特殊~", ".."])(
  "restoration preserves opaque session IDs including %s",
  async (value) => {
    const escaped = { ...header, id: SessionId(value) };
    const f = await fixture(() => pages([escaped, event(0), event(1)]));
    try {
      await seed(f.ctx, [event(0)], escaped);
      expect(await f.ctx.sessionReplication.ensure(escaped.id)).toBe(true);
      const reader = await f.ctx.sessionPersistence.open(escaped.id, "read");
      try {
        expect((await reader.read()).events).toEqual([event(0), event(1)]);
      } finally {
        await reader.close();
      }
    } finally {
      await f.close();
    }
  },
);

test("ownership lost at publication preserves the previous transcript", async () => {
  const f = await fixture(() => pages([header, event(0), event(1)]));
  try {
    await seed(f.ctx, [event(0)]);
    await expect(
      withDownloadedTranscript(
        header.id,
        pages([header, event(0), event(1)]),
        () => {},
        async (remote) => {
          f.ctx.transcriptStore.replace(header.id, remote, () => {
            throw new Error("owner lost");
          });
        },
      ),
    ).rejects.toThrow("owner lost");
    expect(await stored(f.ctx)).toEqual([event(0)]);
    expect(await f.ctx.sessionReplication.ensure(header.id)).toBe(true);
  } finally {
    await f.close();
  }
});

test("a known transcript read failure does not fall back to an empty directory", async () => {
  let lists = 0;
  const client = {
    async listTranscripts() {
      lists++;
      return [];
    },
    async readTranscript() {
      throw new DsrError(404, "session_not_found");
    },
  } as unknown as DsrClient;
  const store = createRemoteTranscriptStore(() => client);
  await expect(
    (async () => {
      for await (const batch of store.read(header.id, header.id, undefined, {
        nextOffset: "1",
      }))
        void batch;
    })(),
  ).rejects.toMatchObject({ status: 404, code: "session_not_found" });
  expect(lists).toBe(0);
});

test.each([false, true])(
  "an explicit directory snapshot %s skips directory enumeration",
  async (exists) => {
    let reads = 0;
    const client = {
      async listTranscripts() {
        throw new Error("unexpected directory scan");
      },
      async readTranscript() {
        reads++;
        return { records: [header], next_from_offset: null, next_offset: "1" };
      },
    } as unknown as DsrClient;
    const records = [];
    for await (const batch of createRemoteTranscriptStore(() => client).read(
      header.id,
      header.id,
      undefined,
      exists ? { nextOffset: "1" } : null,
    ))
      records.push(...batch);
    expect(reads).toBe(exists ? 1 : 0);
    expect(records).toEqual(exists ? [header] : []);
  },
);
