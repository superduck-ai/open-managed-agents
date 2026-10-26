import { mkdir, open, rename, rm } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { ConfigurationError } from "./config.ts";

declare const OMA_BRAIN_STANDALONE: boolean;

function configuredPath(value: string): string {
  if (value === "~") return homedir();
  if (value.startsWith("~/") || value.startsWith("~\\")) return resolve(homedir(), value.slice(2));
  return resolve(value);
}

export function resolveBrainHome(env: Record<string, string | undefined> = process.env): string {
  const explicit = env.DSH_HOME?.trim();
  if (explicit) return configuredPath(explicit);
  const application = typeof OMA_BRAIN_STANDALONE !== "undefined" && OMA_BRAIN_STANDALONE
    ? dirname(process.execPath)
    : resolve(dirname(fileURLToPath(import.meta.url)), "..");
  return join(application, ".dsh");
}

export function resolveTranscriptRoot(env: Record<string, string | undefined> = process.env): string {
  const explicit = env.OMA_BRAIN_TRANSCRIPT_DIR?.trim();
  return explicit ? configuredPath(explicit) : join(resolveBrainHome(env), "transcripts");
}

export async function checkWritableDirectory(directory: string, setting: string): Promise<void> {
  try {
    await mkdir(directory, { recursive: true, mode: 0o700 });
    const source = join(directory, `.oma-write-check-${crypto.randomUUID()}`);
    const destination = `${source}.renamed`;
    try {
      const handle = await open(source, "wx", 0o600);
      try {
        await handle.writeFile("storage check\n");
        await handle.sync();
      } finally { await handle.close(); }
      await rename(source, destination);
    } finally {
      await Promise.all([rm(source, { force: true }), rm(destination, { force: true })]);
    }
  } catch (error) {
    const code = error && typeof error === "object" && "code" in error ? String(error.code) : "I/O failure";
    throw new ConfigurationError(`${setting} storage directory is not writable: ${directory} (${code})`, { cause: error });
  }
}
