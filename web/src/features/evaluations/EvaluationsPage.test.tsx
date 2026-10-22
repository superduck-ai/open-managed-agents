import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  RouterContextProvider,
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router';
import { afterEach, expect, mock, test } from 'bun:test';
import type { ReactNode } from 'react';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { DatasetCasesPage } from './DatasetCasesPage';
import { EvaluationsPage } from './EvaluationsPage';
import { EvaluatorsPage } from './EvaluatorsPage';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('lists datasets and creates one', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations');
  const requests: Array<{ url: string; method: string; body: string; csrf: string }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    const headers = new Headers(init?.headers);
    requests.push({
      url,
      method,
      body: typeof init?.body === 'string' ? init.body : '',
      csrf: headers.get('X-CSRF-Token') ?? '',
    });
    if (method === 'POST') {
      return Response.json(dataset({ id: 'evalds_created', name: 'billing', desc: 'nightly' }));
    }
    return Response.json({ data: [dataset()], next_page: null });
  });

  renderPage(<EvaluationsPage />);
  expect(await screen.findByRole('link', { name: 'billing' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Create dataset' }));
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'billing' } });
  fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'nightly' } });
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create dataset' }));

  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const created = requests.find((request) => request.method === 'POST');
  expect(created?.url).toContain('/api/console/organizations/org_test/workspaces/default/evaluation-datasets');
  expect(created?.csrf).toBe('csrf_test');
  expect(JSON.parse(created?.body ?? '{}')).toEqual({ name: 'billing', desc: 'nightly' });
  expect(screen.getByRole('link', { name: 'billing' }).getAttribute('href')).toBe(
    '/workspaces/default/evaluations/datasets/evalds_test',
  );
});

test('lists cases and imports JSONL', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/datasets/evalds_test');
  const requests: Array<{ url: string; method: string; body: string; contentType: string }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    const headers = new Headers(init?.headers);
    requests.push({
      url,
      method,
      body: typeof init?.body === 'string' ? init.body : '',
      contentType: headers.get('Content-Type') ?? '',
    });
    if (url.endsWith('/cases:import')) {
      return Response.json({ data: [], next_page: null });
    }
    if (url.endsWith('/cases')) {
      return Response.json({
        data: [
          {
            id: 'evalcase_test',
            type: 'evaluation_case',
            dataset_id: 'evalds_test',
            version_id: 'edv_test',
            position: 1,
            name: 'greeting',
            desc: '',
            turns: ['hello'],
            checkpoints: null,
            input_files: null,
            reference_files: null,
            created_at: '2026-10-22T00:00:00Z',
            updated_at: '2026-10-22T00:00:00Z',
          },
        ],
        next_page: null,
      });
    }
    return Response.json(dataset({ desc: 'nightly' }));
  });

  renderPage(<DatasetCasesPage />);
  expect(await screen.findByText('hello')).toBeTruthy();
  expect(screen.getByText('Version 1')).toBeTruthy();
  expect(screen.getByText('nightly')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('One JSON object per line, with name and turns.'), {
    target: { value: '{"name":"second","turns":["hi"]}\n' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Import' }));

  await waitFor(() => expect(requests.some((request) => request.url.endsWith('/cases:import'))).toBe(true));
  const imported = requests.find((request) => request.url.endsWith('/cases:import'));
  expect(imported?.method).toBe('POST');
  expect(imported?.contentType).toContain('application/x-ndjson');
  expect(imported?.body).toBe('{"name":"second","turns":["hi"]}\n');
  expect(imported?.url).toContain('/evaluation-datasets/evalds_test/cases:import');
});

test('lists evaluators and creates an llm evaluator', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/evaluators');
  const requests: Array<{ url: string; method: string; body: string; csrf: string }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    const headers = new Headers(init?.headers);
    requests.push({
      url,
      method,
      body: typeof init?.body === 'string' ? init.body : '',
      csrf: headers.get('X-CSRF-Token') ?? '',
    });
    if (method === 'POST') {
      return Response.json(evaluator());
    }
    return Response.json({ data: [evaluator()], next_page: null });
  });

  renderPage(<EvaluatorsPage />);
  expect(await screen.findByText('judge')).toBeTruthy();
  expect(screen.getByRole('link', { name: 'Datasets' }).getAttribute('href')).toBe('/workspaces/default/evaluations');
  fireEvent.click(screen.getByRole('button', { name: 'Create evaluator' }));
  const dialog = screen.getByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'judge' } });
  fireEvent.change(within(dialog).getByLabelText('Model'), { target: { value: 'claude' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'input' }));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create evaluator' }));

  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const created = requests.find((request) => request.method === 'POST');
  expect(created?.url).toContain('/evaluation-evaluators');
  expect(created?.csrf).toBe('csrf_test');
  expect(JSON.parse(created?.body ?? '{}')).toEqual({
    name: 'judge',
    desc: '',
    evaluator_type: 'llm',
    model: 'claude',
    prompt_template: '{{input}}',
    variables: ['input'],
  });
});

function evaluator() {
  return {
    id: 'evalev_test',
    type: 'evaluation_evaluator',
    name: 'judge',
    desc: '',
    status: 'available',
    version: {
      id: 'eev_test',
      version: 1,
      evaluator_type: 'llm',
      model: 'claude',
      prompt_template: '{{input}}',
      variables: ['input'],
    },
    created_at: '2026-10-22T00:00:00Z',
    updated_at: '2026-10-22T00:00:00Z',
  };
}

function dataset(overrides: Record<string, unknown> = {}) {
  return {
    id: 'evalds_test',
    type: 'evaluation_dataset',
    name: 'billing',
    desc: 'nightly',
    status: 'available',
    version: { id: 'edv_test', version: 1, case_count: 1 },
    created_at: '2026-10-22T00:00:00Z',
    updated_at: '2026-10-22T00:00:00Z',
    ...overrides,
  };
}

function renderPage(children: ReactNode) {
  const rootRoute = createRootRoute();
  const pageRoute = createRoute({ getParentRoute: () => rootRoute, path: '$' });
  const router = createRouter({
    history: createBrowserHistory({ window }),
    routeTree: rootRoute.addChildren([pageRoute]),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
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
  const auth: AuthContextValue = {
    account: null,
    status: 'authenticated',
    csrfToken: 'csrf_test',
    refresh: async () => undefined,
    logout: async () => undefined,
  };
  return render(
    <RouterContextProvider router={router}>
      <I18nProvider initialLocale="en">
        <AuthContext.Provider value={auth}>
          <WorkspaceContext.Provider value={workspace}>
            <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
          </WorkspaceContext.Provider>
        </AuthContext.Provider>
      </I18nProvider>
    </RouterContextProvider>,
  );
}
