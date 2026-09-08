export type ApiError = {
  status: number;
  code: string;
  message: string;
  modelId?: string;
};

export function isModelConfigurationUnavailable(error: unknown) {
  return Boolean(error && typeof error === 'object' && (error as Partial<ApiError>).status === 503);
}

type RequestOptions = RequestInit & {
  csrfToken?: string;
  context?: ConsoleRequestContext;
};

export type ConsoleRequestContext = {
  organizationUuid?: string;
  workspaceId?: string;
  csrfToken?: string;
};

let consoleRequestContext: ConsoleRequestContext = {};
let scopeController = new AbortController();
const failureListeners = new Set<(status: number, context: ConsoleRequestContext) => void>();

export function onApiAuthFailure(listener: (status: number, context: ConsoleRequestContext) => void) {
  failureListeners.add(listener);
  return () => {
    failureListeners.delete(listener);
  };
}

export function cancelScopeRequests() {
  scopeController.abort();
  scopeController = new AbortController();
}

export function getScopeSignal() {
  return scopeController.signal;
}

export function reportApiAuthFailure(status: number, context: ConsoleRequestContext) {
  if (status === 401 || status === 403) failureListeners.forEach((listener) => listener(status, context));
}

export function reportScopeSuccess(context: ConsoleRequestContext) {
  failureListeners.forEach((listener) => listener(200, context));
}

export function setConsoleRequestContext(context: ConsoleRequestContext) {
  consoleRequestContext = context;
}

export function getConsoleRequestContext() {
  return { ...consoleRequestContext };
}

export function consoleRequestHeaders(headers?: HeadersInit, context?: ConsoleRequestContext) {
  const requestHeaders = new Headers(headers);
  applyConsoleRequestContext(requestHeaders, context);
  return requestHeaders;
}

export async function consoleApi<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers(options.headers);
  headers.set('Accept', 'application/json');

  if (options.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  applyConsoleRequestContext(headers, options.context);
  const csrfToken = options.csrfToken ?? consoleRequestContext.csrfToken;
  if (csrfToken) {
    headers.set('X-CSRF-Token', csrfToken);
  }

  return requestJson<T>(path, {
    ...options,
    headers,
    credentials: 'include',
  });
}

export async function filesApi<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = filesRequestHeaders(options.headers);
  headers.set('Accept', 'application/json');

  return requestJson<T>(path, {
    ...options,
    headers,
    credentials: 'include',
  });
}

export async function skillsApi<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = skillsRequestHeaders(options.headers);
  headers.set('Accept', 'application/json');

  return requestJson<T>(path, {
    ...options,
    headers,
    credentials: 'include',
  });
}

export async function messageBatchesApi<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = messageBatchesRequestHeaders(options.headers);
  headers.set('Accept', 'application/json');

  return requestJson<T>(path, {
    ...options,
    headers,
    credentials: 'include',
  });
}

export async function webhooksApi<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = webhooksRequestHeaders(options.headers);
  headers.set('Accept', 'application/json');

  return requestJson<T>(path, {
    ...options,
    headers,
    credentials: 'include',
  });
}

export function filesRequestHeaders(headers?: HeadersInit) {
  const requestHeaders = new Headers(headers);
  requestHeaders.set('anthropic-beta', 'files-api-2025-04-14');
  applyConsoleRequestContext(requestHeaders);
  return requestHeaders;
}

export function skillsRequestHeaders(headers?: HeadersInit) {
  const requestHeaders = new Headers(headers);
  requestHeaders.set('anthropic-beta', 'skills-2025-10-02');
  applyConsoleRequestContext(requestHeaders);
  return requestHeaders;
}

export function messageBatchesRequestHeaders(headers?: HeadersInit) {
  const requestHeaders = new Headers(headers);
  requestHeaders.set('anthropic-beta', 'message-batches-2024-09-24');
  requestHeaders.set('anthropic-version', '2023-06-01');
  applyConsoleRequestContext(requestHeaders);
  return requestHeaders;
}

export function webhooksRequestHeaders(headers?: HeadersInit) {
  const requestHeaders = new Headers(headers);
  requestHeaders.set('anthropic-beta', 'webhooks-2026-03-01');
  applyConsoleRequestContext(requestHeaders);
  return requestHeaders;
}

function applyConsoleRequestContext(headers: Headers, context: ConsoleRequestContext = consoleRequestContext) {
  if (context.organizationUuid && !headers.has('X-Organization-UUID')) {
    headers.set('X-Organization-UUID', context.organizationUuid);
  }
  if (context.workspaceId && !headers.has('X-Workspace-ID')) {
    headers.set('X-Workspace-ID', context.workspaceId);
  }
}

async function requestJson<T>(path: string, options: RequestInit): Promise<T> {
  const headers = new Headers(options.headers);
  const context = {
    organizationUuid: headers.get('X-Organization-UUID') ?? undefined,
    workspaceId: headers.get('X-Workspace-ID') ?? undefined,
  };
  const scoped = Boolean(context.organizationUuid || context.workspaceId);
  const scopeSignal = scopeController.signal;
  const readOnly = !options.method || ['GET', 'HEAD'].includes(options.method.toUpperCase());
  // 写请求一旦发送就让服务端完成；只隔离返回值，不因切换取消或重发写操作。
  const signal =
    scoped && readOnly ? AbortSignal.any([scopeSignal, ...(options.signal ? [options.signal] : [])]) : options.signal;
  const response = await fetch(path, { ...options, signal });
  if (scoped) scopeSignal.throwIfAborted();
  signal?.throwIfAborted();
  if (!response.ok) {
    const error = await toApiError(response);
    signal?.throwIfAborted();
    if (scoped) scopeSignal.throwIfAborted();
    reportApiAuthFailure(response.status, context);
    throw error;
  }
  if (response.status === 204) {
    return undefined as T;
  }
  const data = (await response.json()) as T;
  if (scoped) scopeSignal.throwIfAborted();
  signal?.throwIfAborted();
  if (scoped && readOnly) reportScopeSuccess(context);
  return data;
}

async function toApiError(response: Response): Promise<ApiError> {
  let message = response.statusText;
  let code = 'request_failed';
  let modelId: string | undefined;

  try {
    const payload = (await response.json()) as Record<string, unknown>;
    const error = payload.error;
    if (typeof error === 'string') {
      code = error;
    } else if (error && typeof error === 'object') {
      const typedError = error as Record<string, unknown>;
      if (typeof typedError.type === 'string') {
        code = typedError.type;
      }
      if (typeof typedError.message === 'string') {
        message = typedError.message;
      }
      if (typeof typedError.code === 'string') {
        code = typedError.code;
      }
    }
    if (typeof payload.code === 'string') {
      code = payload.code;
    }
    if (typeof payload.message === 'string') {
      message = payload.message;
    }
    if (typeof payload.model_id === 'string') {
      modelId = payload.model_id;
    }
  } catch {
    // Keep the status text when the backend returned a non-JSON error body.
  }

  return { status: response.status, code, message, ...(modelId ? { modelId } : {}) };
}
