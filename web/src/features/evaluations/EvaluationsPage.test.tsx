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
import { AddSessionToEvaluationDataset } from './AddSessionToEvaluationDataset';
import { DatasetCasesPage } from './DatasetCasesPage';
import { ExperimentComparePage } from './ExperimentComparePage';
import { ExperimentDetailPage } from './ExperimentDetailPage';
import { ExperimentNewPage } from './ExperimentNewPage';
import { EvaluatorDetailPage } from './EvaluatorDetailPage';
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
    if (url.includes('/cases:import')) {
      return Response.json({ data: [], next_page: null });
    }
    if (method === 'POST') {
      return Response.json(dataset({ id: 'evalds_created', name: 'billing', desc: 'nightly' }));
    }
    return Response.json({ data: [dataset()], next_page: null });
  });

  renderPage(<EvaluationsPage />);
  expect(await screen.findByRole('link', { name: 'billing' })).toBeTruthy();
  expect(screen.getByRole('columnheader', { name: 'Version' })).toBeTruthy();
  expect(screen.getByRole('columnheader', { name: 'Cases' })).toBeTruthy();
  expect(screen.getByRole('cell', { name: 'Available' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Create dataset' }));
  expect(screen.getByRole('heading', { name: 'New dataset' })).toBeTruthy();
  expect(screen.getByText('0/50')).toBeTruthy();
  expect(screen.getByPlaceholderText('What this dataset evaluates')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'billing' } });
  fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'nightly' } });
  expect(screen.getByText('7/50')).toBeTruthy();
  const file = new File(['{"name":"second","turns":["hi"]}\n'], 'cases.jsonl', { type: 'application/jsonl' });
  fireEvent.change(screen.getByLabelText(/Drag or choose a dataset file/), { target: { files: [file] } });
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create dataset' }));

  await waitFor(() => expect(requests.some((request) => request.url.includes('/cases:import'))).toBe(true));
  const created = requests.find((request) => request.method === 'POST' && request.body.startsWith('{'));
  const imported = requests.find((request) => request.url.includes('/cases:import'));
  expect(created?.url).toContain('/api/console/organizations/org_test/workspaces/default/evaluation-datasets');
  expect(created?.csrf).toBe('csrf_test');
  expect(JSON.parse(created?.body ?? '{}')).toEqual({ name: 'billing', desc: 'nightly' });
  expect(imported?.method).toBe('POST');
  expect(imported?.csrf).toBe('csrf_test');
  expect(screen.getByRole('link', { name: 'billing' }).getAttribute('href')).toBe(
    '/workspaces/default/evaluations/datasets/evalds_test',
  );
});

