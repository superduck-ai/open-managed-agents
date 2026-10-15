import { expect, test } from "bun:test";
import { jetstream, jetstreamManager, StorageType } from "@nats-io/jetstream";
import { HttpError } from "../src/api.ts";
import { connectNats, consumeTasks } from "../src/transport.ts";

const url = process.env.NATS_TASK_TEST_URL;

test.skipIf(!url)("invalid and permanently rejected tasks do not block following admissions", async () => {
  const nc = await connectNats(url!);
  const manager = await jetstreamManager(nc);
  const abort = new AbortController();
  let consuming: Promise<void> | undefined;
  try {
    await manager.streams.add({ name: "OMA_BRAIN_TASKS", subjects: ["oma.brain.tasks.v1"], storage: StorageType.Memory });
    const ready = Promise.withResolvers<void>();
    const admitted = Promise.withResolvers<void>();
    const attempts: string[] = [];
    consuming = consumeTasks(nc, abort.signal, async (task) => {
      attempts.push(String(task));
      if (task === "missing") throw new HttpError(404);
      admitted.resolve();
    }, () => ready.resolve());
    await ready.promise;
    const js = jetstream(nc);
    for (const task of ["invalid-json", JSON.stringify("missing"), JSON.stringify("healthy")]) {
      await js.publish("oma.brain.tasks.v1", new TextEncoder().encode(task));
    }
    await Promise.race([admitted.promise, Bun.sleep(3000).then(() => { throw new Error("healthy task starved"); })]);
    abort.abort();
    await consuming;
    await nc.flush();
    const info = await manager.consumers.info("OMA_BRAIN_TASKS", "oma_brain");
    expect(attempts).toEqual(["missing", "healthy"]);
    expect(info.num_ack_pending).toBe(0);
    expect(info.num_pending).toBe(0);
  } finally {
    abort.abort();
    await consuming;
    await nc.close();
  }
});
