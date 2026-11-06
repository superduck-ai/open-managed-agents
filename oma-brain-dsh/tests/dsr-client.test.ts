import type { SessionHeader } from "@deepseek-ai/dsh-session";
import { expect, test } from "bun:test";
import { DsrClaims } from "../src/dsr/claims.ts";
import { DsrClient } from "../src/dsr/client.ts";
import { DsrError, type HttpRequest } from "../src/dsr/http.ts";
import { DsrInputStream } from "../src/dsr/input-stream.ts";
import type {
  Claim,
  PermissionRequest,
  Registration,
} from "../src/dsr/wire.ts";

const claim: Claim = {
  code_session_id: "cse_root",
  worker_epoch: "9007199254740993",
  session_token: "session-token",
  model_token: "model-token",
  claim_expires_at: "2026-11-03T00:00:30Z",
};
const registration: Registration = {
  context: {
    model: "model-a",
    models: { "model-a": { id: "model-a", api_backend: "messages" } },
    cwd: "/workspace",
    runtime_tools: [],
    mcp_config: {},
    subagent_contexts: {},
  },
  epoch: claim.worker_epoch,
  protocol: "1",
  lease: { ttl_ms: 20000, grace_ms: 10000, heartbeat_interval_ms: 6000 },
  observability_enabled: false,
};
const header = {
  version: 4,
  id: "cse_root",
  createdAt: 0,
  isSeeded: false,
} as SessionHeader;
const permission: PermissionRequest = {
  input_id: "input",
  turn: "1",
  dsh_session_id: "cse_root",
  tool_use_id: "call",
  tool: { kind: "builtin", name: "bash" },
  input: { command: "pwd" },
};

function fixture(
  respond: (
    request: HttpRequest,
    index: number,
  ) => Response | Promise<Response>,
) {
  const calls: HttpRequest[] = [];
  const client = new DsrClient(
    {
      request: async (request) => {
        calls.push(request);
        return respond(request, calls.length - 1);
      },
      sleep: async () => {},
    },
    claim,
  );
  return { client, calls };
}

test("DSR distinguishes a transcript gap from lost owner and retains its cursor", async () => {
  const { client, calls } = fixture(() =>
    Response.json(
      {},
      {
        status: 409,
        headers: { "DSR-Error": "transcript_gap", "DSR-Next-Seq": "36" },
      },
    ),
  );
  await expect(client.appendEvents("cse_root", [])).rejects.toMatchObject({
    status: 409,
    code: "transcript_gap",
    nextSeq: "36",
  });
  expect(calls).toHaveLength(1);
});

test("DSR requires per-input processing confirmation", async () => {
  for (const response of [
    { scheduled: 0, applied: 1, ignored: 0 },
    {
      scheduled: 0,
      applied: 0,
      ignored: 1,
      results: [
        {
          id: "input",
          status: "processing",
          outcome: "ignored",
          execution_status: "accepted",
        },
      ],
    },
  ]) {
    const { client } = fixture(() => Response.json(response));
    await expect(client.processing("input")).rejects.toBeInstanceOf(DsrError);
  }
});

test("a different persisted failure code is not successful completion", async () => {
  const { client } = fixture(() =>
    Response.json({ result: "failed", failure_code: "worker_shutdown" }),
  );
  await expect(
    client.finish("input", {
      result: "failed",
      failure_code: "quota_exhausted",
    }),
  ).rejects.toMatchObject({ code: "finish_idempotency_mismatch" });
});

test("DSR rejects missing seq zero, stalled pagination and wrong transcript identity", async () => {
  for (const page of [
    { header, events: [{ seq: 1 }], next_from_seq: null },
    { header, events: [], next_from_seq: "0" },
    { header: { ...header, id: "other" }, events: [], next_from_seq: null },
  ]) {
    const { client } = fixture(() => Response.json(page));
    await expect(client.readTranscript("cse_root")).rejects.toMatchObject({
      code: "invalid_response",
    });
  }
});

test("claim response loss is not retried as an idempotent write", async () => {
  let calls = 0;
  const claims = new DsrClaims(
    {
      request: async () => {
        calls++;
        throw new Error("response lost");
      },
      sleep: async () => {},
    },
    "fleet",
    "worker",
  );
  await expect(claims.claim(0)).rejects.toThrow("response lost");
  expect(calls).toBe(1);
});

