import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderPlatformAdmin as render } from '../../../test/platform-admin';
import '../../../test/setup';
import { PlatformAdminClient, type RuntimeProfileRecord } from '../../../shared/api/platform-admin';
import { defaultNativeCapabilities } from '../../../shared/workspaces/runtime';
import { PlatformAdminContext } from '../context';
import { RuntimeProfilesPage } from './RuntimeProfilesPage';
import { OrganizationRuntimePolicySection } from './OrganizationRuntimePolicySection';

const { cleanup, fireEvent, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

const builtinProfile: RuntimeProfileRecord = {
  uuid: '00000000-0000-4000-8000-000000000001',
  id: 'rprof_oma_native_default',
  scope: 'platform',
  name: 'OMA Native',
  provider_kind: 'oma_native',
  endpoint_url: '',
  isolation_mode: 'isolated',
  config: {},
  capability_overrides: {},
  effective_capabilities: defaultNativeCapabilities,
  has_credentials: false,
  credential_hint: '',
  credential_revision: 1,
  builtin: true,
  status: 'active',
  last_health_status: 'healthy',
  last_health_checked_at: null,
  last_health_error: '',
  version: 1,
  created_at: '2026-11-25T00:00:00Z',
  updated_at: '2026-11-25T00:00:00Z',
};

const qoderProfile: RuntimeProfileRecord = {
  ...builtinProfile,
  uuid: '00000000-0000-4000-8000-000000000099',
  id: 'rprof_qoder_cn',
  name: 'Qoder Cloud CN',
  provider_kind: 'qoder_cloud',
  endpoint_url: 'https://api.qoder.com.cn/api/v1/cloud',
  isolation_mode: 'shared_upstream',
  has_credentials: true,
  credential_hint: 'pat_...9876',
  builtin: false,
  last_health_status: 'unknown',
};

function renderAdmin(ui: React.ReactNode) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const client = new PlatformAdminClient(() => undefined);
  client.setCSRFToken('csrf-test');
  return render(
    <QueryClientProvider client={queryClient}>
      <PlatformAdminContext.Provider
        value={{
          client,
          bootstrap: {
            account: { uuid: 'admin-1', name: 'Operator', email: 'admin@example.com' },
            platform_access: { role: 'super_admin', permissions: ['platform:access'] },
            csrf_token: 'csrf-test',
          },
          recheck: async () => undefined,
        }}
      >
        {ui}
      </PlatformAdminContext.Provider>
    </QueryClientProvider>,
    'en',
  );
}

test('RuntimeProfilesPage lists profiles, tests connection with latency, and creates a qoder_cloud profile', async () => {
  const profiles = [builtinProfile, qoderProfile];
  const calls: Array<{ method: string; url: string; body?: unknown }> = [];

  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    const body = init?.body ? JSON.parse(String(init.body)) : undefined;
    calls.push({ method, url, body });

    if (method === 'GET' && url.endsWith('/runtime-profiles')) {
      return Response.json(profiles);
    }
    if (method === 'POST' && url.endsWith('/runtime-profiles/rprof_qoder_cn/test')) {
      return Response.json({
        status: 'healthy',
        latency_ms: 42,
        message: 'ok',
        checked_at: '2026-11-25T01:00:00Z',
      });
    }
    if (method === 'POST' && url.endsWith('/runtime-profiles')) {
      const created: RuntimeProfileRecord = {
        ...qoderProfile,
        uuid: '00000000-0000-4000-8000-000000000100',
        id: 'rprof_qoder_2',
        name: body.name,
        endpoint_url: body.endpoint_url,
      };
      profiles.push(created);
      return Response.json(created, { status: 201 });
    }
    return Response.json({});
  }) as typeof fetch;

  renderAdmin(<RuntimeProfilesPage />);

  expect(await screen.findByText('rprof_oma_native_default')).toBeTruthy();
  expect(screen.getAllByText('OMA Native').length).toBeGreaterThanOrEqual(1);
  expect(screen.getByText('Qoder Cloud CN')).toBeTruthy();
  expect(screen.getByText('pat_...9876')).toBeTruthy();

  const testButtons = screen.getAllByRole('button', { name: 'Test connection' });
  fireEvent.click(testButtons[1]);
  expect(await screen.findByText('42 ms')).toBeTruthy();

  fireEvent.click(screen.getByRole('button', { name: 'Add runtime profile' }));
  expect((screen.getByLabelText('Endpoint URL') as HTMLInputElement).value).toBe(
    'https://api.qoder.com.cn/api/v1/cloud',
  );
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Qoder Staging' } });
  fireEvent.change(screen.getByLabelText(/Access token/), { target: { value: 'qdr_secret_token_1234' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));

  await waitFor(() =>
    expect(calls.some((call) => call.method === 'POST' && call.url.endsWith('/runtime-profiles'))).toBe(true),
  );
  const createCall = calls.find((call) => call.method === 'POST' && call.url.endsWith('/runtime-profiles'));
  expect(createCall?.body).toEqual({
    name: 'Qoder Staging',
    provider_kind: 'qoder_cloud',
    endpoint_url: 'https://api.qoder.com.cn/api/v1/cloud',
    isolation_mode: 'shared_upstream',
    status: 'active',
    credentials: { api_key: 'qdr_secret_token_1234' },
  });
});

test('OrganizationRuntimePolicySection updates allowed profiles while locking built-in oma_native', async () => {
  let savedPayload: unknown = null;
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    if (method === 'GET' && url.endsWith('/organizations/org-1/runtime-policy')) {
      return Response.json({
        organization_uuid: 'org-1',
        default_runtime_profile_uuid: builtinProfile.uuid,
        allowed_runtime_profile_uuids: [builtinProfile.uuid],
        profiles: [builtinProfile, qoderProfile],
      });
    }
    if (method === 'PUT' && url.endsWith('/organizations/org-1/runtime-policy')) {
      savedPayload = JSON.parse(String(init?.body));
      return Response.json({
        organization_uuid: 'org-1',
        ...(savedPayload as object),
        profiles: [builtinProfile, qoderProfile],
      });
    }
    return Response.json({});
  }) as typeof fetch;

  renderAdmin(<OrganizationRuntimePolicySection organizationUuid="org-1" />);

  expect(await screen.findByText('Runtime authorization')).toBeTruthy();
  const checkboxes = screen.getAllByRole('checkbox');
  expect(checkboxes[0].getAttribute('aria-disabled')).toBe('true');

  fireEvent.click(checkboxes[1]);
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));

  await waitFor(() => expect(savedPayload).not.toBeNull());
  expect(savedPayload).toEqual({
    default_runtime_profile_uuid: builtinProfile.uuid,
    allowed_runtime_profile_uuids: [builtinProfile.uuid, qoderProfile.uuid],
  });
  expect(await screen.findByText('Organization runtime policy saved.')).toBeTruthy();
});
