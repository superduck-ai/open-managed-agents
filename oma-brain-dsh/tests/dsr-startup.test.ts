import { expect,test } from "bun:test";
import { mkdtemp,rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { eventually } from "./backend.ts";

test("DSR startup emits the readiness marker consumed by just restart", async () => {
  const root = await mkdtemp(join(tmpdir(), "dsr-startup-"));
  let claims = 0;
  const server = Bun.serve({ port: 0, fetch: request => {
    const url = new URL(request.url);
    if (url.pathname === "/v1/dsr/claims") {
      claims++;
      return new Response(null, { status: 204 });
    }
    return new Response(null, { status: 404 });
  } });
  const child = Bun.spawn([process.execPath, "src/main.ts"], {
    env: { ...process.env, OMA_BRAIN_PROTOCOL: "dsr1", OMA_API_URL: server.url.origin,
      OMA_BRAIN_CLAIM_TOKEN: "test-claim", DSH_HOME: root, OMA_BRAIN_TRANSCRIPT_DIR: join(root, "transcripts") },
    stdin: "ignore", stdout: "pipe", stderr: "pipe",
  });
  let stdout = "";
  const reading = (async () => { for await (const chunk of child.stdout) stdout += new TextDecoder().decode(chunk); })();
  const stderr = new Response(child.stderr).text();
  try {
    await eventually(() => stdout.includes("oma-brain-dsh ready"));
    expect(claims).toBeGreaterThan(0);
  } finally {
    child.kill("SIGTERM");
    await child.exited;
    await reading;
    await stderr;
    server.stop(true);
    await rm(root, { recursive: true, force: true });
  }
});
