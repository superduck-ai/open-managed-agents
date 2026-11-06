import { Context } from "@deepseek-ai/cordis";
import SessionStore, {
  SessionId,
  Session,
  appendPluginRecord,
} from "@deepseek-ai/dsh-session";
import TranscriptPersistence from "../src/runtime/transcript-persistence.ts";
import { createInterface } from "node:readline";
import { DsrClient } from "../src/dsr/client.ts";
import { createRemoteTranscriptStore } from "../src/dsr/replication.ts";
import { SessionReplication } from "../src/runtime/replication.ts";
import { TranscriptUploadError } from "../src/runtime/transcript-batch.ts";
import type { SessionHandle } from "@deepseek-ai/dsh-session-persistence";
import type { Claim } from "../src/dsr/wire.ts";

declare module "@deepseek-ai/dsh-session/types" {
  interface PluginRecordMap {
    "plugin:verification/upload": { index: number; payload: string };
  }
}

interface ProbeConfig {
  url: string;
  directory: string;
  claim: Claim;
}
interface ProbeCommand {
  id: string;
  action: "prepare" | "append" | "flush" | "capacity" | "status" | "drain";
  count?: number;
  payloadBytes?: number;
}

const input = createInterface({ input: process.stdin });
const lines = input[Symbol.asyncIterator]();
const first = await lines.next();
if (first.done) throw new Error("Missing probe configuration");
const config: ProbeConfig = JSON.parse(first.value);
const ctx = new Context();
await ctx.plugin(SessionStore);
await ctx.plugin(TranscriptPersistence);
const client = new DsrClient(
  {
    request: (options) =>
      fetch(config.url + options.path, {
        method: options.method,
        headers: {
          Authorization: `Bearer ${options.token}`,
          "Content-Type": "application/json",
          ...options.headers,
        },
        body:
          options.body === undefined ? undefined : JSON.stringify(options.body),
        signal: options.signal
          ? AbortSignal.any([options.signal, AbortSignal.timeout(15_000)])
          : AbortSignal.timeout(15_000),
      }),
  },
  config.claim,
);
await ctx.plugin(SessionReplication, {
  remote: createRemoteTranscriptStore(() => client),
});
const id = SessionId(config.claim.code_session_id);
const source = Session.create(id);
let next = 0;
let writer: SessionHandle | undefined;
let capacityDone = false;
const pending = new Set<Promise<void>>();
function emit(value: object) {
  process.stdout.write(JSON.stringify(value) + "\n");
}
async function append(command: ProbeCommand, create: boolean) {
  if (create)
    writer = await ctx.sessionPersistence.create({
      id,
      version: 4,
      createdAt: 0,
      isSeeded: false,
    });
  if (!writer) throw new Error("Transcript has not been prepared");
  for (let offset = 0; offset < (command.count ?? 1); offset++)
    appendPluginRecord(source, "plugin:verification/upload", {
      index: next + offset,
      payload: "x".repeat(command.payloadBytes ?? 0),
    });
  const records = source.snapshotEvents().slice(next);
  await writer.append(records);
  next += records.length;
}
async function localCount(): Promise<number> {
  const handle = await ctx.sessionPersistence.open(id, "read");
  try {
    return (await handle.read()).events.length;
  } finally {
    await handle.close();
  }
}
async function run(command: ProbeCommand) {
  try {
    switch (command.action) {
      case "prepare":
        await ctx.sessionReplication.ensure(id);
        await append(command, true);
        break;
      case "append":
        await append(command, false);
        break;
      case "flush":
        await ctx.sessionReplication.flush(id);
        break;
      case "capacity":
        emit({
          id: command.id,
          phase: "started",
          status: ctx.sessionReplication.status(id),
        });
        await ctx.sessionReplication.waitForCapacity(id);
        capacityDone = true;
        break;
      case "drain":
        await ctx.sessionReplication.drain(id);
        break;
      case "status":
        break;
    }
    emit({
      id: command.id,
      phase: "done",
      capacityDone,
      localCount: next,
      status: ctx.sessionReplication.status(id),
    });
  } catch (error) {
    emit({
      id: command.id,
      phase: "failed",
      permanent: error instanceof TranscriptUploadError,
      message: error instanceof Error ? error.message : "Probe failed",
      localCount: await localCount(),
      status: ctx.sessionReplication.status(id),
    });
  }
}
try {
  emit({ phase: "ready" });
  for (;;) {
    const line = await lines.next();
    if (line.done) break;
    const task = run(JSON.parse(line.value));
    pending.add(task);
    void task.finally(() => pending.delete(task));
  }
} finally {
  ctx.sessionReplication.cancel(id);
  await Promise.all(pending);
  ctx.sessionReplication.forget(id);
  await writer?.close();
  await ctx.fiber.dispose();
}
