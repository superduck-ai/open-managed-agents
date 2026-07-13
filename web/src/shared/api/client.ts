export type ApiError = {
  status: number;
  code: string;
  message: string;
};

type RequestOptions = RequestInit & {
  csrfToken?: string;
};

type ConsoleRequestContext = {
  organizationUuid?: string;
  workspaceId?: string;
};

let consoleRequestContext: ConsoleRequestContext = {};

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
  headers.set("Accept", "application/json");

  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  applyConsoleRequestContext(headers);
  if (options.csrfToken) {
    headers.set("X-CSRF-Token", options.csrfToken);
  }

  return requestJson<T>(path, {
    ...options,
    headers,
    credentials: "include",
  });
}

export async function filesApi<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = filesRequestHeaders(options.headers);
  headers.set("Accept", "application/json");

  return requestJson<T>(path, {
    ...options,
    headers,
    credentials: "include",
  });
}

export async function skillsApi<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = skillsRequestHeaders(options.headers);
  headers.set("Accept", "application/json");

  return requestJson<T>(path, {
    ...options,
    headers,
    credentials: "include",
  });
}

export async function messageBatchesApi<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = messageBatchesRequestHeaders(options.headers);
  headers.set("Accept", "application/json");

  return requestJson<T>(path, {
    ...options,
    headers,
    credentials: "include",
  });
}

export async function webhooksApi<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = webhooksRequestHeaders(options.headers);
  headers.set("Accept", "application/json");

  return requestJson<T>(path, {
    ...options,
    headers,
    credentials: "include",
  });
}

export function filesRequestHeaders(headers?: HeadersInit) {
  const requestHeaders = new Headers(headers);
  requestHeaders.set("anthropic-beta", "files-api-2025-04-14");
  applyConsoleRequestContext(requestHeaders);
  return requestHeaders;
}

export function skillsRequestHeaders(headers?: HeadersInit) {
  const requestHeaders = new Headers(headers);
  requestHeaders.set("anthropic-beta", "skills-2025-10-02");
  applyConsoleRequestContext(requestHeaders);
  return requestHeaders;
}

export function messageBatchesRequestHeaders(headers?: HeadersInit) {
  const requestHeaders = new Headers(headers);
  requestHeaders.set("anthropic-beta", "message-batches-2024-09-24");
  requestHeaders.set("anthropic-version", "2023-06-01");
  applyConsoleRequestContext(requestHeaders);
  return requestHeaders;
}

export function webhooksRequestHeaders(headers?: HeadersInit) {
  const requestHeaders = new Headers(headers);
  requestHeaders.set("anthropic-beta", "webhooks-2026-03-01");
  applyConsoleRequestContext(requestHeaders);
  return requestHeaders;
}

function applyConsoleRequestContext(headers: Headers, context: ConsoleRequestContext = consoleRequestContext) {
  if (context.organizationUuid && !headers.has("X-Organization-UUID")) {
    headers.set("X-Organization-UUID", context.organizationUuid);
  }
  if (context.workspaceId && !headers.has("X-Workspace-ID")) {
    headers.set("X-Workspace-ID", context.workspaceId);
  }
}

async function requestJson<T>(path: string, options: RequestInit): Promise<T> {
  const response = await fetch(path, options);
  if (!response.ok) {
    throw await toApiError(response);
  }
  return (await response.json()) as T;
}

async function toApiError(response: Response): Promise<ApiError> {
  let message = response.statusText;
  let code = "request_failed";

  try {
    const payload = (await response.json()) as Record<string, unknown>;
    const error = payload.error;
    if (typeof error === "string") {
      code = error;
    } else if (error && typeof error === "object") {
      const typedError = error as Record<string, unknown>;
      if (typeof typedError.type === "string") {
        code = typedError.type;
      }
      if (typeof typedError.message === "string") {
        message = typedError.message;
      }
    }
    if (typeof payload.message === "string") {
      message = payload.message;
    }
  } catch {
    // Keep the status text when the backend returned a non-JSON error body.
  }

  return { status: response.status, code, message };
}
