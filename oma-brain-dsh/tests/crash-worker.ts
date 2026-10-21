import { Context } from "@deepseek-ai/cordis";
import { createInterface } from "node:readline";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import type { Assignment } from "../src/assignment.ts";

const ctx = new Context();
const api = new OmaApi(process.env.TEST_API_URL!);
await mountOma(ctx, api);
const session = createOmaSession(
  ctx,
  api,
  JSON.parse(process.env.TEST_ASSIGNMENT!) as Assignment,
);
await session.open();
const lines = createInterface({ input: process.stdin });
const task = session.receive("crash_input", {
  message: { content: "execute a command" },
});
for await (const line of lines) await session.control(JSON.parse(line));
await task;
await session.close();
await ctx.fiber.dispose();