test('explains dataset files and checkpoints in the create dialog', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations');
  globalThis.fetch = mock(async () => Response.json({ data: [], next_page: null }));
  renderPage(<EvaluationsPage />);
  fireEvent.click(screen.getAllByRole('button', { name: 'Create dataset' })[0]);
  expect(screen.getByRole('tab', { name: 'Upload data' })).toBeTruthy();
  expect(screen.getByRole('tab', { name: 'Reuse existing' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Download template' })).toBeTruthy();
  expect(screen.getByText(/ZIP template is a complete example/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'How to create a dataset' }));
  expect(await screen.findByRole('heading', { name: 'File format' })).toBeTruthy();
  expect(screen.getAllByText(/cases\.jsonl/).length).toBeGreaterThan(0);
  expect(screen.getByRole('cell', { name: 'checkpoints' })).toBeTruthy();
  expect(screen.getByText(/Acceptance checks the evaluator/)).toBeTruthy();
  expect(screen.getByRole('cell', { name: 'input_files' })).toBeTruthy();
  expect(screen.getByRole('cell', { name: 'reference_files' })).toBeTruthy();
});

test('creates a dataset from selected cases', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations');
  const requests: Array<{ url: string; method: string; body: string }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    requests.push({ url, method, body: typeof init?.body === 'string' ? init.body : '' });
    if (url.includes('/cases')) {
      return Response.json({
        data: [evaluationCase('evalcase_a', 'alpha', 'one'), evaluationCase('evalcase_b', 'beta', 'two')],
        next_page: null,
      });
    }
    if (url.includes('evaluation-dataset-presets')) {
      return Response.json({
        data: [
          {
            id: 'evalds_oma_demo_03',
            type: 'evaluation_dataset_preset',
            name: 'Demo',
            desc: '',
            status: 'available',
            versions: [{ id: 'edv_oma_demo_03_v2', version: 2, case_count: 2 }],
            published_at: '2026-10-22T00:00:00Z',
          },
        ],
        next_page: null,
      });
    }
    if (method === 'POST') {
      return Response.json(dataset({ id: 'evalds_created', name: 'combined' }));
    }
    return Response.json({ data: [dataset()], next_page: null });
  });

  renderPage(<EvaluationsPage />);
  fireEvent.click(await screen.findByRole('button', { name: 'Create dataset' }));
  fireEvent.click(screen.getByRole('tab', { name: 'Reuse existing' }));
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'combined' } });
  expect(await screen.findByRole('checkbox', { name: /beta/ })).toBeTruthy();
  expect(screen.getByRole('combobox', { name: 'Source dataset' })).toBeTruthy();
  expect(screen.getByRole('combobox', { name: 'Available version' })).toBeTruthy();
  fireEvent.click(screen.getByRole('checkbox', { name: /beta/ }));
  expect(await screen.findByText('Selected 1')).toBeTruthy();
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create dataset' }));

  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const created = requests.find((request) => request.method === 'POST');
  expect(JSON.parse(created?.body ?? '{}')).toEqual({
    name: 'combined',
    desc: '',
    source_dataset_id: 'evalds_test',
    case_ids: ['evalcase_b'],
  });
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
    if (isCaseList(url)) {
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
  expect(screen.getByText('Single turn')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'View greeting' }));
  expect(await screen.findByText('Input files')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Close' }));
  expect(screen.getAllByText('v1').length).toBeGreaterThan(0);
  expect(screen.getByText('nightly')).toBeTruthy();
  await openCaseAction('Import JSONL');
  expect(screen.getAllByText(/acceptance checklist for evaluators/).length).toBeGreaterThan(0);
  fireEvent.change(screen.getByLabelText('One JSON object per line, with name and turns.'), {
    target: { value: '{"name":"second","turns":["hi"]}\n' },
  });
  fireEvent.click(await screen.findByRole('button', { name: 'Import' }));

  await waitFor(() => expect(requests.some((request) => request.url.endsWith('/cases:import'))).toBe(true));
  const imported = requests.find((request) => request.url.endsWith('/cases:import'));
  expect(imported?.method).toBe('POST');
  expect(imported?.contentType).toContain('application/x-ndjson');
  expect(imported?.body).toBe('{"name":"second","turns":["hi"]}\n');
  expect(imported?.url).toContain('/evaluation-datasets/evalds_test/cases:import');
});

test('pages a long dataset case list', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/datasets/evalds_test');
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (isCaseList(url)) {
      const page = new URL(url, 'https://oma.duck.ai').searchParams.get('page');
      const start = page === 'more' ? 20 : 0;
      const count = page === 'more' ? 1 : 20;
      return Response.json({
        data: Array.from({ length: count }, (_, offset) => {
          const index = start + offset;
          return {
            id: `evalcase_${index}`,
            type: 'evaluation_case',
            dataset_id: 'evalds_test',
            version_id: 'edv_test',
            position: index + 1,
            name: `case ${index}`,
            desc: '',
            turns: [`turn ${index}`],
            checkpoints: null,
            input_files: null,
            reference_files: null,
            created_at: '2026-10-22T00:00:00Z',
            updated_at: '2026-10-22T00:00:00Z',
          };
        }),
        next_page: page === 'more' ? null : 'more',
      });
    }
    return Response.json(dataset({ version: { id: 'edv_test', version: 1, case_count: 21 } }));
  });

  renderPage(<DatasetCasesPage />);
  expect(await screen.findByText('turn 0')).toBeTruthy();
  expect(screen.getByText('21 cases')).toBeTruthy();
  expect(screen.queryByText('turn 20')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Page 2' }));
  expect(await screen.findByText('turn 20')).toBeTruthy();
  expect(screen.queryByText('turn 0')).toBeNull();
  expect(screen.getByRole('button', { name: 'Page 2' }).getAttribute('aria-current')).toBe('page');
});

