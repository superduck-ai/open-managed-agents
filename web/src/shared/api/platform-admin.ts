import { reportApiAuthFailure, type ApiError } from './client';
import type { RuntimeCapabilities, RuntimeProviderKind } from '../workspaces/runtime';
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
    const signal = options.signal ? AbortSignal.any([scope, options.signal]) : scope;
    const headers = new Headers({ Accept: 'application/json' });
    const multipart = options.body instanceof FormData;
    if (options.body !== undefined && !multipart) headers.set('Content-Type', 'application/json');
    if (options.method && options.method !== 'GET') headers.set('X-CSRF-Token', this.csrfToken);
    if (options.key) headers.set('Idempotency-Key', options.key);
    const read = !options.method || options.method === 'GET';
    const response = await fetch(`/api/platform-admin${path}`, {
      method: options.method ?? 'GET',
      headers,
      credentials: 'include',
      cache: 'no-store',
      body: multipart
        ? (options.body as FormData)
        : options.body === undefined
          ? undefined
          : JSON.stringify(options.body),
      signal: read ? signal : undefined,
    });
    const body = await response.json().catch(() => null);
    signal.throwIfAborted();
    if (response.status === 204) return undefined as T;
    if (!response.ok) {
      const error = platformError(response.status, body);
      if (
        response.status === 401 ||
        (response.status === 403 && ['platform_access_denied', 'verified_login_required'].includes(error.code))
      ) {
        this.invalidate();
        this.denied(response.status);
        if (response.status === 401) reportApiAuthFailure(401, {});
      }
      throw error;
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

export type OAuthProviderRecord = {
  archived: boolean;
  enabled: boolean;
  provider_key: string;
  default_secret_name: string;
  default_allowed_hosts: string[];
  builtin: boolean;
  protected: boolean;
  has_client_id: boolean;
  configured: boolean;
  version: number;
  id: string;
  name: string;
  logo_url?: string;
  authorize_endpoint: string;
  token_endpoint: string;
  scope: string;
  client_id: string;
  has_client_secret: boolean;
  token_endpoint_auth_method: string;
  token_endpoint_encoding: string;
  created_at: string;
  updated_at: string;
};

export type OAuthProviderInput = {
  default_allowed_hosts?: string[];
  default_secret_name?: string;
  enabled?: boolean;
  name: string;
  logo_url?: string;
  authorize_endpoint: string;
  token_endpoint: string;
  scope: string;
  client_id?: string;
  client_secret?: string;
  token_endpoint_auth_method?: string;
  token_endpoint_encoding?: string;
};

export function listOAuthProviders(client: PlatformAdminClient) {
  return client.request<OAuthProviderRecord[]>('/oauth-providers');
}

export function createOAuthProvider(client: PlatformAdminClient, input: OAuthProviderInput) {
  return client.request<OAuthProviderRecord>('/oauth-providers', { method: 'POST', body: input });
}

export function updateOAuthProvider(
  client: PlatformAdminClient,
  id: string,
  input: Partial<OAuthProviderInput> & { archived?: boolean; expected_version: number },
) {
  return client.request<OAuthProviderRecord>(`/oauth-providers/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: input,
  });
}

export function deleteOAuthProvider(client: PlatformAdminClient, id: string, expectedVersion: number) {
  return client.request<void>(`/oauth-providers/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    body: { expected_version: expectedVersion },
  });
}

export type RuntimeProfileStatus = 'active' | 'disabled' | 'archived';

export type RuntimeProfileRecord = {
  uuid: string;
  id: string;
  scope: string;
  name: string;
  provider_kind: RuntimeProviderKind;
  endpoint_url: string;
  isolation_mode: string;
  config: Record<string, unknown>;
  capability_overrides: Record<string, unknown>;
  effective_capabilities: RuntimeCapabilities;
  has_credentials: boolean;
  credential_hint: string;
  credential_revision: number;
  builtin: boolean;
  status: RuntimeProfileStatus;
  last_health_status: string;
  last_health_checked_at: string | null;
  last_health_error: string;
  version: number;
  created_at: string;
  updated_at: string;
};

export type RuntimeProfileCreateInput = {
  name: string;
  provider_kind: string;
  endpoint_url: string;
  isolation_mode?: string;
  config?: Record<string, unknown>;
  capability_overrides?: Record<string, unknown>;
  credentials?: Record<string, unknown>;
  status?: RuntimeProfileStatus;
};

export type RuntimeProfileUpdateInput = {
  expected_version: number;
  name?: string;
  endpoint_url?: string;
  isolation_mode?: string;
  config?: Record<string, unknown>;
  capability_overrides?: Record<string, unknown>;
  credentials?: Record<string, unknown>;
  status?: RuntimeProfileStatus;
};

export type RuntimeProfileTestResult = {
  status: string;
  latency_ms: number;
  message: string;
  checked_at: string;
};

export type OrganizationRuntimePolicyRecord = {
  organization_uuid: string;
  default_runtime_profile_uuid: string;
  allowed_runtime_profile_uuids: string[];
  profiles: RuntimeProfileRecord[];
};

export type UpdateOrganizationRuntimePolicyInput = {
  default_runtime_profile_uuid: string;
  allowed_runtime_profile_uuids: string[];
};

export function listRuntimeProfiles(client: PlatformAdminClient) {
  return client.request<RuntimeProfileRecord[]>('/runtime-profiles');
}

export function createRuntimeProfile(client: PlatformAdminClient, input: RuntimeProfileCreateInput) {
  return client.request<RuntimeProfileRecord>('/runtime-profiles', { method: 'POST', body: input });
}

export function updateRuntimeProfile(client: PlatformAdminClient, id: string, input: RuntimeProfileUpdateInput) {
  return client.request<RuntimeProfileRecord>(`/runtime-profiles/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: input,
  });
}

export function deleteRuntimeProfile(client: PlatformAdminClient, id: string, expectedVersion: number) {
  return client.request<void>(`/runtime-profiles/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    body: { expected_version: expectedVersion },
  });
}

export function testRuntimeProfile(client: PlatformAdminClient, id: string) {
  return client.request<RuntimeProfileTestResult>(`/runtime-profiles/${encodeURIComponent(id)}/test`, {
    method: 'POST',
  });
}

export function getOrganizationRuntimePolicy(client: PlatformAdminClient, organizationUuid: string) {
  return client.request<OrganizationRuntimePolicyRecord>(
    `/organizations/${encodeURIComponent(organizationUuid)}/runtime-policy`,
  );
}

export function updateOrganizationRuntimePolicy(
  client: PlatformAdminClient,
  organizationUuid: string,
  input: UpdateOrganizationRuntimePolicyInput,
) {
  return client.request<OrganizationRuntimePolicyRecord>(
    `/organizations/${encodeURIComponent(organizationUuid)}/runtime-policy`,
    {
      method: 'PUT',
      body: input,
    },
  );
}

function platformError(status: number, body: { error?: { code?: string; message?: string } } | null): ApiError {
  return {
    status,
    code: body?.error?.code ?? 'request_failed',
    message: body?.error?.message ?? 'Request failed. Please try again later.',
  };
}
