import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import type { ReactNode } from 'react';
import type { EnvironmentApiResponse } from '../types';
const { act, render, fireEvent, screen, cleanup, waitFor } = await import('@testing-library/react');
const { RouterContextProvider, createRootRoute, createRouter, createBrowserHistory } =
  await import('@tanstack/react-router');
const { QueryClient, QueryClientProvider, onlineManager } = await import('@tanstack/react-query');
const { I18nProvider } = await import('../../../shared/i18n');
const { setConsoleRequestContext } = await import('../../../shared/api/client');
const { EnvironmentForm } = await import('./form');
const { EnvironmentsPage } = await import('./EnvironmentsPage');
const { EnvironmentList } = await import('./list');
const { EnvironmentPrebuildLogs } = await import('./prebuild-logs');
const originalFetch = globalThis.fetch;
const clients: InstanceType<typeof QueryClient>[] = [];
const histories: ReturnType<typeof createBrowserHistory>[] = [];
const entity: EnvironmentApiResponse = {
  id: 'env_form',
  name: 'Tools',
  description: '',
  type: 'environment',
  state: 'active',
  scope: 'organization',
  created_at: '2026-09-20T00:00:00Z',
  updated_at: '2026-09-20T00:00:00Z',
  archived_at: null,
  metadata: {},
  config: { type: 'cloud', packages: {}, networking: { type: 'unrestricted' } },
};
function mount(children: ReactNode, path = '/workspaces/default/environments') {
  resetTestDom(`https://oma.duck.ai${path}`);
  setConsoleRequestContext({ workspaceId: 'default', organizationUuid: 'org_test', csrfToken: 'test-csrf' });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const history = createBrowserHistory({ window });
  histories.push(history);
  const router = createRouter({ history, routeTree: createRootRoute() });
  return render(
    <QueryClientProvider client={client}>
      <RouterContextProvider router={router}>
        <I18nProvider initialLocale="en">{children}</I18nProvider>
      </RouterContextProvider>
    </QueryClientProvider>,
  );
}
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  histories.splice(0).forEach((history) => history.destroy());
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

test('creates only cloud environments through the authenticated API', async () => {
  const saved = mock();
  const requests: { body: Record<string, unknown>; headers: Headers }[] = [];
  globalThis.fetch = mock(async (_input: RequestInfo | URL, init?: RequestInit) => {
    requests.push({ body: JSON.parse(String(init?.body)), headers: new Headers(init?.headers) });
    return new Response(JSON.stringify(entity), { headers: { 'content-type': 'application/json' } });
  }) as typeof fetch;
  mount(<EnvironmentForm workspaceId="default" onSaved={saved} onCancel={() => {}} />);
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Local tools' } });
  expect(screen.queryByRole('radio', { name: 'Self-hosted' })).toBeNull();
  expect(screen.getByText('Cloud', { exact: true })).toBeTruthy();
  expect(screen.getByRole('radio', { name: 'Limited' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Create environment' }));
  await waitFor(() => expect(saved).toHaveBeenCalledTimes(1));
  expect(requests[0].body).toMatchObject({ name: 'Local tools', config: { type: 'cloud' } });
  expect(requests[0].headers.get('x-csrf-token')).toBe('test-csrf');
  expect(requests[0].headers.get('x-workspace-id')).toBe('default');
});

test('shows save only after edits, discards locally, and retains changes on API failure', async () => {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify({ error: { message: 'environment name already exists' } }), {
        status: 409,
        headers: { 'content-type': 'application/json' },
      }),
  ) as typeof fetch;
  mount(<EnvironmentForm entity={entity} workspaceId="default" onSaved={() => {}} onCancel={() => {}} />);
  expect(screen.queryByRole('button', { name: 'Save', exact: true })).toBeNull();
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Renamed' } });
  fireEvent.click(screen.getByRole('button', { name: 'Discard', exact: true }));
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Tools');
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Conflict' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  expect(await screen.findByText('An environment with this name already exists.')).toBeTruthy();
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Conflict');
});

test('background detail refresh preserves a draft and saving establishes the next editable baseline', async () => {
  let current = entity;
  let reads = 0;
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(input instanceof Request ? input.url : String(input), window.location.origin);
    if (url.pathname.endsWith('/work')) return Response.json({ data: [], has_more: false });
    if (init?.method === 'POST') {
      current = { ...current, ...JSON.parse(String(init.body)), updated_at: '2026-09-22T00:02:00Z' };
    } else {
      reads++;
    }
    return Response.json(current);
  }) as typeof fetch;
  mount(<EnvironmentsPage />, '/workspaces/default/environments/env_form');
  await screen.findByLabelText('Name');
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Unsaved draft' } });

  current = { ...current, updated_at: '2026-09-22T00:01:00Z' };
  await act(async () => {
    onlineManager.setOnline(false);
    onlineManager.setOnline(true);
  });
  await waitFor(() => expect(reads).toBe(2));
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Unsaved draft');
  expect(screen.queryByText('Discard unsaved changes?')).toBeNull();

  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Save', exact: true })).toBeNull());
  await waitFor(() => expect(reads).toBe(3));
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Unsaved draft');
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Next draft' } });
  expect(screen.getByRole('button', { name: 'Save', exact: true })).toBeTruthy();
  current = { ...current, name: 'Server update', updated_at: '2026-09-22T00:03:00Z' };
  await act(async () => {
    onlineManager.setOnline(false);
    onlineManager.setOnline(true);
  });
  await waitFor(() => expect(reads).toBe(4));
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Next draft');
  fireEvent.click(screen.getByRole('button', { name: 'Discard', exact: true }));
  await waitFor(() => expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Server update'));
});

