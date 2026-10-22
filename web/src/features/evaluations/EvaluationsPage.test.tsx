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
import { ExperimentComparePage } from './ExperimentComparePage';
import { ExperimentNewPage } from './ExperimentNewPage';
import { EvaluationsPage } from './EvaluationsPage';
import { EvaluatorsPage } from './EvaluatorsPage';
import { ExperimentsPage } from './ExperimentsPage';

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

test('lists experiments and creates one', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/experiments');
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
      return Response.json(experiment());
    }
    return Response.json({ data: [experiment()], next_page: null });
  });

  renderPage(<ExperimentsPage />);
  expect(await screen.findByText('nightly')).toBeTruthy();
  expect(screen.getByRole('link', { name: 'Evaluators' }).getAttribute('href')).toBe(
    '/workspaces/default/evaluations/evaluators',
  );
  cleanup();
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/experiments/new');
  renderPage(<ExperimentNewPage />);
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'nightly' } });
  fireEvent.change(screen.getByLabelText('Dataset version'), { target: { value: 'edv_test' } });
  fireEvent.change(screen.getByLabelText('Agent'), { target: { value: 'agent_test' } });
  fireEvent.change(screen.getByLabelText('Environment'), { target: { value: 'env_test' } });
  fireEvent.change(screen.getByLabelText('Timeout (seconds)'), { target: { value: '60' } });
  fireEvent.change(screen.getByLabelText('Evaluators'), { target: { value: 'evalev_a, evalev_b' } });
  fireEvent.click(screen.getByRole('button', { name: 'New experiment' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const created = requests.find((request) => request.method === 'POST');
  expect(created?.url).toContain('/evaluation-experiments');
  expect(created?.csrf).toBe('csrf_test');
  expect(JSON.parse(created?.body ?? '{}')).toEqual({
    name: 'nightly',
    desc: '',
    dataset_version_id: 'edv_test',
    agent_id: 'agent_test',
    environment_id: 'env_test',
    timeout_seconds: 60,
    evaluator_ids: ['evalev_a', 'evalev_b'],
  });
});

test('reuses a published dataset preset', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations');
  const requests: Array<{ url: string; method: string; body: string }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    requests.push({ url, method, body: typeof init?.body === 'string' ? init.body : '' });
    if (method === 'POST') {
      return Response.json(dataset({ id: 'evalds_copied', name: 'copied' }));
    }
    if (url.includes('/evaluation-dataset-presets/') && url.includes('/cases')) {
      return Response.json({
        data: [
          {
            id: 'evalcase_oma_demo_03_v2_01',
            type: 'evaluation_case',
            dataset_id: 'evalds_oma_demo_03',
            version_id: 'edv_oma_demo_03_v2',
            position: 1,
            name: 'OMA demo case 01',
            desc: '',
            turns: ['OMA demo case 1 on dataset 3 version 2. Reply with the word ready.'],
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
    if (url.includes('/evaluation-dataset-presets')) {
      return Response.json({
        data: [
          {
            id: 'evalds_oma_demo_03',
            type: 'evaluation_dataset_preset',
            name: 'OMA demo dataset 03',
            desc: 'OMA demo preset. Not a QCA dataset.',
            status: 'available',
            versions: [
              { id: 'edv_oma_demo_03_v1', version: 1, case_count: 1 },
              { id: 'edv_oma_demo_03_v2', version: 2, case_count: 2 },
            ],
            published_at: '2026-10-22T00:00:00Z',
          },
        ],
        next_page: null,
      });
    }
    return Response.json({ data: [dataset()], next_page: null });
  });

  renderPage(<EvaluationsPage />);
  fireEvent.click(await screen.findByRole('tab', { name: 'Presets' }));
  expect(await screen.findByText('OMA demo dataset 03')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Reuse OMA demo dataset 03' }));
  expect(await screen.findByText('OMA demo case 1 on dataset 3 version 2. Reply with the word ready.')).toBeTruthy();
  const dialog = screen.getByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'copied' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Reuse' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const created = requests.find((request) => request.method === 'POST');
  expect(created?.url).toContain('/evaluation-datasets');
  expect(JSON.parse(created?.body ?? '{}')).toEqual({
    name: 'copied',
    desc: '',
    source_version_id: 'edv_oma_demo_03_v2',
  });
  expect(requests.some((request) => request.url.includes('version_id=edv_oma_demo_03_v2'))).toBe(true);
});

test('creates an evaluator from a published preset', async () => {
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
    if (url.includes('/evaluation-evaluator-presets')) {
      return Response.json({
        data: [
          {
            id: 'evalev_oma_demo_01',
            type: 'evaluation_evaluator_preset',
            name: 'OMA demo evaluator 01',
            desc: 'OMA demo preset. Not a QCA evaluator.',
            status: 'available',
            version: {
              id: 'eev_oma_demo_01_v1',
              version: 1,
              evaluator_type: 'llm',
              model: 'oma-demo',
              prompt_template: 'OMA demo scorer. Score the answer from 0 to 1. {{input}}',
              variables: ['input'],
            },
            created_at: '2026-10-22T00:00:00Z',
            updated_at: '2026-10-22T00:00:00Z',
          },
        ],
        next_page: null,
      });
    }
    return Response.json({ data: [evaluator()], next_page: null });
  });

  renderPage(<EvaluatorsPage />);
  fireEvent.click(await screen.findByRole('tab', { name: 'Presets' }));
  expect(await screen.findByText('OMA demo evaluator 01')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Create from preset OMA demo evaluator 01' }));
  const dialog = screen.getByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'copied judge' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create from preset' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const created = requests.find((request) => request.method === 'POST');
  expect(created?.url).toContain('/evaluation-evaluators');
  expect(created?.csrf).toBe('csrf_test');
  expect(JSON.parse(created?.body ?? '{}')).toEqual({
    name: 'copied judge',
    desc: '',
    source_version_id: 'eev_oma_demo_01_v1',
  });
});

test('compare shows per-evaluator scores for completed experiments', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/experiments/compare');
  globalThis.fetch = mock(async () =>
    Response.json({
      data: [
        {
          ...experiment(),
          status: 'completed',
          evaluators: [
            { id: 'evalev_a', version_id: 'eev_a', position: 1 },
            { id: 'evalev_b', version_id: 'eev_b', position: 2 },
            { id: 'evalev_c', version_id: 'eev_c', position: 3 },
          ],
          scores: [
            {
              id: 'evalscore_a1',
              item_id: 'evalitem_1',
              evaluator_id: 'evalev_a',
              version_id: 'eev_a',
              status: 'completed',
              score: 0.5,
            },
            {
              id: 'evalscore_a2',
              item_id: 'evalitem_2',
              evaluator_id: 'evalev_a',
              version_id: 'eev_a',
              status: 'completed',
              score: 1,
            },
            {
              id: 'evalscore_b',
              item_id: 'evalitem_1',
              evaluator_id: 'evalev_b',
              version_id: 'eev_b',
              status: 'failed',
              error: 'Could not parse evaluator score',
            },
          ],
        },
      ],
      next_page: null,
    }),
  );
  renderPage(<ExperimentComparePage />);
  fireEvent.change(screen.getByLabelText('Dataset version'), { target: { value: 'edv_test' } });
  fireEvent.change(screen.getByLabelText('Evaluators'), { target: { value: 'evalev_a, evalev_b, evalev_c' } });
  fireEvent.click(screen.getByRole('button', { name: 'Compare' }));
  expect(await screen.findByRole('columnheader', { name: 'evalev_a' })).toBeTruthy();
  expect(screen.getByRole('columnheader', { name: 'evalev_b' })).toBeTruthy();
  expect(screen.getByRole('columnheader', { name: 'evalev_c' })).toBeTruthy();
  expect(screen.getByRole('cell', { name: '0.5, 1' })).toBeTruthy();
  expect(screen.getByRole('cell', { name: 'Could not parse evaluator score' })).toBeTruthy();
  expect(screen.getByRole('cell', { name: 'Missing' })).toBeTruthy();
});

test('compare shows an empty state when no experiments are completed', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/experiments/compare');
  globalThis.fetch = mock(async () => Response.json({ data: [], next_page: null }));
  renderPage(<ExperimentComparePage />);
  fireEvent.change(screen.getByLabelText('Dataset version'), { target: { value: 'edv_test' } });
  fireEvent.change(screen.getByLabelText('Evaluators'), { target: { value: 'evalev_a' } });
  fireEvent.click(screen.getByRole('button', { name: 'Compare' }));
  expect(await screen.findByText('No completed experiments.')).toBeTruthy();
});

function experiment() {
  return {
    id: 'evalexp_test',
    type: 'evaluation_experiment',
    name: 'nightly',
    desc: '',
    status: 'running',
    dataset_id: 'evalds_test',
    version_id: 'edv_test',
    agent_id: 'agent_test',
    agent_version: 4,
    environment_id: 'env_test',
    timeout_seconds: 60,
    item_count: 1,
    session_count: 1,
    evaluator_count: 1,
    created_at: '2026-10-22T00:00:00Z',
    updated_at: '2026-10-22T00:00:00Z',
  };
}

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
