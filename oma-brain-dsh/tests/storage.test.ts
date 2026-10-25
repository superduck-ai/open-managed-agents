import { afterEach, expect, test } from "bun:test";
import { chmod, mkdir, mkdtemp, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { checkWritableDirectory, resolveBrainHome, resolveTranscriptRoot } from "../src/storage.ts";
import { Backend } from "./backend.ts";

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });
async function temporary() {
  const root = await mkdtemp(join(tmpdir(), "oma-storage-"));
  cleanups.push(() => rm(root, { recursive: true, force: true }));
  return root;
}

test("non-directory storage fails with the setting and resolved path", async () => {
  const path = join(await temporary(), "file");
  await writeFile(path, "existing data");
  await expect(checkWritableDirectory(path, "DSH_HOME")).rejects.toThrow(`DSH_HOME storage directory is not writable: ${path}`);
});

test.skipIf(process.platform === "win32" || process.getuid?.() === 0)("read-only storage is refused by an actual write probe", async () => {
  const path = await temporary();
  await chmod(path, 0o500);
  try {
    await expect(checkWritableDirectory(path, "OMA_BRAIN_TRANSCRIPT_DIR")).rejects.toThrow("OMA_BRAIN_TRANSCRIPT_DIR storage directory is not writable");
  } finally { await chmod(path, 0o700); }
});

for (const [setting, readOnly] of [
  ["DSH_HOME", false], ["OMA_BRAIN_TRANSCRIPT_DIR", false],
  ["DSH_HOME", true], ["OMA_BRAIN_TRANSCRIPT_DIR", true],
] as const) {
  test.skipIf(readOnly && (process.platform === "win32" || process.getuid?.() === 0))(`startup refuses ${readOnly ? "read-only" : "invalid"} ${setting} before making any claims`, async () => {
    const root = await temporary();
    const blocked = join(root, "blocked");
    if (readOnly) {
      await mkdir(join(blocked, "attachments", "v1"), { recursive: true });
      await chmod(blocked, 0o500);
      cleanups.push(() => chmod(blocked, 0o700));
    } else await writeFile(blocked, "existing data");
    const backend = new Backend();
    cleanups.push(async () => { backend.stop(); });
    await backend.ready;
    const binary = process.env.BRAIN_TEST_BINARY;
    const entry = resolve(process.env.BRAIN_TEST_ENTRY ?? fileURLToPath(new URL("../src/main.ts", import.meta.url)));
    const child = Bun.spawn(binary ? [binary] : [process.execPath, entry], {
      cwd: root,
      env: { ...process.env, OMA_API_URL: backend.url, OMA_BRAIN_CLAIM_TOKEN: backend.claimToken,
        DSH_HOME: join(root, "home"), OMA_BRAIN_TRANSCRIPT_DIR: join(root, "transcripts"), [setting]: blocked },
      stdout: "pipe", stderr: "pipe",
    });
    const timeout = setTimeout(() => child.kill("SIGKILL"), 5000);
    const [code, stderr] = await Promise.all([child.exited, new Response(child.stderr).text()]).finally(() => clearTimeout(timeout));
    expect(code).toBe(1);
    expect(stderr).toContain(`${setting} storage directory is not writable`);
    expect(stderr).toContain(blocked);
    expect(backend.claimRequests).toEqual([]);
    expect(backend.registrations).toBe(0);
  });
}

test("source defaults stay below brain-dsh independently of cwd and explicit home controls transcript defaults", () => {
  const expected = fileURLToPath(new URL("../.dsh", import.meta.url));
  expect(resolveBrainHome({})).toBe(expected);
  expect(resolveBrainHome({ DSH_HOME: "  " })).toBe(expected);
  expect(resolveTranscriptRoot({})).toBe(join(expected, "transcripts"));
  const home = join(tmpdir(), "explicit-home");
  expect(resolveBrainHome({ DSH_HOME: home })).toBe(home);
  expect(resolveTranscriptRoot({ DSH_HOME: home })).toBe(join(home, "transcripts"));
  expect(resolveTranscriptRoot({ DSH_HOME: home, OMA_BRAIN_TRANSCRIPT_DIR: join(tmpdir(), "explicit-transcripts") }))
    .toBe(join(tmpdir(), "explicit-transcripts"));
});

test("writable nested storage is prepared and no probe files remain", async () => {
  const path = join(await temporary(), "nested", "transcripts");
  await checkWritableDirectory(path, "OMA_BRAIN_TRANSCRIPT_DIR");
  expect(await readdir(path)).toEqual([]);
});
