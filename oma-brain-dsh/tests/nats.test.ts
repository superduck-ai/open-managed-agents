import { expect, test } from "bun:test";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { chmod, copyFile, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { jetstream, jetstreamManager, StorageType } from "@nats-io/jetstream";
import { connectNats } from "../src/transport.ts";
import { Backend, eventually, toolEvents, textEvents } from "./backend.ts";

const natsUrl = process.env.NATS_TEST_URL;

test.skipIf(!natsUrl)("startup explains a missing backend-owned task stream", async () => {
  const nc = await connectNats(natsUrl!);
  const manager = await jetstreamManager(nc);
  let child: Bun.Subprocess<"ignore", "pipe", "pipe"> | undefined;
  try {
    await expect(manager.streams.info("OMA_BRAIN_TASKS")).rejects.toMatchObject({ code: 10059 });
    child = Bun.spawn([process.execPath, process.env.BRAIN_TEST_ENTRY ?? "src/main.ts"], {
      env: { ...process.env, OMA_API_URL: "http://127.0.0.1:1", NATS_URL: natsUrl },
      stdin: "ignore", stdout: "pipe", stderr: "pipe",
    });
    const [code, stdout, stderr] = await Promise.all([
      child.exited,
      new Response(child.stdout).text(),
      new Response(child.stderr).text(),
    ]);
    expect(code).toBe(1);
    expect(stdout).not.toContain("oma-brain-dsh ready");
    expect(stderr).toContain("NATS stream OMA_BRAIN_TASKS is missing (10059)");
    expect(stderr).toContain("Start the Brain-enabled OMA backend");
    // Startup must not silently create a stream with different retention settings.
    await expect(manager.streams.info("OMA_BRAIN_TASKS")).rejects.toMatchObject({ code: 10059 });
  } finally {
    child?.kill();
    await child?.exited;
    await nc.close();
  }
});

test.skipIf(!natsUrl)(
  "NATS assignments register the Bun host and SSE drives user input and approvals",
  async () => {
    const backend = new Backend();
    await backend.ready;
    backend.modelScript = [toolEvents(), textEvents("NATS completed")];
    const nc = await connectNats(natsUrl!);
    const manager = await jetstreamManager(nc);
    try {
      await manager.streams.info("OMA_BRAIN_TASKS");
      await nc.close();
      backend.stop();
      throw new Error(
        "NATS_TEST_URL must point to an isolated server without OMA_BRAIN_TASKS",
      );
    } catch (error) {
      if ((error as { code?: number }).code !== 10059) throw error;
    }
    await manager.streams.add({
      name: "OMA_BRAIN_TASKS",
      subjects: ["oma.brain.tasks.v1"],
      storage: StorageType.Memory,
    });
    const js = jetstream(nc);
    const binary = process.env.BRAIN_TEST_BINARY;
    const runtimeDirectory = binary
      ? await mkdtemp(join(tmpdir(), "oma-brain-dsh-binary-"))
      : undefined;
    const standalone =
      runtimeDirectory && binary
        ? join(runtimeDirectory, basename(binary))
        : undefined;
    if (standalone) {
      await copyFile(binary!, standalone);
      await chmod(standalone, 0o755);
      // Exercise runtime .env loading without source files, node_modules or Bun on PATH.
      await writeFile(
        join(runtimeDirectory!, ".env"),
        `OMA_API_URL=${backend.url}\nNATS_URL=${natsUrl}\n`,
      );
    }
    const child = spawn(
      standalone ?? process.execPath,
      standalone ? [] : [process.env.BRAIN_TEST_ENTRY ?? "src/main.ts"],
      {
        cwd: runtimeDirectory,
        env: standalone
          ? { PATH: runtimeDirectory, SYSTEMROOT: process.env.SYSTEMROOT }
          : {
              ...process.env,
              OMA_API_URL: backend.url,
              NATS_URL: natsUrl,
            },
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    const exited = once(child, "exit");
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => {
      stdout += String(chunk);
    });
    child.stderr.on("data", (chunk) => {
      stderr += String(chunk);
    });
    backend.onApproval = async (p) => {
      backend.deliver(`reply_${crypto.randomUUID()}`, {
        type: "control_response",
        response: {
          subtype: "success",
          request_id: p.request_id,
          response: {
            behavior: "allow",
            updatedInput: { command: "nats-approved" },
          },
        },
      });
    };
    try {
      await eventually(() => stdout.includes("oma-brain-dsh ready"));
      await js.publish("oma.brain.tasks.v1", JSON.stringify({
        session_id: backend.a.session_id, worker_epoch: backend.a.worker_epoch, ingress_token: backend.a.ingress_token, model_token: backend.a.model_token,
      }));
      await eventually(() => backend.connections === 1);
      expect(backend.registrations).toBe(1);
      const initializeId = `initialize_${crypto.randomUUID()}`;
      backend.deliver(initializeId, {
        type: "control_request",
        request_id: initializeId,
        request: { subtype: "initialize" },
      });
      await eventually(() => backend.processed.includes(initializeId));
      expect(backend.requests).toHaveLength(0);
      const eventId = `task_${crypto.randomUUID()}`;
      backend.deliver(eventId, {
        type: "user",
        message: { content: "run through NATS" },
      });
      await eventually(() =>
        [...backend.publicLog.values()].some(
          (p) =>
            p.type === "result" &&
            p.is_error === false,
        ),
      );
      expect(backend.executions).toEqual([
        { name: "bash", arguments: { command: "nats-approved" } },
      ]);
      expect(backend.processed).toContain(eventId);
      await nc.flush();
      const task = await manager.consumers.info("OMA_BRAIN_TASKS", "oma_brain");
      expect(task.num_ack_pending).toBe(0);
      expect(backend.registrations).toBe(1);
      expect(
        [...backend.publicLog.values()].filter(
          (p) => p.type === "result",
        ),
      ).toHaveLength(1);
      child.kill("SIGTERM");
      const [code] = await exited;
      expect(code).toBe(0);
      expect(stderr).toBe("");
      await eventually(() => backend.streams.size === 0);
    } finally {
      if (child.exitCode === null && child.signalCode === null)
        child.kill("SIGKILL");
      await exited;
      await manager.streams.delete("OMA_BRAIN_TASKS");
      await nc.close();
      backend.stop();
      if (runtimeDirectory)
        await rm(runtimeDirectory, { recursive: true, force: true });
    }
  },
);