test('archived environments cannot be edited', () => {
  mount(
    <EnvironmentForm
      entity={{ ...entity, state: 'archived', archived_at: entity.updated_at }}
      workspaceId="default"
      onSaved={() => {}}
      onCancel={() => {}}
    />,
  );
  expect((screen.getByLabelText('Name') as HTMLInputElement).readOnly).toBe(true);
  expect(screen.getByRole('radio', { name: 'Limited' }).getAttribute('aria-disabled')).toBe('true');
  expect(screen.queryByRole('button', { name: 'Add metadata' })).toBeNull();
});

test('selection and name links do not open preview; clicking the row does', async () => {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify({ data: [entity], next_page: null }), {
        headers: { 'content-type': 'application/json' },
      }),
  ) as typeof fetch;
  const preview = mock();
  const action = mock();
  mount(
    <EnvironmentList
      workspaceId="default"
      listHref="/workspaces/default/environments"
      onPreview={preview}
      onAction={action}
    />,
  );
  const link = await screen.findByRole('link', { name: 'Tools' });
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select Tools' }));
  expect(preview).not.toHaveBeenCalled();
  expect(screen.getByText('1 selected')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Archive', exact: true }));
  expect(action.mock.calls[0][0].entities.map((value: EnvironmentApiResponse) => value.id)).toEqual(['env_form']);
  fireEvent.click(link);
  expect(preview).not.toHaveBeenCalled();
  expect(window.location.pathname).toBe('/workspaces/default/environments/env_form');
  fireEvent.click(link.closest('tr')!);
  expect(preview).toHaveBeenCalledWith('env_form', ['env_form']);
  expect(link.getAttribute('href')).toBe('/workspaces/default/environments/env_form');
});

