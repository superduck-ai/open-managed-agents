import { Context } from "@deepseek-ai/cordis";
import { expect, test } from "bun:test";
import { OmaApi } from "../src/api.ts";
import { DsrClaims } from "../src/dsr/claims.ts";
import { DsrClient } from "../src/dsr/client.ts";
import { DsrOwner } from "../src/dsr/worker.ts";
import type { HttpRequest } from "../src/dsr/http.ts";

for (const [status, code, release] of [
  [409, "input_not_eligible", true],
  [400, "invalid_request", true],
  [429, "rate_limited", true],
  [401, "unauthorized", true],
  [410, "lease_expired", true],
  [404, "session_not_found", true],
  [503, "unavailable", true],
  [409, "epoch_superseded", false],
] as const) {
  test(`owner ${release ? "releases" : "does not release"} its claim after ${code}`, async () => {
    const calls: HttpRequest[] = [];
    const transport = {
      request: async (request: HttpRequest) => {
        calls.push(request);
        return request.path.endsWith("/release")
          ? Response.json({ ok: true })
          : Response.json({}, { status, headers: { "DSR-Error": code } });
      },
      sleep: async () => {},
    };
    const claim = {
      code_session_id: "cse_release",
      worker_epoch: "7",
      session_token: "session",
      model_token: "model",
      claim_expires_at: new Date(Date.now() + 60000).toISOString(),
    };
    const owner = new DsrOwner(
      new Context(),
      new OmaApi("http://127.0.0.1"),
      new DsrClient(transport, claim),
      new DsrClaims(transport, "fleet", "worker"),
      10,
    );
    await owner.run();
    const releases = calls.filter((request) =>
      request.path.endsWith("/release"),
    );
    expect(releases).toHaveLength(release ? 1 : 0);
    if (release) {
      expect(releases[0]!.token).toBe("fleet");
      expect(releases[0]!.body).toEqual({
        epoch: "7",
        reason: "admission_failed",
      });
    }
  });
}

test("a failed best-effort release still completes owner cleanup", async () => {
  let releases = 0;
  const transport = {
    request: async (request: HttpRequest) => {
      if (request.path.endsWith("/release")) {
        releases++;
        throw new TypeError("offline");
      }
      return Response.json(
        {},
        { status: 409, headers: { "DSR-Error": "input_not_eligible" } },
      );
    },
    sleep: async () => {},
  };
  const owner = new DsrOwner(
    new Context(),
    new OmaApi("http://127.0.0.1"),
    new DsrClient(transport, {
      code_session_id: "cse_release",
      worker_epoch: "7",
      session_token: "session",
      model_token: "model",
      claim_expires_at: new Date(Date.now() + 60000).toISOString(),
    }),
    new DsrClaims(transport, "fleet", "worker"),
    10,
  );
  await owner.run();
  await owner.close();
  expect(releases).toBe(4);
});
