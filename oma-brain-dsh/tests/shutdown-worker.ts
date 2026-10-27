import { mock } from "bun:test";

const keepAlive = setInterval(() => {}, 1_000);
mock.module("@deepseek-ai/cordis", () => ({
  Context: class {
    async plugin() {}
    async parallel() {}
    fiber = {
      async dispose() {
        console.log("disposing");
        if (process.env.TEST_SHUTDOWN_MODE === "hang") {
          await new Promise(() => {});
        }
        clearInterval(keepAlive);
        if (process.env.TEST_SHUTDOWN_MODE === "fail") throw new Error("dispose failed");
      },
    };
  },
}));
mock.module("../src/plugin.ts", () => ({}));
await import("../src/main.ts");
console.log("ready");