test('edits and deletes a dataset case', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/datasets/evalds_test');
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
    if (isCaseList(url)) {
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
            checkpoints: ['done'],
            input_files: [{ name: 'brief.txt', file_id: 'file_brief' }],
            reference_files: null,
            created_at: '2026-10-22T00:00:00Z',
            updated_at: '2026-10-22T00:00:00Z',
          },
        ],
        next_page: null,
      });
    }
    if (method === 'PATCH') {
      return Response.json({
        id: 'evalcase_test',
        type: 'evaluation_case',
        dataset_id: 'evalds_test',
        version_id: 'edv_test',
        position: 1,
        name: 'edited',
        desc: 'note',
        turns: ['hello again'],
        checkpoints: ['done'],
        input_files: [{ name: 'brief.txt', file_id: 'file_brief' }],
        reference_files: null,
        created_at: '2026-10-22T00:00:00Z',
        updated_at: '2026-10-22T00:00:00Z',
      });
    }
    return Response.json(dataset({ desc: 'nightly' }));
  });

  renderPage(<DatasetCasesPage />);
  fireEvent.click(await screen.findByRole('button', { name: 'Edit version 1' }));
  const drawer = await screen.findByRole('dialog');
  fireEvent.change(within(drawer).getByLabelText('Name'), { target: { value: 'edited' } });
  fireEvent.change(within(drawer).getByLabelText('Turn 1'), { target: { value: 'hello again' } });
  fireEvent.click(within(drawer).getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'PATCH')).toBe(true));
  const saved = requests.find((request) => request.method === 'PATCH');
  expect(saved?.url).toContain('/evaluation-datasets/evalds_test/cases/evalcase_test');
  expect(saved?.csrf).toBe('csrf_test');
  expect(JSON.parse(saved?.body ?? '{}')).toMatchObject({
    name: 'edited',
    turns: ['hello again'],
    checkpoints: ['done'],
    input_files: [{ name: 'brief.txt', file_id: 'file_brief' }],
  });
  fireEvent.click(within(drawer).getByRole('button', { name: 'Delete case' }));
  const confirm = await screen.findByRole('alertdialog');
  fireEvent.click(within(confirm).getByRole('button', { name: 'Delete' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'DELETE')).toBe(true));
  const deleted = requests.find((request) => request.method === 'DELETE');
  expect(deleted?.url).toContain('/cases/evalcase_test');
  expect(deleted?.csrf).toBe('csrf_test');
});