test("permission retry retains original arguments and accepts external results", async () => {
  const { client, calls } = fixture((_request, index) =>
    index === 0
      ? new Response(null, { status: 503 })
      : Response.json({
          request_id: "dsrp_call",
          state: "resolved",
          decision: {
            behavior: "external_result",
            external_result: {
              content: [{ type: "text", text: "done" }],
              isError: false,
            },
          },
        }),
  );
  expect((await client.permission(permission)).state).toBe("resolved");
  expect(calls.map((call) => call.body)).toEqual([permission, permission]);
});

test("concurrent lease expiry shares one same-epoch registration", async () => {
  let attempts = 0;
  const { client, calls } = fixture((request) => {
    if (request.path.endsWith("/register")) return Response.json(registration);
    if (attempts++ < 2)
      return Response.json(
        {},
        { status: 410, headers: { "DSR-Error": "lease_expired" } },
      );
    return Response.json({ dsh_session_id: "root", next_seq: "0" });
  });
  await Promise.all([
    client.putHeader("root", { id: "root", version: 4 } as never, null),
    client.putHeader("root", { id: "root", version: 4 } as never, null),
  ]);
  expect(calls.filter((call) => call.path.endsWith("/register"))).toHaveLength(
    1,
  );
  expect(
    calls.every(
      (call) =>
        call.token === claim.session_token &&
        call.headers?.["DSR-Protocol"] === "1",
    ),
  ).toBe(true);
});

test("processing can succeed despite a missing ACK locator only with explicit durable state", async () => {
  const { client } = fixture(() =>
    Response.json({
      scheduled: 0,
      applied: 0,
      ignored: 1,
      results: [
        {
          id: "input",
          status: "processing",
          outcome: "ignored",
          execution_status: "running",
        },
      ],
    }),
  );
  expect(await client.processing("input")).toBe("running");
});

test("DSR retains immutable session bindings and string transcript watermarks", async () => {
  const { client, calls } = fixture(() =>
    Response.json({ dsh_session_id: "cse_root", next_seq: "9007199254740991" }),
  );
  expect(await client.putHeader("cse_root", header, null)).toBe(
    "9007199254740991",
  );
  expect(
    calls.every((call) => call.path.startsWith("/v1/dsr/sessions/cse_root/")),
  ).toBe(true);
  expect(
    calls.every(
      (call) =>
        !call.body || !Object.hasOwn(call.body as object, "worker_epoch"),
    ),
  ).toBe(true);
});

test("input stream preserves task identity separately from large SSE cursors", async () => {
  const abort = new AbortController();
  const { client, calls } = fixture(
    () =>
      new Response(
        ': keep-alive\n\nid: 9007199254740993\nevent: input\ndata: {"id":"input-a","lane":"task","kind":"user.message","content":[]}\n\n',
        { headers: { "Content-Type": "text/event-stream" } },
      ),
  );
  const stream = new DsrInputStream(client).read(abort.signal);
  expect((await stream.next()).value).toMatchObject({
    id: "input-a",
    lane: "task",
  });
  abort.abort();
  await stream.return(undefined);
  expect(calls[0]!.timeout).toBe(0);
});

test("a caller cannot rotate credentials under an existing DSR client", async () => {
  const mutable = { ...claim };
  let token: string | undefined;
  const client = new DsrClient(
    {
      request: async (request) => {
        token = request.token;
        return Response.json(registration);
      },
    },
    mutable,
  );
  mutable.session_token = "replacement-token";
  mutable.worker_epoch = "9007199254740994";
  await client.register();
  expect(token).toBe(claim.session_token);
  expect(client.claim.worker_epoch).toBe(claim.worker_epoch);
});

test("claim and release retain string epochs and separate fleet credentials", async () => {
  const calls: HttpRequest[] = [];
  const claims = new DsrClaims(
    {
      request: async (request) => {
        calls.push(request);
        return request.path.includes("/release")
          ? Response.json({ ok: true })
          : Response.json(claim);
      },
    },
    "fleet",
    "worker",
  );
  const acquired = await claims.claim(25000);
  expect(acquired?.worker_epoch).toBe("9007199254740993");
  await claims.release(acquired!, "idle");
  expect(calls[0]!.timeout).toBe(40000);
  expect(
    calls.every(
      (call) =>
        call.token === "fleet" && call.headers?.["X-Worker-ID"] === "worker",
    ),
  ).toBe(true);
  expect(calls[1]!.body).toEqual({ epoch: "9007199254740993", reason: "idle" });
});

