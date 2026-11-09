import { Context } from "@deepseek-ai/cordis";
import { expect, spyOn, test } from "bun:test";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import { OmaApi } from "../src/api.ts";
import { DsrClient } from "../src/dsr/client.ts";
import { DsrSession } from "../src/dsr/session.ts";
import { dsrAssignment } from "../src/dsr/assignment.ts";
import { createDsrTransport } from "../src/dsr/transport.ts";
import { dsrRuntimeServices, mountDsr } from "../src/dsr/worker.ts";
import { omaRuntimeConfig } from "../src/oma-session.ts";
import { DsrBackend } from "./dsr-backend.ts";
import type { PublicEvent } from "../src/dsr/wire.ts";

test("session checkpoint sends buffered append previews before the durable message", async () => {
  const backend = new DsrBackend();
  await backend.ready;
  const ctx = new Context();
  let session: DsrSession | undefined;
  try {
    const api = new OmaApi(backend.url);
    const clients = await mountDsr(ctx, api);
    const client = new DsrClient(createDsrTransport(api), {
      code_session_id: backend.a.session_id,
      session_token: backend.a.ingress_token,
      model_token: backend.a.model_token,
      worker_epoch: String(backend.a.worker_epoch),
      claim_expires_at: new Date(Date.now() + 30000).toISOString(),
    });
    clients.set(backend.a.session_id, client);
    const assignment = dsrAssignment(
      api,
      client.claim,
      await client.register(),
    );
    api.assignments.set(assignment.session_id, assignment);
    await ctx.plugin({
      inject: dsrRuntimeServices,
      apply: async (scope: Context) => {
        session = new DsrSession(
          scope,
          client,
          omaRuntimeConfig(api, assignment),
        );
        await session.open();
      },
    });
    const batches: PublicEvent[][] = [];
    const publish = spyOn(client, "publish").mockImplementation(
      async (events) => {
        batches.push(events);
      },
    );
    try {
      session!.stream({
        type: "start",
        attemptId: "attempt",
        revision: 1,
        turn: 1,
        step: 1,
      } as AssistantStreamFrame);
      for (const text of ["hello", " world"]) {
        session!.stream({
          type: "chunk",
          attemptId: "attempt",
          revision: 1,
          index: 0,
          time: 0,
          chunk: { type: "text-delta", index: 0, text },
        } as AssistantStreamFrame);
      }
      expect(batches).toHaveLength(0);
      await session!.publish({
        id: "formal",
        type: "system.message",
        content: [{ type: "text", text: "finished" }],
      });
      expect(batches).toHaveLength(2);
      expect(batches[0]!.map((event) => event.type)).toEqual([
        "preview.start",
        "preview.delta",
        "preview.delta",
      ]);
      expect(batches[0]![1]).toMatchObject({
        delta: { content: { text: "hello" } },
      });
      expect(batches[0]![2]).toMatchObject({
        delta: { content: { text: " world" } },
      });
      expect(batches[1]!.map((event) => event.id)).toEqual(["formal"]);
    } finally {
      publish.mockRestore();
    }
  } finally {
    await session?.close();
    await ctx.fiber.dispose();
    backend.stop();
  }
});