test('package edits replace saved build status without affecting unrelated form edits', async () => {
  globalThis.fetch = mock(async () =>
    Response.json({ build: { state: 'ready', stage: 'template', can_start: false, can_cancel: false } }),
  ) as typeof fetch;
  mount(
    <EnvironmentForm
      entity={{ ...entity, config: { type: 'cloud', packages: { npm: ['is-number@7.0.0'] } } }}
      workspaceId="default"
      onSaved={() => {}}
      onCancel={() => {}}
    />,
  );
  expect(await screen.findByRole('button', { name: 'Preinstalled' })).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Renamed' } });
  expect(screen.getByRole('button', { name: 'Preinstalled' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Remove is-number@7.0.0' }));
  expect(screen.queryByRole('button', { name: 'Preinstalled' })).toBeNull();
  expect(screen.getByText('Unsaved', { exact: true })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Discard', exact: true }));
  expect(await screen.findByRole('button', { name: 'Preinstalled' })).toBeTruthy();
});

test('packages support the Add button and Enter while preserving version syntax and IME input', async () => {
  mount(<EnvironmentForm workspaceId="default" onSaved={() => {}} onCancel={() => {}} />);
  fireEvent.click(screen.getByRole('button', { name: 'Add Python pip' }));
  fireEvent.click(screen.getByRole('button', { name: 'Add Python packages' }));
  const input = await screen.findByLabelText('Python packages to add');
  const add = screen.getByRole('button', { name: 'Add', exact: true });
  expect((add as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(input, { target: { value: 'pyfiglet==1.0.4' } });
  fireEvent.click(add);
  expect(screen.getByRole('button', { name: 'Remove pyfiglet==1.0.4' })).toBeTruthy();

  fireEvent.click(screen.getByRole('button', { name: 'Add Python packages' }));
  const nextInput = await screen.findByLabelText('Python packages to add');
  fireEvent.change(nextInput, { target: { value: 'pandas==2.2.0' } });
  fireEvent.keyDown(nextInput, { key: 'Enter', isComposing: true });
  expect(screen.queryByRole('button', { name: 'Remove pandas==2.2.0' })).toBeNull();
  fireEvent.keyDown(nextInput, { key: 'Enter' });
  expect(screen.getByRole('button', { name: 'Remove pandas==2.2.0' })).toBeTruthy();
});

test('opening logs drains pages, strips split ANSI colors, and preserves literal text', async () => {
  const requests: URL[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    requests.push(url);
    if (url.pathname.endsWith('/logs'))
      return Response.json(
        url.searchParams.get('cursor') === 'next'
          ? { text: '196m\nfinished pyfiglet==1.0.4\u001b[39m', next_cursor: '', complete: true }
          : { text: '<script>literal log</script>\u001b[38;5;', next_cursor: 'next', complete: false },
      );
    return Response.json({
      build: {
        job_id: '9007199254740993',
        state: 'running',
        stage: 'image',
        can_start: false,
        can_cancel: false,
        image_logs: true,
        template_logs: false,
      },
    });
  }) as typeof fetch;
  mount(
    <EnvironmentForm
      entity={{ ...entity, config: { type: 'cloud', packages: { npm: ['is-number@7.0.0'] } } }}
      workspaceId="default"
      onSaved={() => {}}
      onCancel={() => {}}
    />,
  );
  await screen.findByRole('button', { name: 'Preparing' });
  expect(requests.some((url) => url.pathname.endsWith('/logs'))).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: 'Preparing' }));
  expect(screen.queryByRole('button', { name: 'Cancel', exact: true })).toBeNull();
  expect((await screen.findByText(/finished/)).textContent).toBe(
    '<script>literal log</script>\nfinished pyfiglet==1.0.4',
  );
  const logs = requests.filter((url) => url.pathname.endsWith('/logs'));
  expect(logs.map((url) => url.searchParams.get('cursor'))).toEqual(['', 'next']);
  for (const url of logs) {
    expect(url.searchParams.get('stage')).toBe('image');
    expect(url.searchParams.get('job_id')).toBe('9007199254740993');
  }
  expect(document.querySelector('script')).toBeNull();
});

test('logs wait at the live tail and automatically read the final failure', async () => {
  const cursors: string[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    cursors.push(new URL(String(input), 'https://oma.duck.ai').searchParams.get('cursor') || '');
    return Response.json(
      cursors.length === 1
        ? { text: 'Starting build\n', next_cursor: 'tail', complete: false }
        : cursors.length === 2
          ? { text: '', next_cursor: 'tail', complete: false }
          : { text: 'ERROR: Invalid requirement', next_cursor: '', complete: true },
    );
  }) as typeof fetch;
  mount(<EnvironmentPrebuildLogs environmentId="env_form" workspaceId="default" jobId="42" stage="image" />);
  await waitFor(() => expect(cursors).toEqual(['', 'tail']));
  await new Promise((resolve) => setTimeout(resolve, 100));
  expect(cursors).toEqual(['', 'tail']);
  expect(await screen.findByText(/ERROR: Invalid requirement/, {}, { timeout: 4000 })).toBeTruthy();
  expect(cursors).toEqual(['', 'tail', 'tail']);
  expect(screen.queryByRole('button')).toBeNull();
});

test('a log download failure retains output and retries the same cursor', async () => {
  const cursors: string[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    cursors.push(new URL(String(input), 'https://oma.duck.ai').searchParams.get('cursor') || '');
    if (cursors.length === 2) return Response.json({ error: { message: 'unavailable' } }, { status: 502 });
    return Response.json(
      cursors.length === 1
        ? { text: 'Starting build\n', next_cursor: 'tail', complete: false }
        : { text: 'Final failure log', next_cursor: '', complete: true },
    );
  }) as typeof fetch;
  mount(<EnvironmentPrebuildLogs environmentId="env_form" workspaceId="default" jobId="42" stage="image" />);
  expect(await screen.findByRole('alert')).toBeTruthy();
  expect(screen.getByText('Starting build')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  expect(await screen.findByText(/Final failure log/)).toBeTruthy();
  expect(cursors).toEqual(['', 'tail', 'tail']);
});

test('failed cancel refreshes the job ID before the next cancellation', async () => {
  let jobId = '9007199254740993';
  const cancellations: unknown[] = [];
  globalThis.fetch = mock(async (_input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === 'POST') {
      cancellations.push(JSON.parse(String(init.body)));
      jobId = '9007199254740994';
      if (cancellations.length === 1) return Response.json({ error: { message: 'stale job' } }, { status: 409 });
      return new Response(null, { status: 202 });
    }
    return Response.json({
      build: { job_id: jobId, state: 'running', stage: 'image', can_start: false, can_cancel: true },
    });
  }) as typeof fetch;
  mount(
    <EnvironmentForm
      entity={{ ...entity, config: { type: 'cloud', packages: { npm: ['is-number@7.0.0'] } } }}
      workspaceId="default"
      onSaved={() => {}}
      onCancel={() => {}}
    />,
  );
  fireEvent.click(await screen.findByRole('button', { name: 'Preparing' }));
  fireEvent.click(screen.getByRole('button', { name: 'Cancel', exact: true }));
  expect(await screen.findByText('Could not complete the request. Try again.')).toBeTruthy();
  await waitFor(() =>
    expect((screen.getByRole('button', { name: 'Cancel', exact: true }) as HTMLButtonElement).disabled).toBe(false),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Cancel', exact: true }));
  await waitFor(() => expect(cancellations).toEqual([{ job_id: '9007199254740993' }, { job_id: '9007199254740994' }]));
  await waitFor(() => expect(screen.queryByText('Could not complete the request. Try again.')).toBeNull());
});

test('template failures show the actual error even without template logs', async () => {
  const message = 'MANIFEST_UNKNOWN: the previous image no longer exists';
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    if (String(input).includes('/logs')) return Response.json({ text: 'image build logs', complete: true });
    return Response.json({
      build: {
        job_id: '42',
        stage: 'template',
        state: 'failed',
        message,
        can_start: true,
        image_logs: true,
        template_logs: false,
      },
    });
  }) as typeof fetch;
  mount(
    <EnvironmentForm
      entity={{ ...entity, config: { type: 'cloud', packages: { npm: ['is-number'] } } }}
      workspaceId="default"
      onSaved={() => {}}
      onCancel={() => {}}
    />,
  );
  fireEvent.click(await screen.findByRole('button', { name: 'Preparation failed' }));
  expect((await screen.findByText(message)).tagName).toBe('PRE');
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.getByRole('button', { name: /Build template/ }).getAttribute('aria-pressed')).toBe('true');
  fireEvent.click(screen.getByRole('button', { name: /Build image/ }));
  expect(await screen.findByText('image build logs')).toBeTruthy();
  expect(screen.queryByText(message)).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: /Build template/ }));
  expect(screen.getByText(message).tagName).toBe('PRE');
});
