import { Context } from "@deepseek-ai/cordis";
import * as brain from "./plugin.ts";
import { ConfigurationError, DEFAULT_SHUTDOWN_DRAIN_MS, SHUTDOWN_SETTLE_MS, readShutdownDrainMs } from "./config.ts";

const SHUTDOWN_CLEANUP_MS = 3_000;

const ctx = new Context();
let closing = false;
let shutdownDrainMs = DEFAULT_SHUTDOWN_DRAIN_MS;
for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.on(signal, async () => {
    if (closing) {
      if (signal === "SIGINT") process.exit(130);
      return;
    }
    closing = true;
    const limitMs = shutdownDrainMs + SHUTDOWN_SETTLE_MS + SHUTDOWN_CLEANUP_MS;
    const timeout = setTimeout(() => {
      console.error(`Brain shutdown timed out after ${limitMs / 1000} seconds; forcing exit`);
      process.exit(1);
    }, limitMs);
    timeout.unref();
    try {
      await ctx.parallel("oma/shutdown-drain").catch(() => {
        console.error("Brain shutdown drain failed");
      });
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
  shutdownDrainMs = readShutdownDrainMs();
  await ctx.plugin(brain);
} catch (error) {
  console.error(error instanceof ConfigurationError ? error.message : "Brain startup failed");
  await ctx.fiber.dispose();
  process.exitCode = 1;
}
