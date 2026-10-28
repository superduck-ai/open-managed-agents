import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, test } from 'bun:test';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { setConsoleRequestContext } from '../../shared/api/client';
import { I18nProvider } from '../../shared/i18n';
import { Toaster, toast } from '../../shared/ui/sonner';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { TelemetryDestinationsPage } from './TelemetryDestinationsPage';

const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  toast.dismiss();
  globalThis.fetch = originalFetch;
});

type Recorded = {
  url: string;
  method: string;
  beta: string | null;
  csrf: string | null;
  body?: Record<string, unknown>;
};

const stored = {
  id: 'tdst_1',
  type: 'telemetry_destination',
  display_name: 'Honeycomb',
  endpoint_url: 'https://api.example.com',
  header_names: ['x-honeycomb-team'],
  state: { type: 'disabled', reason: 'sustained_failure' },
  traces: {
    enabled: true,
    content_mode: 'metadata_only',
    endpoint_url: 'https://api.example.com/v1/traces',
    max_content_bytes: 786432,
  },
  workspaces: { type: 'all', workspace_ids: [] },
  delivery_health: {
    last_error: 'destination returned status 503',
    last_error_at: '2026-10-26T10:00:00Z',
    last_success_at: null,
  },
  created_at: '2026-10-26T00:00:00Z',
  updated_at: '2026-10-26T00:00:00Z',
};

type Override = (url: string, method: string) => Response | undefined;

function mockApi(destination: typeof stored | (typeof stored)[] = stored, override?: Override) {
  const requests: Recorded[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const headers = new Headers(init?.headers);
    const method = init?.method ?? 'GET';
    requests.push({
      url: String(input),
      method,
      beta: headers.get('anthropic-beta'),
      csrf: headers.get('X-CSRF-Token'),
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    });
    const overridden = override?.(String(input), method);
    if (overridden) return overridden;
    if (String(input).endsWith('/test'))
      return Response.json({ type: 'telemetry_destination_test_result', success: true, status_code: 200, error: null });
    if (method === 'GET') return Response.json({ data: [destination].flat(), next_page: null });
    return Response.json({ ...stored, id: 'tdst_new' });
  }) as typeof fetch;
  return requests;
}

function renderPage(role: string, workspaceId?: string, locale: 'en' | 'zh-CN' = 'en') {
  resetTestDom('https://oma.duck.ai/settings/telemetry');
  setConsoleRequestContext({ organizationUuid: 'org_test', csrfToken: 'csrf_test' });
  const auth: AuthContextValue = {
    account: {
      uuid: 'acct',
      email_address: 'a@example.com',
      memberships: [{ role, organization: { uuid: 'org_test' } }],
    },
    status: 'authenticated',
    csrfToken: 'csrf_test',
    refresh: async () => ({ account: null }),
    logout: async () => undefined,
  };
  const workspace: WorkspaceContextValue = {
    orgUuid: 'org_test',
    workspaces: [defaultWorkspace],
    activeWorkspace: defaultWorkspace,
    activeWorkspaceId: defaultWorkspace.id,
    isLoading: false,
    error: null,
    selectWorkspace: () => undefined,
    createWorkspace: async () => defaultWorkspace,
    refreshWorkspaces: async () => undefined,
  };
  return render(
    <I18nProvider initialLocale={locale}>
      <AuthContext.Provider value={auth}>
        <WorkspaceContext.Provider value={workspace}>
          <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
            <Toaster />
            <TelemetryDestinationsPage workspaceId={workspaceId} />
          </QueryClientProvider>
        </WorkspaceContext.Provider>
      </AuthContext.Provider>
    </I18nProvider>,
  );
}