test('imports a dataset case from a session id', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/datasets/evalds_test');
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
      return Response.json({
        id: 'evalcase_from_session',
        type: 'evaluation_case',
        dataset_id: 'evalds_test',
        version_id: 'edv_test',
        position: 2,
        name: 'checkout',
        desc: '',
        turns: ['where is my order'],
        checkpoints: ['ready'],
        input_files: null,
        reference_files: null,
        created_at: '2026-10-22T00:00:00Z',
        updated_at: '2026-10-22T00:00:00Z',
      });
    }
    if (isCaseList(url)) {
      return Response.json({ data: [], next_page: null });
    }
    return Response.json(dataset());
  });

  renderPage(<DatasetCasesPage />);
  await openCaseAction('Import from session');
  fireEvent.change(await screen.findByLabelText('Session'), { target: { value: 'sesn_case' } });
  const checkpoints = document.getElementById('import-session-case-checkpoints');
  if (!checkpoints) {
    throw new Error('missing checkpoints');
  }
  expect(checkpoints.getAttribute('placeholder')).toContain('Produce a PDF report');
  fireEvent.change(checkpoints, { target: { value: '{' } });
  fireEvent.click(screen.getByRole('button', { name: 'Import from session' }));
  expect(
    await screen.findByText(
      'Write one checkpoint per line, or paste a string array such as ["The task finishes without an error"].',
    ),
  ).toBeTruthy();
  expect(requests.some((request) => request.method === 'POST')).toBe(false);
  fireEvent.change(checkpoints, { target: { value: '期望的结果是不会报错，正常结束' } });
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'checkout' } });
  fireEvent.click(screen.getByRole('button', { name: 'Import from session' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const created = requests.find((request) => request.method === 'POST');
  expect(created?.url).toContain('/evaluation-datasets/evalds_test/cases:from-session');
  expect(created?.csrf).toBe('csrf_test');
  expect(JSON.parse(created?.body ?? '{}')).toEqual({
    session_id: 'sesn_case',
    name: 'checkout',
    desc: '',
    checkpoints: ['期望的结果是不会报错，正常结束'],
    include_output_files: false,
  });
});

test('imports several sessions and shows per-id failures', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/datasets/evalds_test');
  const requests: Array<{ url: string; method: string; body: string }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    requests.push({ url, method, body: typeof init?.body === 'string' ? init.body : '' });
    if (url.includes('/cases:from-sessions')) {
      return Response.json({
        created: [{ id: 'evalcase_from_session', type: 'evaluation_case' }],
        failed: [{ session_id: 'sesn_missing', error: 'Session not found' }],
      });
    }
    if (isCaseList(url)) {
      return Response.json({ data: [], next_page: null });
    }
    return Response.json(dataset());
  });

  renderPage(<DatasetCasesPage />);
  await openCaseAction('Import sessions');
  fireEvent.change(await screen.findByLabelText('Session ids'), {
    target: { value: 'sesn_ok, sesn_missing\nsesn_ok' },
  });
  fireEvent.change(screen.getByLabelText('Name prefix'), { target: { value: 'bad-' } });
  const outputFiles = document.querySelector('input[type="checkbox"]') as HTMLInputElement;
  fireEvent.click(outputFiles);
  await waitFor(() => expect(screen.getAllByRole('checkbox')[0].getAttribute('aria-checked')).toBe('true'));
  fireEvent.click(screen.getByRole('button', { name: 'Import sessions' }));
  await waitFor(() => expect(requests.some((request) => request.url.includes('/cases:from-sessions'))).toBe(true));
  const created = requests.find((request) => request.url.includes('/cases:from-sessions'));
  expect(JSON.parse(created?.body ?? '{}')).toEqual({
    session_ids: ['sesn_ok', 'sesn_missing', 'sesn_ok'],
    name_prefix: 'bad-',
    desc: '',
    include_output_files: true,
  });
  expect(await screen.findByText('Added 1.')).toBeTruthy();
  expect(screen.getByText('sesn_missing: Session not found')).toBeTruthy();
});

