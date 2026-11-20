import { expect, test } from "bun:test";
import { SessionId } from "@deepseek-ai/dsh-session";
import { OmaApi } from "../src/api.ts";
import { omaRoute } from "../src/model-provider.ts";
import { assignment } from "./backend.ts";

function budgetResponse(state: "available" | "reached", status: number) {
  return new Response(null, { status, headers: { "X-Oma-Session-Budget": state } });
}

function budgetCall(backend: "messages" | "chat_completions",
  fetcher: (...args: Parameters<typeof fetch>) => Promise<Response>, signal?: AbortSignal) {
  const a = assignment();
  a.launch.models!["test-model"]!.api_backend = backend;
  const api = new OmaApi("http://oma.test", Object.assign(fetcher, { preconnect: fetch.preconnect }));
  api.assignments.set(a.session_id, a);
  const route = omaRoute(api, { model: "test-model", provider: "oma", sessionId: SessionId(a.session_id), messages: [] });
  return route.model.doStream({
    prompt: [{ role: "user", content: [{ type: "text", text: "finish the original task" }] }],
    abortSignal: signal,
  });
}

test("provider validation errors do not retry a model request", async () => {
  let posts = 0;
  const call = budgetCall("messages", async () => {
    posts++;
    return Response.json({ type: "error", error: { type: "invalid_request_error", message: "Session budget reached" } }, { status: 400 });
  });
  await expect(call).rejects.toThrow();
  expect(posts).toBe(1);
});

test("cancelling a paused turn discards its pending request", async () => {
  const stop = new AbortController();
  const probed = Promise.withResolvers<void>();
  const methods: string[] = [];
  const call = budgetCall("messages", async (_input, init) => {
    methods.push(init!.method!);
    if (init!.method === "POST") return budgetResponse("reached", 400);
    probed.resolve();
    return budgetResponse("reached", 204);
  }, stop.signal);
  await probed.promise;
  stop.abort(new Error("cancel paused turn"));
  await expect(call).rejects.toThrow();
  expect(methods).toEqual(["POST", "HEAD"]);
});

test.each(["messages", "chat_completions"] as const)("%s resumes with the identical request only after the budget opens", async (backend) => {
  const bodies: string[] = [];
  let probes = 0;
  const call = await budgetCall(backend, async (_input, init) => {
    if (init!.method === "HEAD") {
      expect(init!.body).toBeUndefined();
      probes++;
      if (probes === 1) return budgetResponse("reached", 204);
      if (probes === 2) {
        if (backend === "messages") throw new TypeError("connection reset");
        return new Response(null, { status: 503 });
      }
      return budgetResponse("available", 204);
    }
    bodies.push(String(init!.body));
    if (bodies.length === 1) return budgetResponse("reached", 400);
    return new Response("", { headers: { "content-type": "text/event-stream" } });
  });
  await call.stream.cancel();
  expect(probes).toBe(3);
  expect(bodies).toHaveLength(2);
  expect(bodies[1]).toBe(bodies[0]);
});
