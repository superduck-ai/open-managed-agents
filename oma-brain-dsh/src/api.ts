import type { Assignment } from "./assignment.ts";
import type { Payload } from "./json.ts";

export class HttpError extends Error {
  constructor(readonly status: number) {
    super(`OMA HTTP ${status}`);
  }
}

export class OmaApi {
  readonly assignments = new Map<string, Assignment>();
  readonly modelSessions = new Map<string, string>();
  readonly modelResponses = new Map<string, string>();
  constructor(
    readonly base: string,
    readonly fetcher: typeof fetch = fetch,
  ) {}

  async request(
    path: string,
    token: string,
    method = "GET",
    body?: unknown,
    headers?: HeadersInit,
    signal?: AbortSignal,
    timeout = 20_000,
  ): Promise<Payload> {
    const response = await this.requestResponse(
      path,
      token,
      method,
      body,
      headers,
      signal,
      timeout,
    );
    if (!response.ok) throw new HttpError(response.status);
    if (response.status === 204) return {};
    return response.json();
  }

  /** Generic HTTP transport shared by OMA operations and the CCRv2 client. */
  async requestResponse(
    path: string,
    token: string,
    method = "GET",
    body?: unknown,
    headers?: HeadersInit,
    signal?: AbortSignal,
    timeout = 20_000,
  ): Promise<Response> {
    return this.fetcher(
      `${this.base.replace(/\/$/, "")}${path}`,
      {
        method,
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
          ...headers,
        },
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: timeout === 0 ? signal : signal
          ? AbortSignal.any([signal, AbortSignal.timeout(timeout)])
          : AbortSignal.timeout(timeout),
        redirect: "error",
      },
    );
  }

  assignment(id: string): Assignment {
    const assignment = this.assignments.get(id);
    if (!assignment) throw new Error("Session has no claimed assignment");
    return assignment;
  }

}
