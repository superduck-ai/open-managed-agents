import { resolve } from "node:path";

const name = `oma-brain-dsh-test-${crypto.randomUUID()}`;
const image = process.env.NATS_TEST_IMAGE ?? "nats:2.14.6-alpine";
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

let started = false;
try {
  await command([
    process.execPath,
    "run",
    executable ? "build:executable" : "build",
  ]);
  await command([
    "docker",
    "run",
    "--detach",
    "--rm",
    "--name",
    name,
    "--publish",
    "127.0.0.1::4222",
    image,
    "--jetstream",
  ]);
  started = true;
  const address = await command(["docker", "port", name, "4222/tcp"]);
  if (!/^127\.0\.0\.1:\d+$/.test(address))
    throw new Error("Unexpected test NATS address");
  const child = Bun.spawn([process.execPath, "test", "--timeout", "15000"], {
    env: {
      ...process.env,
      NATS_TEST_URL: `nats://${address}`,
      BRAIN_TEST_ENTRY: "dist/main.js",
      BRAIN_TEST_BINARY: executable
        ? resolve(
            "dist",
            process.platform === "win32"
              ? "oma-brain-dsh.exe"
              : "oma-brain-dsh",
          )
        : "",
    },
    stdin: "inherit",
    stdout: "inherit",
    stderr: "inherit",
  });
  process.exitCode = await child.exited;
} finally {
  if (started) await command(["docker", "stop", "--time", "1", name]);
}