test('non-administrators can read destinations but not change them', async () => {
  const requests = mockApi();
  renderPage('developer');
  expect(await screen.findByText('Honeycomb')).toBeTruthy();
  expect(screen.getByText('Disabled after 24h of failures')).toBeTruthy();
  expect(screen.getByText('The destination returned HTTP 503')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Add destination' })).toBeNull();
  expect((screen.getByRole('button', { name: 'Send test span' }) as HTMLButtonElement).disabled).toBe(true);
  expect(requests[0]).toMatchObject({
    url: '/v1/organizations/telemetry_destinations?limit=100',
    beta: 'telemetry-destinations-2026-08-11',
  });
});

test('administrators create, test and re-enable destinations through the beta API', async () => {
  const requests = mockApi();
  renderPage('admin');
  fireEvent.click(await screen.findByRole('button', { name: 'Add destination' }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('OTLP endpoint URL'), {
    target: { value: 'https://otlp.example.com' },
  });
  fireEvent.change(within(dialog).getByLabelText('Header name'), { target: { value: 'Authorization' } });
  fireEvent.change(within(dialog).getByLabelText('Header value'), { target: { value: 'Bearer secret' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add destination' }));
  await waitFor(() =>
    expect(
      requests.some(
        (request) => request.method === 'POST' && request.url === '/v1/organizations/telemetry_destinations',
      ),
    ).toBe(true),
  );
  const create = requests.find((request) => request.method === 'POST')!;
  expect(create.body).toMatchObject({
    endpoint_url: 'https://otlp.example.com',
    headers: { Authorization: 'Bearer secret' },
  });
  expect(create.csrf).toBe('csrf_test');
  fireEvent.click(screen.getByRole('button', { name: 'Send test span' }));
  expect(await screen.findByText('Test span delivered')).toBeTruthy();
  fireEvent.click(screen.getByRole('switch', { name: 'Enable destination' }));
  await waitFor(() =>
    expect(
      requests.some(
        (request) => request.url === '/v1/organizations/telemetry_destinations/tdst_1' && request.body?.state,
      ),
    ).toBe(true),
  );
  expect(requests.find((request) => request.url === '/v1/organizations/telemetry_destinations/tdst_1')!.body).toEqual({
    state: { type: 'enabled' },
  });
});

test('workspace settings list only destinations covering the workspace and create them for it', async () => {
  const requests = mockApi();
  renderPage('admin', 'wrkspc_scoped');
  expect(await screen.findByText('Honeycomb')).toBeTruthy();
  expect(requests[0].url).toBe('/v1/organizations/telemetry_destinations?limit=100&workspace_ids[]=wrkspc_scoped');
  fireEvent.click(screen.getByRole('button', { name: 'Add destination' }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('OTLP endpoint URL'), {
    target: { value: 'https://otlp.example.com' },
  });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add destination' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  expect(requests.find((request) => request.method === 'POST')!.body).toMatchObject({
    workspaces: { type: 'include', workspace_ids: ['wrkspc_scoped'] },
  });
});

test('delivery health compares timestamps by time rather than text', async () => {
  mockApi({
    ...stored,
    state: { type: 'enabled' },
    delivery_health: {
      last_error: 'destination returned status 503',
      last_error_at: '2026-10-26T10:00:00.5Z',
      last_success_at: '2026-10-26T10:00:00Z',
    },
  } as typeof stored);
  renderPage('developer');
  expect(await screen.findByText('The destination returned HTTP 503')).toBeTruthy();
});

const apiError = (status: number, message: string) =>
  Response.json({ type: 'error', error: { type: 'invalid_request_error', message } }, { status });

async function openCreateDialog(name = 'Add destination') {
  fireEvent.click(await screen.findByRole('button', { name }));
  return screen.findByRole('dialog');
}

test('advanced settings bound the content per span and send it in bytes', async () => {
  const requests = mockApi();
  renderPage('admin');
  const dialog = await openCreateDialog();
  fireEvent.change(within(dialog).getByLabelText('OTLP endpoint URL'), {
    target: { value: 'https://otlp.example.com' },
  });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Advanced settings' }));
  const maxContent = await within(dialog).findByLabelText(/Maximum content per span/);
  const submit = within(dialog).getByRole('button', { name: 'Add destination' }) as HTMLButtonElement;
  fireEvent.change(maxContent, { target: { value: '0.8' } });
  expect(within(dialog).getByText('Enter a size greater than 0 and at most 0.75 MB.')).toBeTruthy();
  expect(maxContent.getAttribute('aria-invalid')).toBe('true');
  expect(submit.disabled).toBe(true);
  fireEvent.change(maxContent, { target: { value: '0.5' } });
  expect(within(dialog).queryByText('Enter a size greater than 0 and at most 0.75 MB.')).toBeNull();
  fireEvent.click(submit);
  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  expect(requests.find((request) => request.method === 'POST')!.body).toMatchObject({
    traces: { content_mode: 'metadata_only', max_content_bytes: 524288 },
  });
});

test('an invalid endpoint URL explains why the destination cannot be added', async () => {
  mockApi();
  renderPage('admin');
  const dialog = await openCreateDialog();
  const endpoint = within(dialog).getByLabelText('OTLP endpoint URL');
  for (const value of ['not-a-url', 'ftp://example.com']) {
    fireEvent.change(endpoint, { target: { value } });
    expect(
      within(dialog).getByText('Enter a full URL starting with https://, for example https://otlp.example.com.'),
    ).toBeTruthy();
    expect(endpoint.getAttribute('aria-invalid')).toBe('true');
  }
  fireEvent.change(endpoint, { target: { value: 'https://otlp.example.com' } });
  expect(endpoint.getAttribute('aria-invalid')).toBeNull();
});

test('the organization page explains the destination limit', async () => {
  mockApi(Array.from({ length: 10 }, (_, index) => ({ ...stored, id: `tdst_${index}` })));
  renderPage('admin');
  expect(
    await screen.findByText(
      'This organization has reached the limit of 10 destinations. Delete one before adding another.',
    ),
  ).toBeTruthy();
  expect((screen.getByRole('button', { name: 'Add destination' }) as HTMLButtonElement).disabled).toBe(true);
});

test('Chinese pages localize API and delivery errors while keeping unmapped details', async () => {
  mockApi(stored, (url, method) => {
    if (url.endsWith('/test')) return apiError(429, 'telemetry destination tests must be at least 10 seconds apart');
    if (method === 'POST' && url.endsWith('/telemetry_destinations'))
      return apiError(400, 'endpoint_url must not include credentials, a query or a fragment');
    return undefined;
  });
  renderPage('admin', undefined, 'zh-CN');
  expect(await screen.findByText('目标返回 HTTP 503')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '发送测试 span' }));
  expect(await screen.findByText('两次发送测试 span 至少需要间隔 10 秒。')).toBeTruthy();
  const dialog = await openCreateDialog('添加目标位置');
  fireEvent.change(within(dialog).getByLabelText('OTLP 端点 URL'), {
    target: { value: 'https://otlp.example.com' },
  });
  fireEvent.click(within(dialog).getByRole('button', { name: '添加目标位置' }));
  expect(
    await within(dialog).findByText('无法保存目标：endpoint_url must not include credentials, a query or a fragment'),
  ).toBeTruthy();
});

test('failed state changes and deletions are reported', async () => {
  mockApi(stored, (url, method) => {
    if (url.endsWith('/tdst_1') && method === 'POST') return apiError(409, 'modified concurrently');
    if (url.endsWith('/tdst_1') && method === 'DELETE') return apiError(500, 'telemetry destination request failed');
    return undefined;
  });
  renderPage('admin');
  fireEvent.click(await screen.findByRole('switch', { name: 'Enable destination' }));
  expect(await screen.findByText('The destination was changed elsewhere. Reload and try again.')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Delete destination' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Delete' }));
  expect(
    await screen.findByText('Could not delete the destination: telemetry destination request failed'),
  ).toBeTruthy();
});
