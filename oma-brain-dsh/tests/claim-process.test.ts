import { expect,test } from "bun:test";
import {
chmod,
copyFile,
mkdir,
mkdtemp,
rm,
writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename,join } from "node:path";
import { eventually,textEvents,toolEvents } from "./backend.ts";
import { DsrBackend } from "./dsr-backend.ts";

test("startup rejects a legacy protocol before contacting the backend", async () => {
  const child = Bun.spawn(
    [process.execPath, process.env.BRAIN_TEST_ENTRY ?? "src/main.ts"],
    {
      env: {
        ...process.env,
        OMA_BRAIN_PROTOCOL: "ccrv2",
        OMA_API_URL: "http://127.0.0.1:1",
        OMA_BRAIN_CLAIM_TOKEN: "private-token",
      },
      stdout: "pipe",
      stderr: "pipe",
    },
  );
  const [code, stderr] = await Promise.all([
    child.exited,
    new Response(child.stderr).text(),
  ]);
  expect(code).toBe(1);
  expect(stderr).toContain("only supports dsr1");
  expect(stderr).not.toContain("private-token");
});

test("DSR startup reports rejected claim credentials without exposing them", async () => {
  const backend = new DsrBackend();
  await backend.ready;
  const root = await mkdtemp(join(tmpdir(), "dsr-rejected-"));
  try {
    const child = Bun.spawn(
      [process.execPath, process.env.BRAIN_TEST_ENTRY ?? "src/main.ts"],
      {
        env: {
          ...process.env,
          OMA_BRAIN_PROTOCOL: "dsr1",
          OMA_API_URL: backend.url,
          OMA_BRAIN_CLAIM_TOKEN: "wrong-private-token",
          DSH_HOME: root,
        },
        stdout: "pipe",
        stderr: "pipe",
      },
    );
    const [code, stderr] = await Promise.all([
      child.exited,
      new Response(child.stderr).text(),
    ]);
    expect(code).toBe(1);
    expect(stderr).toContain("Brain claim rejected with HTTP 401");
    expect(stderr).not.toContain("wrong-private-token");
  } finally {
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});

test("DSR process claims, executes a task, persists it and settles delivery", async () => {
  const backend = new DsrBackend();
  backend.modelScript = [toolEvents(), textEvents("completed")];
  await backend.ready;
  const root = await mkdtemp(join(tmpdir(), "dsr-process-"));
  const binary = process.env.BRAIN_TEST_BINARY;
  const standalone = binary ? join(root, "bin", basename(binary)) : undefined;
  if (standalone) {
    await mkdir(join(root, "bin"));
    await copyFile(binary!, standalone);
    await chmod(standalone, 0o755);
    await writeFile(
      join(root, ".env"),
      `OMA_API_URL=${backend.url}\nOMA_BRAIN_CLAIM_TOKEN=${backend.claimToken}\nDSH_HOME=${root}\n`,
    );
  }
  const child = Bun.spawn(
    standalone
      ? [standalone]
      : [process.execPath, process.env.BRAIN_TEST_ENTRY ?? "src/main.ts"],
    {
      cwd: standalone ? root : undefined,
      env: standalone
        ? { PATH: root, SYSTEMROOT: process.env.SYSTEMROOT }
        : {
            ...process.env,
            OMA_BRAIN_PROTOCOL: "dsr1",
            OMA_API_URL: backend.url,
            OMA_BRAIN_CLAIM_TOKEN: backend.claimToken,
            DSH_HOME: root,
          },
      stdout: "pipe",
      stderr: "pipe",
    },
  );
  let stdout = "";
  const output = (async () => {
    for await (const chunk of child.stdout)
      stdout += new TextDecoder().decode(chunk);
  })();
  const errors = new Response(child.stderr).text();
  try {
    backend.queueClaim();
    await eventually(() => backend.connections > 0);
    backend.deliver("process-input", {
      lane: "task",
      kind: "user.message",
      content: [{ type: "text", text: "hello" }],
    });
    await eventually(() => backend.processed.includes("process-input"));
    expect(backend.finishes.get("process-input")).toEqual({
      result: "success",
    });
    expect(backend.requests).toHaveLength(2);
    expect(backend.executions).toHaveLength(1);
    expect(backend.permissions.size).toBe(1);
    expect(backend.transcripts.size).toBe(1);
    expect(stdout).toContain("oma-brain-dsh ready");
  } finally {
    child.kill("SIGTERM");
    await child.exited;
    await output;
    await errors;
    backend.stop();
    await rm(root, { recursive: true, force: true });
  }
});
