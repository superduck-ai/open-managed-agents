import { Context } from "@deepseek-ai/cordis";
import * as brain from "./plugin.ts";
import { ConfigurationError } from "./config.ts";
import { emitBrainLog, startTelemetry, stopTelemetry } from "./telemetry.ts";

startTelemetry();

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
    emitBrainLog("info", "oma_brain_shutdown_started", { signal });
    try {
      await ctx.fiber.dispose();
      process.exitCode = 0;
    } catch {
      console.error("Brain shutdown failed");
      emitBrainLog("error", "oma_brain_shutdown_failed", { signal });
      process.exitCode = 1;
    } finally {
      clearTimeout(timeout);
      await stopTelemetry();
    }
  });
}
try {
  await ctx.plugin(brain);
} catch (error) {
  console.error(error instanceof ConfigurationError ? error.message : "Brain startup failed");
  emitBrainLog("error", "oma_brain_startup_failed", {
    failure_reason: error instanceof ConfigurationError ? "configuration_error" : "startup_error",
  });
  await ctx.fiber.dispose();
  await stopTelemetry();
  process.exitCode = 1;
}