test('adds a session to a custom evaluation dataset', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_case');
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
      return Response.json({ id: 'evalcase_from_session', type: 'evaluation_case' });
    }
    return Response.json({
      data: [
        dataset(),
        dataset({ id: 'evalds_old', name: 'old', status: 'archived' }),
        dataset({ id: 'evalds_refunds', name: 'refunds' }),
      ],
      next_page: null,
    });
  });

  renderPage(<AddSessionToEvaluationDataset sessionId="sesn_case" />);
  fireEvent.click(screen.getByRole('button', { name: 'Add to evaluation dataset' }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Dataset' }));
  const listbox = await screen.findByRole('listbox');
  expect(within(listbox).queryByText('old')).toBeNull();
  const option = within(listbox).getByRole('option', { name: 'refunds' });
  fireEvent.pointerDown(option);
  fireEvent.mouseDown(option);
  fireEvent.pointerUp(option);
  fireEvent.mouseUp(option);
  fireEvent.click(option);
  await waitFor(() =>
    expect(within(dialog).getByRole('combobox', { name: 'Dataset' }).textContent).toContain('refunds'),
  );
  fireEvent.change(within(dialog).getByLabelText('Checkpoints'), { target: { value: '["ready"]' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add to evaluation dataset' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const created = requests.find((request) => request.method === 'POST');
  expect(created?.url).toContain('/evaluation-datasets/evalds_refunds/cases:from-session');
  expect(created?.csrf).toBe('csrf_test');
  expect(JSON.parse(created?.body ?? '{}')).toEqual({
    session_id: 'sesn_case',
    name: '',
    desc: '',
    checkpoints: ['ready'],
    include_output_files: false,
  });
});

const evaluatorPromptStarter = [
  '## Scoring',
  '1. Accuracy: Does the answer solve the question?',
  '2. Completeness: Are the key points covered?',
  '3. Clarity: Is the answer structured and easy to understand?',
  '',
  '## Requirements',
  '- Judge the Agent answer against each checkpoint.',
  '- Give a score and a short reason across these dimensions.',
].join('\n');

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
    if (url.includes('/llm_providers')) {
      return Response.json([
        {
          id: 'prov_test',
          type: 'llm_provider',
          name: 'Anthropic',
          slug: 'anthropic',
          api_backend: 'messages',
          base_url: 'https://api.example',
          has_api_key: true,
          api_key_last4: '1234',
          model_ids: ['claude'],
          created_at: '2026-10-22T00:00:00Z',
          updated_at: '2026-10-22T00:00:00Z',
        },
      ]);
    }
    return Response.json({ data: [evaluator()], next_page: null });
  });

  renderPage(<EvaluatorsPage />);
  expect(await screen.findByRole('link', { name: 'judge' })).toBeTruthy();
  expect(screen.getByRole('link', { name: 'judge' }).getAttribute('href')).toBe(
    '/workspaces/default/evaluations/evaluators/evalev_test',
  );
  expect(screen.getByRole('link', { name: 'Datasets' }).getAttribute('href')).toBe('/workspaces/default/evaluations');
  fireEvent.click(screen.getByRole('button', { name: 'Create evaluator' }));
  const dialog = screen.getByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'judge' } });
  fireEvent.click(await screen.findByRole('combobox', { name: 'Model' }));
  fireEvent.click(await screen.findByRole('option', { name: 'anthropic/claude' }));
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
    model: 'anthropic/claude',
    prompt_template: `${evaluatorPromptStarter} {{input}}`,
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
    if (url.includes('/v1/agents')) {
      return Response.json({ data: [{ id: 'agent_test', name: 'agent' }] });
    }
    if (url.includes('/v1/environments')) {
      return Response.json({ data: [{ id: 'env_test', name: 'env' }] });
    }
    if (url.includes('/evaluation-datasets')) {
      return Response.json({ data: [dataset()], next_page: null });
    }
    if (url.includes('/evaluation-evaluator-presets')) {
      return Response.json({
        data: [
          {
            id: 'evalev_oma_latency',
            type: 'evaluation_evaluator_preset',
            name: 'Latency',
            desc: '',
            status: 'available',
            version: {
              id: 'eev_oma_latency_v1',
              version: 1,
              evaluator_type: 'agent',
              prompt_template: 'score',
              variables: ['input'],
            },
            created_at: '2026-10-22T00:00:00Z',
            updated_at: '2026-10-22T00:00:00Z',
          },
        ],
        next_page: null,
      });
    }
    if (url.includes('/evaluation-evaluators')) {
      return Response.json({
        data: [
          { ...evaluator(), id: 'evalev_a', name: 'Alpha' },
          { ...evaluator(), id: 'evalev_b', name: 'Beta' },
        ],
        next_page: null,
      });
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
  expect(await screen.findByRole('option', { name: 'billing v1' })).toBeTruthy();
  expect(screen.getByRole('option', { name: 'agent' })).toBeTruthy();
  expect(screen.getByRole('option', { name: 'env' })).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Dataset version'), { target: { value: 'edv_test' } });
  fireEvent.change(screen.getByLabelText('Agent'), { target: { value: 'agent_test' } });
  fireEvent.change(screen.getByLabelText('Environment'), { target: { value: 'env_test' } });
  fireEvent.change(screen.getByLabelText('Timeout (seconds)'), { target: { value: '60' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add environment variable' }));
  fireEvent.change(screen.getByLabelText('Environment variables'), { target: { value: 'TRACE=1' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add resource' }));
  fireEvent.change(screen.getByLabelText('Resources'), {
    target: { value: '[{"type":"file","file_id":"file_a"}]' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Select evaluators' }));
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Alpha' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Beta' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Latency' }));
  fireEvent.click(screen.getByRole('button', { name: 'New experiment' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const created = requests.find((request) => request.method === 'POST');
  expect(created?.url).toContain('/evaluation-experiments');
  expect(created?.csrf).toBe('csrf_test');
  expect(window.location.pathname).toBe('/workspaces/default/evaluations/experiments/evalexp_test');
  expect(JSON.parse(created?.body ?? '{}')).toEqual({
    name: 'nightly',
    desc: '',
    dataset_version_id: 'edv_test',
    agent_id: 'agent_test',
    environment_id: 'env_test',
    timeout_seconds: 60,
    env: { TRACE: '1' },
    resources: [{ type: 'file', file_id: 'file_a' }],
    evaluator_ids: ['evalev_a', 'evalev_b', 'evalev_oma_latency'],
  });
});

test('shows evaluator prompt, score schema, and agent-owned skills', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/evaluators/evalev_test');
  globalThis.fetch = mock(async () =>
    Response.json({
      ...evaluator(),
      version: {
        ...evaluator().version,
        evaluator_type: 'agent',
        agent_id: 'agent_score',
        prompt_template: '',
        agent_contract: {
          result_path: '/data/session/result.json',
          score_path: '/output/score.json',
          score_fields: ['score', 'explanation', 'details'],
        },
      },
    }),
  );
  renderPage(<EvaluatorDetailPage />);
  expect(await screen.findByRole('heading', { name: 'Prompt template' })).toBeTruthy();
  expect(screen.getByRole('heading', { name: 'Output metrics' })).toBeTruthy();
  expect(screen.getByText('/data/session/result.json → /output/score.json')).toBeTruthy();
  expect(
    screen.getByText('Skills stay on the bound Agent. This evaluator does not mount skills of its own.'),
  ).toBeTruthy();
  expect(screen.getByRole('link', { name: 'Open Agent agent_score' }).getAttribute('href')).toBe(
    '/workspaces/default/agents/agent_score',
  );
});

test('saves an llm evaluator prompt as a new version', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/evaluators/evalev_test');
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
    return Response.json(evaluator());
  });
  renderPage(<EvaluatorDetailPage />);
  fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));
  expect(await screen.findByLabelText('Prompt')).toBeTruthy();
  expect(
    screen.getByText('Saving creates a new version. Experiments already created keep the previous version.'),
  ).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Prompt'), { target: { value: 'Score {{input}} again' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save as new version' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const saved = requests.find((request) => request.method === 'POST');
  expect(saved?.url).toContain('/evaluation-evaluators/evalev_test/versions');
  expect(saved?.csrf).toBe('csrf_test');
  expect(JSON.parse(saved?.body ?? '{}')).toEqual({
    model: 'claude',
    prompt_template: 'Score {{input}} again',
    variables: ['input'],
  });
});

test('shows scores on an experiment', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/experiments/evalexp_test');
  globalThis.fetch = mock(async () =>
    Response.json({
      ...experiment(),
      scores: [
        {
          id: 'evalscore_test',
          item_id: 'evalitem_test',
          evaluator_id: 'evalev_test',
          version_id: 'eev_test',
          status: 'completed',
          score: 0.91,
          explanation: 'contract path',
        },
      ],
    }),
  );
  renderPage(<ExperimentDetailPage />);
  expect(await screen.findByRole('heading', { name: 'nightly' })).toBeTruthy();
  expect(screen.getAllByText('1 / 1 (1 ok)').length).toBe(2);
  expect(screen.getByRole('tab', { name: 'Overview' })).toBeTruthy();
  expect(screen.getByRole('tab', { name: 'Report' })).toBeTruthy();
  expect(screen.getAllByText('0.91').length).toBeGreaterThan(0);
  fireEvent.click(screen.getByRole('tab', { name: 'Configuration' }));
  expect(screen.getByText('env_test')).toBeTruthy();
  expect(screen.getByText('60')).toBeTruthy();
  fireEvent.click(screen.getByRole('tab', { name: 'Case results' }));
  expect(screen.getAllByRole('cell', { name: '0.91' }).length).toBeGreaterThan(0);
  fireEvent.click(screen.getByRole('button', { name: 'View evalitem_test' }));
  expect(await screen.findByText('contract path')).toBeTruthy();
});

test('opens a case result with the case, the session trace, and the score', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/experiments/evalexp_test');
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes('/events')) {
      return Response.json({
        data: [
          { id: 'evt_user', type: 'user.message', content: '写一封拒绝合作的邮件。' },
          { id: 'evt_agent', type: 'agent.message', content: '您好，无法继续合作。' },
        ],
        next_page: null,
      });
    }
    if (url.includes('/sessions/sesn_case')) {
      return Response.json({
        id: 'sesn_case',
        type: 'session',
        status: 'idle',
        created_at: '2026-10-27T11:29:22.000Z',
        updated_at: '2026-10-27T11:29:32.150Z',
        usage: { input_tokens: 12, output_tokens: 40 },
      });
    }
    if (isCaseList(url)) {
      return Response.json({
        data: [
          {
            ...evaluationCase('evalcase_test', '礼貌拒信', '写一封拒绝合作的邮件。'),
            desc: '办公写作',
            checkpoints: ['明确拒绝', '语气礼貌'],
          },
        ],
        next_page: null,
      });
    }
    return Response.json({
      ...experiment(),
      items: [
        {
          id: 'evalitem_test',
          case_id: 'evalcase_test',
          position: 1,
          session_id: 'sesn_case',
          status: 'completed',
        },
      ],
      scores: [
        {
          id: 'evalscore_test',
          item_id: 'evalitem_test',
          evaluator_id: 'evalev_test',
          version_id: 'eev_test',
          status: 'completed',
          score: 1,
          explanation: '任务规模较小，未调用工具。',
          details: { findings: 'first_response' },
        },
      ],
    });
  });

  renderPage(<ExperimentDetailPage />);
  fireEvent.click(await screen.findByRole('tab', { name: 'Case results' }));
  fireEvent.click(screen.getByRole('button', { name: 'View evalcase_test' }));
  const drawer = await screen.findByRole('dialog');
  expect(await within(drawer).findByText('Duration 10.15s')).toBeTruthy();
  expect(within(drawer).getByRole('heading', { name: 'Agent result' })).toBeTruthy();
  expect(within(drawer).getByText('礼貌拒信')).toBeTruthy();
  expect(within(drawer).getByText('办公写作')).toBeTruthy();
  expect(within(drawer).getByText('Turn 1')).toBeTruthy();
  expect(within(drawer).getAllByText('写一封拒绝合作的邮件。').length).toBeGreaterThan(0);
  expect(within(drawer).getByText('明确拒绝')).toBeTruthy();
  expect(within(drawer).getAllByText('您好，无法继续合作。').length).toBe(2);
  expect(within(drawer).getByText('12 / 40')).toBeTruthy();
  expect(within(drawer).getByText('user')).toBeTruthy();
  expect(within(drawer).getByText('agent')).toBeTruthy();
  expect(within(drawer).getByText('Score 100.0')).toBeTruthy();
  expect(within(drawer).getByText('任务规模较小，未调用工具。')).toBeTruthy();
  expect(within(drawer).getByText('{"findings":"first_response"}')).toBeTruthy();
});

