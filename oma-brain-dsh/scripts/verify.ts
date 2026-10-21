import { resolve } from "node:path";

const executable = process.argv.includes("--executable");

async function command(args: string[]): Promise<string> {
  const child = Bun.spawn(args, { stdout: "pipe", stderr: "pipe" });
  const [stdout, stderr, code] = await Promise.all([
    new Response(child.stdout).text(),
    new Response(child.stderr).text(),
    child.exited,
  ]);
  if (code !== 0) throw new Error(`${args[0]} failed: ${stderr.trim()}`);
  return stdout.trim();
}

await command([process.execPath, "run", "build"]);
if (executable) await command([process.execPath, "run", "build:executable"]);
const child = Bun.spawn([process.execPath, "test", "--timeout", "15000"], {
  env: {
    ...process.env,
    BRAIN_TEST_ENTRY: "dist/main.js",
    BRAIN_TEST_BINARY: executable
      ? resolve("dist", process.platform === "win32" ? "oma-brain-dsh.exe" : "oma-brain-dsh")
      : "",
  },
  stdin: "inherit",
  stdout: "inherit",
  stderr: "inherit",
});
process.exitCode = await child.exited;