test("directory enumeration uses advancing cursors and preserves header-only children", async () => {
  const { client, calls } = fixture((_request, index) =>
    Response.json({
      data: [
        {
          dsh_session_id: index === 0 ? "child-a" : "child-b",
          parent_dsh_session_id: "cse_root",
          session_thread_id: null,
          header_present: true,
          next_seq: "0",
        },
      ],
      next_after: index === 0 ? "child-a" : null,
    }),
  );
  expect(
    (await client.listTranscripts()).map((row) => row.dsh_session_id),
  ).toEqual(["child-a", "child-b"]);
  expect(calls[1]!.path).toContain("after=child-a");
});

test("public output response loss resends identical keys without accepting a preview as durable output", async () => {
  const { client, calls } = fixture((_request, index) =>
    index === 0
      ? new Response(null, { status: 503 })
      : Response.json({ accepted: 0, duplicates: 1 }),
  );
  const events = [
    {
      id: "output",
      type: "system.message" as const,
      content: [{ type: "text", text: "done" }],
    },
  ];
  await client.publish(events);
  expect(calls.map((call) => call.body)).toEqual([{ events }, { events }]);
});

test("directory and event cursors cannot skip unseen records", async () => {
  const invalid = fixture(() =>
    Response.json({
      data: [{ dsh_session_id: "child-a", next_seq: "0" }],
      next_after: "child-z",
    }),
  );
  await expect(invalid.client.listTranscripts()).rejects.toMatchObject({
    code: "invalid_response",
  });
  const skipped = fixture(() =>
    Response.json({ header, events: [], next_from_seq: "1" }),
  );
  await expect(skipped.client.readTranscript("cse_root")).rejects.toMatchObject(
    { code: "invalid_response" },
  );
});

test("DSR separates execution completion and cancellation intent from transport status", async () => {
  for (const state of ["processing", "terminal", "accepted", undefined]) {
    const { client } = fixture(() =>
      Response.json({
        scheduled: 0,
        applied: 1,
        ignored: 0,
        results: [
          {
            id: "input",
            status: "processing",
            outcome: "applied",
            execution_status: state,
          },
        ],
      }),
    );
    await expect(client.processing("input")).rejects.toMatchObject({
      code: "processing_unconfirmed",
    });
  }
  const { client } = fixture(() =>
    Response.json({
      scheduled: 0,
      applied: 1,
      ignored: 0,
      results: [
        {
          id: "input",
          status: "processing",
          outcome: "applied",
          execution_status: "completed",
          cancel_requested_at: "2026-11-04T00:00:00Z",
        },
      ],
    }),
  );
  expect(await client.processing("input")).toBe("completed");
  expect(client.inputCancelled("input")).toBe(true);
});

test("DSR treats scheduled task settlement as durable acceptance without claiming a broker ACK", async () => {
  const { client } = fixture(() =>
    Response.json({
      applied: 0,
      ignored: 0,
      scheduled: 1,
      results: [
        {
          id: "input",
          status: "processed",
          outcome: "scheduled",
          execution_status: "completed",
        },
      ],
    }),
  );
  const response = await client.delivery([
    { id: "input", status: "processed" },
  ]);
  expect(response.scheduled).toBe(1);
  expect(response.applied).toBe(0);
});

test("registration rejects missing or malformed frozen context", async () => {
  for (const context of [
    undefined,
    null,
    {},
    { ...registration.context, models: null },
    { ...registration.context, cwd: 12 },
    {
      ...registration.context,
      subagent_contexts: { bad: { mcp_config: null } },
    },
  ]) {
    const { client } = fixture(() =>
      Response.json({ ...registration, context }),
    );
    await expect(client.register()).rejects.toThrow();
  }
});

test("registration carries the complete initial context", async () => {
  const { client, calls } = fixture(() => Response.json(registration));
  expect((await client.register()).context).toEqual(registration.context);
  expect(calls.map((call) => call.path)).toEqual([
    "/v1/dsr/sessions/cse_root/register",
  ]);
});

test("transcript reads send only the sequence cursor", async () => {
  const { client, calls } = fixture(() =>
    Response.json({
      header: null,
      events: [{ seq: 200 }],
      next_from_seq: null,
    }),
  );
  await client.readTranscript("cse_root", "200");
  const url = new URL(calls[0]!.path, "http://localhost");
  expect(url.pathname.endsWith("/transcripts/cse_root")).toBe(true);
  expect([...url.searchParams.entries()]).toEqual([["from_seq", "200"]]);
});