test('shows the analysis report in prose', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/experiments/evalexp_test');
  globalThis.fetch = mock(async () =>
    Response.json({
      ...experiment(),
      items: [
        {
          id: 'evalitem_test',
          case_id: 'evalcase_test',
          position: 1,
          session_id: 'sesn_case',
          status: 'completed',
        },
      ],
      scores: [
        {
          id: 'evalscore_test',
          item_id: 'evalitem_test',
          evaluator_id: 'evalev_test',
          version_id: 'eev_test',
          status: 'completed',
          score: 1,
          explanation: '共 1 轮，最慢 4 秒。',
          details: {
            rounds: [
              {
                index: 1,
                model: 'default-2/deepseek-v4.1-flash',
                input_tokens: 258,
                output_tokens: 311,
                duration_seconds: 4,
              },
            ],
            slowest_seconds: 4,
          },
        },
      ],
    }),
  );
  renderPage(<ExperimentDetailPage />);
  fireEvent.click(await screen.findByRole('tab', { name: 'Report' }));
  expect(screen.getByRole('heading', { name: 'Main issues' })).toBeTruthy();
  expect(screen.getByText('No failed scores, and none below a full mark.')).toBeTruthy();
  expect(screen.getByText('共 1 轮，最慢 4 秒。')).toBeTruthy();
  expect(
    screen.getByText('Round 1, default-2/deepseek-v4.1-flash, 4s, input 258, output 311'),
  ).toBeTruthy();
  expect(screen.getByText('Average 100%')).toBeTruthy();
  expect(screen.queryByText(/input_tokens/)).toBeNull();
  expect(screen.queryByText(/slowest_seconds/)).toBeNull();
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
  const detail = screen.getByRole('link', { name: 'View OMA demo dataset 03' });
  expect(detail.getAttribute('href')).toBe('/workspaces/default/evaluations/datasets/evalds_oma_demo_03?preset=1');
});

