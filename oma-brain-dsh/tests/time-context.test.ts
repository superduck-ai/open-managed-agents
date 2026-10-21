import { expect, spyOn, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, modelSystemText } from "./backend.ts";

test("Brain preserves clock readings across resume and refreshes after ten minutes", async () => {
  const backend = new Backend();
  await backend.ready;
  let context = new Context();
  let session: ReturnType<typeof createOmaSession> | undefined;
  const now = Date.now.bind(Date);
  let offset = 0;
  const clock = spyOn(Date, "now").mockImplementation(() => now() + offset);
  const readings = (index: number) => JSON.stringify(backend.requests[index]!.messages)
    .match(/Time sampled while preparing turn/g) ?? [];
  try {
    let api = new OmaApi(backend.url);
    await mountOma(context, api);
    session = createOmaSession(context, api, backend.a);
    await session.open();
    await session.receive("clock_first", { content: "What is the current date?" });
    expect(readings(0)).toHaveLength(1);
    expect(JSON.stringify(backend.requests[0]!.messages)).toContain("+08:00[Asia/Shanghai]");
    expect(modelSystemText(backend.requests[0]!)).not.toContain("Time sampled while preparing turn");
    expect(backend.executions).toHaveLength(0);
    await session.close();
    await context.fiber.dispose();

    context = new Context();
    api = new OmaApi(backend.url);
    await mountOma(context, api);
    session = createOmaSession(context, api, backend.a);
    await session.open();
    await session.receive("clock_resumed", { content: "Continue." });
    expect(readings(1)).toHaveLength(1);

    offset = 11 * 60 * 1000;
    await session.receive("clock_refresh", { content: "What time is it now?" });
    expect(readings(2)).toHaveLength(2);
    expect(modelSystemText(backend.requests[2]!)).toBe(modelSystemText(backend.requests[0]!));
  } finally {
    clock.mockRestore();
    await session?.close();
    await context.fiber.dispose();
    backend.stop();
  }
});
