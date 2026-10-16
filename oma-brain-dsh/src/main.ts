import { Context } from "@deepseek-ai/cordis";
import * as brain from "./plugin.ts";
import { ConfigurationError } from "./config.ts";

const ctx = new Context();
let closing = false;
for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.on(signal, async () => {
    if (closing) {
      if (signal === "SIGINT") process.exit(130);
      return;
    }
    closing = true;
    const timeout = setTimeout(() => {
      console.error("Brain shutdown timed out after 10 seconds; forcing exit");
      process.exit(1);
    }, 3_000);
    timeout.unref();
    try {
      await ctx.fiber.dispose();
      process.exitCode = 0;
    } catch {
      console.error("Brain shutdown failed");
      process.exitCode = 1;
    } finally {
      clearTimeout(timeout);
    }
  });
}
try {
  await ctx.plugin(brain);
} catch (error) {
  console.error(error instanceof ConfigurationError ? error.message : "Brain startup failed");
  await ctx.fiber.dispose();
  process.exitCode = 1;
}