test('shows a preset dataset on the detail page', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/datasets/evalds_oma_demo_03?preset=1');
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes('/cases')) {
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
    return Response.json({
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
    });
  });

  renderPage(<DatasetCasesPage />);
  expect(await screen.findByRole('heading', { name: 'OMA demo dataset 03' })).toBeTruthy();
  expect(
    await screen.findByText('OMA demo case 1 on dataset 3 version 2. Reply with the word ready.'),
  ).toBeTruthy();
  expect(screen.getByText('Built-in')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Add case' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Edit version 2' })).toBeNull();
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
            id: 'evalev_oma_latency',
            type: 'evaluation_evaluator_preset',
            name: '时延效率',
            desc: 'OMA-authored preset. Not a QCA evaluator.',
            status: 'available',
            version: {
              id: 'eev_oma_latency_v1',
              version: 1,
              evaluator_type: 'agent',
              prompt_template: 'Read /data/session/result.json and write /output/score.json. {{agent_trajectory}}',
              variables: ['agent_trajectory', 'invocation_result'],
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
  expect(await screen.findByText('时延效率')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Create from preset 时延效率' }));
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
    source_version_id: 'eev_oma_latency_v1',
  });
});

test('compare shows per-evaluator scores for completed experiments', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/experiments/compare');
  const evaluators = [
    { id: 'evalev_a', version_id: 'eev_a', position: 1 },
    { id: 'evalev_b', version_id: 'eev_b', position: 2 },
    { id: 'evalev_c', version_id: 'eev_c', position: 3 },
  ];
  globalThis.fetch = mock(async () =>
    Response.json({
      data: [
        {
          ...experiment(),
          status: 'completed',
          evaluators,
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
        {
          ...experiment(),
          id: 'evalexp_other',
          name: 'other',
          agent_id: 'agent_other',
          status: 'completed',
          evaluators,
          scores: [],
        },
      ],
      next_page: null,
    }),
  );
  renderPage(<ExperimentComparePage />);
  expect(await screen.findByRole('option', { name: 'nightly' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Choose experiments to compare' })).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Baseline'), { target: { value: 'evalexp_test' } });
  expect(await screen.findByText('Fixed for this comparison')).toBeTruthy();
  const picker = screen.getByLabelText('Choose experiments to compare');
  expect(await within(picker).findByRole('option', { name: 'other' })).toBeTruthy();
  fireEvent.change(picker, { target: { value: 'evalexp_other' } });
  fireEvent.click(screen.getByRole('button', { name: 'Compare' }));
  expect(await screen.findByRole('columnheader', { name: 'evalev_a' })).toBeTruthy();
  expect(screen.getByRole('columnheader', { name: 'evalev_b' })).toBeTruthy();
  expect(screen.getByRole('columnheader', { name: 'evalev_c' })).toBeTruthy();
  expect(screen.getByRole('cell', { name: '0.5, 1' })).toBeTruthy();
  expect(screen.getByRole('cell', { name: 'Could not parse evaluator score' })).toBeTruthy();
  expect(screen.getAllByRole('cell', { name: 'Missing' }).length).toBeGreaterThan(0);
});

test('compare shows an empty state when no experiments are completed', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/evaluations/experiments/compare');
  globalThis.fetch = mock(async () => Response.json({ data: [], next_page: null }));
  renderPage(<ExperimentComparePage />);
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

function evaluationCase(id: string, name: string, turn: string) {
  return {
    id,
    type: 'evaluation_case',
    dataset_id: 'evalds_test',
    version_id: 'edv_test',
    position: 1,
    name,
    desc: '',
    turns: [turn],
    checkpoints: null,
    input_files: null,
    reference_files: null,
    created_at: '2026-10-22T00:00:00Z',
    updated_at: '2026-10-22T00:00:00Z',
  };
}

async function openCaseAction(name: string) {
  fireEvent.click(screen.getByRole('button', { name: 'Add case' }));
  fireEvent.click(await screen.findByRole('menuitem', { name }));
}

function isCaseList(url: string) {
  return (url.split('?')[0] ?? url).endsWith('/cases');
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
