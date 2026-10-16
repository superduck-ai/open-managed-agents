import { reportApiAuthFailure, type ApiError } from './client';
export class PlatformAdminClient {
  private controller = new AbortController();
  csrfToken = '';
  constructor(private denied: (status: number) => void) {}

  setCSRFToken(token: string) {
    this.csrfToken = token;
  }

  invalidate() {
    this.controller.abort();
    this.controller = new AbortController();
    this.csrfToken = '';
  }

  async request<T>(
    path: string,
    options: { method?: string; body?: unknown; key?: string; signal?: AbortSignal } = {},
  ): Promise<T> {
    const scope = this.controller.signal;
    const headers = new Headers({ Accept: 'application/json' });
    if (options.body !== undefined) headers.set('Content-Type', 'application/json');
    if (options.method && options.method !== 'GET') headers.set('X-CSRF-Token', this.csrfToken);
    if (options.key) headers.set('Idempotency-Key', options.key);
    const read = !options.method || options.method === 'GET';
    const response = await fetch(`/api/platform-admin${path}`, {
      method: options.method ?? 'GET',
      headers,
      credentials: 'include',
      cache: 'no-store',
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: read ? AbortSignal.any([scope, ...(options.signal ? [options.signal] : [])]) : undefined,
    });
    const body = await response.json().catch(() => null);
    scope.throwIfAborted();
    if (!response.ok) {
      if (response.status === 401 || response.status === 403) {
        this.invalidate();
        this.denied(response.status);
        if (response.status === 401) reportApiAuthFailure(401, {});
      }
      throw platformError(response.status, body);
    }
    if (body === null)
      throw {
        status: 502,
        code: 'invalid_response',
        message: 'Invalid service response. Please try again.',
      } satisfies ApiError;
    return body as T;
  }
}

function platformError(status: number, body: { error?: { code?: string; message?: string } } | null): ApiError {
  return {
    status,
    code: body?.error?.code ?? 'request_failed',
    message: body?.error?.message ?? 'Request failed. Please try again later.',
  };
}
