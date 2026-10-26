import { expect, test } from "bun:test";
import { mkdtemp, realpath, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

for (const explicit of [false, true]) {
  test(`kernel uses ${explicit ? "explicit DSH_HOME" : "the application directory"} from a different working directory`, async () => {
    const directory = await realpath(await mkdtemp(join(tmpdir(), "oma-dsh-home-")));
    const home = explicit ? join(directory, "custom") : fileURLToPath(new URL("../.dsh", import.meta.url));
    try {
      const source = `
        const { Context } = await import(${JSON.stringify(import.meta.resolve("@deepseek-ai/cordis"))});
        const { mountKernel } = await import(${JSON.stringify(new URL("../src/kernel.ts", import.meta.url).href)});
        const ctx = new Context();
        await mountKernel(ctx);
        console.log(JSON.stringify({ home: process.env.DSH_HOME, attachments: ctx.attachments.root }));
        await ctx.fiber.dispose();
      `;
      const child = Bun.spawn([process.execPath, "--eval", source], {
        cwd: directory, env: { ...process.env, DSH_HOME: explicit ? home : "" },
        stdout: "pipe", stderr: "pipe",
      });
      const [stdout, stderr, code] = await Promise.all([
        new Response(child.stdout).text(), new Response(child.stderr).text(), child.exited,
      ]);
      expect(stderr).toBe("");
      expect(code).toBe(0);
      expect(JSON.parse(stdout)).toEqual({ home, attachments: join(home, "attachments", "v1") });
    } finally { await rm(directory, { recursive: true, force: true }); }
  });
}
