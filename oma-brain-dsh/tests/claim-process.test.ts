import { expect, test } from "bun:test";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { chmod, copyFile, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { Backend, eventually, toolEvents, textEvents } from "./backend.ts";

test("startup explains a rejected brain claim token", async () => {
  const backend = new Backend();
  await backend.ready;
  try {
    const child = Bun.spawn([process.execPath, process.env.BRAIN_TEST_ENTRY ?? "src/main.ts"], {
      env: { ...process.env, OMA_API_URL: backend.url, OMA_BRAIN_CLAIM_TOKEN: "wrong-claim-token" },
      stdin: "ignore", stdout: "pipe", stderr: "pipe",
    });
    const [code, stdout, stderr] = await Promise.all([
      child.exited,
      new Response(child.stdout).text(),
      new Response(child.stderr).text(),
    ]);
    expect(code).toBe(1);
    expect(stdout).not.toContain("oma-brain-dsh ready");
    expect(stderr).toContain("Brain claim rejected with HTTP 401");
    expect(stderr).not.toContain("wrong-claim-token");
  } finally {
    backend.stop();
  }
});

test("startup requires a brain claim token", async () => {
  const child = Bun.spawn([process.execPath, process.env.BRAIN_TEST_ENTRY ?? "src/main.ts"], {
    env: { ...process.env, OMA_API_URL: "http://127.0.0.1:1", OMA_BRAIN_CLAIM_TOKEN: "" },
    stdin: "ignore", stdout: "pipe", stderr: "pipe",
  });
  const [code, stderr] = await Promise.all([child.exited, new Response(child.stderr).text()]);
  expect(code).toBe(1);
  expect(stderr).toContain("OMA_BRAIN_CLAIM_TOKEN is required");
});

test("claimed sessions register the Bun host and SSE drives user input and approvals", async () => {
  const backend = new Backend();
  await backend.ready;
  backend.modelScript = [toolEvents(), textEvents("claim completed")];
  const binary = process.env.BRAIN_TEST_BINARY;
  const runtimeDirectory = binary ? await mkdtemp(join(tmpdir(), "oma-brain-dsh-binary-")) : undefined;
  const standalone = runtimeDirectory && binary ? join(runtimeDirectory, basename(binary)) : undefined;
  if (standalone) {
    await copyFile(binary!, standalone);
    await chmod(standalone, 0o755);
    // Exercise runtime .env loading without source files, node_modules or Bun on PATH.
    await writeFile(
      join(runtimeDirectory!, ".env"),
      `OMA_API_URL=${backend.url}\nOMA_BRAIN_CLAIM_TOKEN=${backend.claimToken}\n`,
    );
  }
  const child = spawn(
    standalone ?? process.execPath,
    standalone ? [] : [process.env.BRAIN_TEST_ENTRY ?? "src/main.ts"],
    {
      cwd: runtimeDirectory,
      env: standalone
        ? { PATH: runtimeDirectory, SYSTEMROOT: process.env.SYSTEMROOT }
        : { ...process.env, OMA_API_URL: backend.url, OMA_BRAIN_CLAIM_TOKEN: backend.claimToken },
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  const exited = once(child, "exit");
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => { stdout += String(chunk); });
  child.stderr.on("data", (chunk) => { stderr += String(chunk); });
  backend.onApproval = async (p) => {
    backend.deliver(`reply_${crypto.randomUUID()}`, {
      type: "control_response",
      response: {
        subtype: "success",
        request_id: p.request_id,
        response: { behavior: "allow", updatedInput: { command: "claim-approved" } },
      },
    });
  };
  try {
    await eventually(() => stdout.includes("oma-brain-dsh ready"));
    expect(backend.claimRequests[0]!.blockMs).toBe(0);
    backend.queueClaim();
    await eventually(() => backend.connections === 1);
    expect(backend.registrations).toBe(1);
    const workerIds = new Set(backend.claimRequests.map((request) => request.workerId));
    expect(workerIds.size).toBe(1);
    expect(backend.claimRequests.some((request) => request.blockMs === 25_000)).toBe(true);
    expect(backend.claimRequests[0]!.body).toEqual({
      max_sessions: 8, busy_sessions: 0, resident_sessions: 0, max_tasks: 4, admission_failures: 0,
    });
    expect(backend.acks).toEqual([{
      session_id: backend.a.session_id, worker_id: [...workerIds][0], worker_epoch: backend.a.worker_epoch,
    }]);
    const initializeId = `initialize_${crypto.randomUUID()}`;
    backend.deliver(initializeId, {
      type: "control_request",
      request_id: initializeId,
      request: { subtype: "initialize" },
    });
    await eventually(() => backend.processed.includes(initializeId));
    expect(backend.requests).toHaveLength(0);
    const eventId = `task_${crypto.randomUUID()}`;
    backend.deliver(eventId, { type: "user", message: { content: "run through a claim" } });
    await eventually(() =>
      [...backend.publicLog.values()].some((p) => p.type === "result" && p.is_error === false),
    );
    expect(backend.executions).toEqual([{ name: "bash", arguments: { command: "claim-approved" } }]);
    await eventually(() => backend.processed.includes(eventId));
    expect(backend.registrations).toBe(1);
    expect([...backend.publicLog.values()].filter((p) => p.type === "result")).toHaveLength(1);
    expect(backend.releases).toEqual([]);
    child.kill("SIGTERM");
    const [code] = await exited;
    expect(code).toBe(0);
    expect(stderr).toBe("");
    expect(backend.releases).toEqual([{
      session_id: backend.a.session_id, worker_id: [...workerIds][0],
      worker_epoch: backend.a.worker_epoch, reason: "idle",
    }]);
    await eventually(() => backend.streams.size === 0);
  } finally {
    if (child.exitCode === null && child.signalCode === null) child.kill("SIGKILL");
    await exited;
    backend.stop();
    if (runtimeDirectory) await rm(runtimeDirectory, { recursive: true, force: true });
  }
});

test("shutdown hands busy claimed sessions back to the backend", async () => {
  const backend = new Backend();
  await backend.ready;
  const modelStarted = Promise.withResolvers<void>();
  backend.onModel = async () => {
    modelStarted.resolve();
    return await new Promise<never>(() => {});
  };
  const child = Bun.spawn([process.execPath, process.env.BRAIN_TEST_ENTRY ?? "src/main.ts"], {
    env: { ...process.env, OMA_API_URL: backend.url, OMA_BRAIN_CLAIM_TOKEN: backend.claimToken },
    stdin: "ignore", stdout: "pipe", stderr: "pipe",
  });
  try {
    backend.queueClaim();
    await eventually(() => backend.connections === 1);
    backend.deliver(`task_${crypto.randomUUID()}`, { type: "user", message: { content: "keep working" } });
    await modelStarted.promise;
    child.kill("SIGTERM");
    expect(await child.exited).toBe(0);
    expect(backend.releases).toEqual([{
      session_id: backend.a.session_id, worker_id: backend.claimRequests[0]!.workerId,
      worker_epoch: backend.a.worker_epoch, reason: "shutdown",
    }]);
  } finally {
    child.kill("SIGKILL");
    backend.stop();
  }
});
