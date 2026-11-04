import type { Assignment } from "./assignment.ts";

export class HttpError extends Error {
  constructor(readonly status: number) {
    super(`OMA HTTP ${status}`);
  }
}

export class OmaApi {
  telemetry?: { close(id: string): Promise<void> };
  readonly assignments = new Map<string, Assignment>();
  readonly modelSessions = new Map<string, string>();
  readonly modelResponses = new Map<string, string>();
  readonly modelRequestIDs = new Map<string, string>();
  constructor(
    readonly base: string,
    readonly fetcher: typeof fetch = fetch,
  ) {}

  async requestResponse(
    path: string,
    token: string,
    method = "GET",
    body?: unknown,
    headers?: HeadersInit,
    signal?: AbortSignal,
    timeout = 20_000,
  ): Promise<Response> {
    return this.fetcher(`${this.base.replace(/\/$/, "")}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
        ...headers,
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal:
        timeout === 0
          ? signal
          : signal
            ? AbortSignal.any([signal, AbortSignal.timeout(timeout)])
            : AbortSignal.timeout(timeout),
      redirect: "error",
    });
  }

  assignment(id: string): Assignment {
    const assignment = this.assignments.get(id);
    if (!assignment) throw new Error("Session has no claimed assignment");
    return assignment;
  }
}
