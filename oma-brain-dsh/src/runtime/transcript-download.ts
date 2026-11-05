import { Context } from "@deepseek-ai/cordis";
import * as JsonlPersistence from "@deepseek-ai/dsh-session-persistence-jsonl";
import { createReadStream } from "node:fs";
import { mkdtemp, open, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import {
  type SessionEvent,
  type SessionHeader,
  type SessionId,
} from "@deepseek-ai/dsh-session";
import {
  assertContiguous,
  assertStoredId,
  assertVersion,
  materializeCreateHeader,
  validateStoredEvents,
} from "@deepseek-ai/dsh-session-persistence";
import { sameJson } from "./json-equality.ts";
import type { DshLogData } from "./transcript-types.ts";

export interface DownloadedTranscript {
  header: SessionHeader;
  count: number;
  batches(): AsyncGenerator<SessionEvent[]>;
}

async function* eventBatches(path: string): AsyncGenerator<SessionEvent[]> {
  const input = createReadStream(path, { encoding: "utf8" });
  const lines = createInterface({ input, crlfDelay: Infinity });
  try {
    let batch: SessionEvent[] = [];
    for await (const line of lines) {
      batch.push(JSON.parse(line) as SessionEvent);
      if (batch.length === 200) {
        yield batch;
        batch = [];
      }
    }
    if (batch.length) yield batch;
  } finally {
    lines.close();
    input.destroy();
  }
}

async function validateTranscript(
  root: string,
  path: string,
  header: SessionHeader,
): Promise<SessionHeader> {
  const ctx = new Context();
  try {
    await ctx.plugin(JsonlPersistence, {
      root: join(root, "validation"),
      compression: "none",
    });
    const writer = await ctx.sessionPersistence.create(header);
    try {
      for await (const batch of eventBatches(path)) await writer.append(batch);
      await writer.flush();
    } finally {
      await writer.close();
    }
    const reader = await ctx.sessionPersistence.open(header.id, "read");
    try {
      await reader.read();
      return reader.header;
    } finally {
      await reader.close();
    }
  } finally {
    await ctx.fiber.dispose();
  }
}

async function storedEvent(
  path: string,
  seq: number,
): Promise<SessionEvent | undefined> {
  for await (const batch of eventBatches(path)) {
    const event = batch.find((item) => item.seq === seq);
    if (event) return event;
  }
  return undefined;
}

export async function withDownloadedTranscript<T>(
  id: SessionId,
  source: AsyncIterable<readonly DshLogData[]>,
  assertCurrent: () => void,
  consume: (transcript: DownloadedTranscript | undefined) => Promise<T>,
): Promise<T> {
  const root = await mkdtemp(join(tmpdir(), "oma-brain-transcript-"));
  const path = join(root, "events.jsonl");
  try {
    const output = await open(path, "wx", 0o600);
    let header: SessionHeader | undefined;
    let count = 0;
    try {
      for await (const batch of source) {
        assertCurrent();
        const pending: SessionEvent[] = [];
        for (const data of batch) {
          if (!data || typeof data !== "object" || Array.isArray(data))
            throw new Error("Invalid dsh log record");
          if (!("seq" in data)) {
            assertStoredId(id, data);
            assertVersion(data);
            if (data.isSeeded)
              throw new Error("Forked sessions are not enabled");
            const candidate = materializeCreateHeader(data);
            if (header && !sameJson(header, candidate))
              throw new Error("Log header changed");
            header ??= candidate;
          } else {
            if (!header) throw new Error("Missing dsh log header");
            const [event] = validateStoredEvents(header, [
              structuredClone(data),
            ]);
            if (event.seq < count) {
              const previous =
                pending.find((item) => item.seq === event.seq) ??
                (await storedEvent(path, event.seq));
              if (!sameJson(previous, event))
                throw new Error("Conflicting dsh log event");
            } else {
              assertContiguous(id, [event], count);
              pending.push(event);
              count++;
            }
          }
        }
        if (pending.length)
          await output.writeFile(
            pending.map((event) => JSON.stringify(event)).join("\n") + "\n",
          );
        assertCurrent();
      }
    } finally {
      await output.close();
    }
    if (header) header = await validateTranscript(root, path, header);
    assertCurrent();
    return await consume(
      header ? { header, count, batches: () => eventBatches(path) } : undefined,
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}
