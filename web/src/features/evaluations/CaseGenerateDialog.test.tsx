import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { CaseGenerateDialog } from './CaseGenerateDialog';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

afterEach(cleanup);

const scope: WorkspaceContextValue = {
  orgUuid: 'org',
  workspaces: [defaultWorkspace],
  activeWorkspace: defaultWorkspace,
  activeWorkspaceId: 'ws',
  isLoading: false,
  error: null,
  selectWorkspace: () => undefined,
  createWorkspace: async () => defaultWorkspace,
  refreshWorkspaces: async () => undefined,
};

type Recorded = { url: string; body: unknown };

const planResponse = {
  type: 'evaluation_case_plan',
  scenarios: [{ name: '查询订单', description: '常规查询', persona: '', polarity: 'positive', count: 2 }],
};

const draftsResponse = {
  type: 'evaluation_case_drafts',
  data: [draft('订单 A100 到哪了', ['lookup_order']), draft('订单 B200 什么时候到', null)],
  requested: 2,
  duplicates: 1,
  failed: 0,
};

test('the case count can be typed and stays between 1 and 50', () => {
  mockConsole(() => undefined);
  try {
    renderDialog(() => undefined);
    const count = screen.getByLabelText('条数');
    fireEvent.change(count, { target: { value: '30' } });
    expect(screen.getByRole('button', { name: '生成 30 条用例' })).toBeTruthy();
    fireEvent.change(count, { target: { value: '80' } });
    fireEvent.blur(count);
    expect(screen.getByRole('button', { name: '生成 50 条用例' })).toBeTruthy();
  } finally {
    restoreFetch();
  }
});

test('a failed draft batch leaves the plan open for another try', async () => {
  const requests = mockConsole((url) => {
    if (url.includes('case-drafts:generate')) {
      return Response.json(
        { type: 'error', error: { type: 'api_error', message: 'Could not generate evaluation cases' } },
        { status: 500 },
      );
    }
    return undefined;
  });
  try {
    renderDialog(() => undefined);

    fireEvent.click(screen.getByRole('button', { name: '生成 20 条用例' }));
    expect(await screen.findByDisplayValue('查询订单')).toBeTruthy();
    expect(screen.getByRole('alert').textContent).toContain('没能生成评测用例');
    expect(screen.getByRole('button', { name: '生成 2 条用例' })).toBeTruthy();
    expect(requests.filter((request) => request.url.includes('case-drafts:generate'))).toHaveLength(1);
  } finally {
    restoreFetch();
  }
});

test('one click plans and generates, and only the kept drafts are saved', async () => {
  const requests = mockConsole(() => undefined);
  let open = true;
  try {
    renderDialog((next) => (open = next));

    fireEvent.change(screen.getByLabelText('补充说明'), { target: { value: '覆盖物流查询' } });
    fireEvent.click(screen.getByRole('button', { name: '生成 20 条用例' }));
    expect(await screen.findByDisplayValue('订单 A100 到哪了')).toBeTruthy();
    expect(requests.find((request) => request.url.includes('case-drafts:plan'))?.body).toEqual({
      note: '覆盖物流查询',
      dataset_id: 'evalds_1',
      count: 20,
    });
    expect(requests.find((request) => request.url.includes('case-drafts:generate'))?.body).toEqual({
      note: '覆盖物流查询',
      dataset_id: 'evalds_1',
      scenarios: planResponse.scenarios,
    });
    expect(screen.getByText('计划 2 条，去掉重复 1 条，0 条没有生成。')).toBeTruthy();
    expect(screen.getByText('期望工具：lookup_order')).toBeTruthy();

    fireEvent.click(screen.getAllByRole('checkbox', { name: '保留这条用例' })[1]);
    fireEvent.click(await screen.findByRole('button', { name: '添加 1 条用例' }));
    await waitFor(() => expect(open).toBe(false));
    expect(requests.find((request) => request.url.endsWith('/datasets/evalds_1/cases'))?.body).toEqual({
      cases: [
        {
          query: '订单 A100 到哪了',
          expected_answer: '物流节点',
          expected_tool_trace: ['lookup_order'],
          polarity: 'positive',
          acceptance: '',
          origin: 'generated',
        },
      ],
    });
  } finally {
    restoreFetch();
  }
});

test('an unchanged plan returns to the generated cases without another request', async () => {
  const requests = mockConsole(() => undefined);
  try {
    renderDialog(() => undefined);

    fireEvent.click(screen.getByRole('button', { name: '生成 20 条用例' }));
    expect(await screen.findByDisplayValue('订单 A100 到哪了')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '调整计划' }));
    expect(await screen.findByDisplayValue('查询订单')).toBeTruthy();
    expect(screen.getByRole('button', { name: '返回用例' })).toBeTruthy();

    const generated = requests.filter((request) => request.url.includes('case-drafts:generate')).length;
    fireEvent.click(screen.getByRole('button', { name: '返回用例' }));
    expect(await screen.findByDisplayValue('订单 A100 到哪了')).toBeTruthy();
    expect(requests.filter((request) => request.url.includes('case-drafts:generate'))).toHaveLength(generated);
  } finally {
    restoreFetch();
  }
});

test('a changed plan can still return to the previous cases', async () => {
  const requests = mockConsole(() => undefined);
  try {
    renderDialog(() => undefined);

    fireEvent.click(screen.getByRole('button', { name: '生成 20 条用例' }));
    expect(await screen.findByDisplayValue('订单 A100 到哪了')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '调整计划' }));
    fireEvent.change(await screen.findByDisplayValue('查询订单'), { target: { value: '物流查询' } });
    expect(screen.getByRole('button', { name: '重新生成 2 条用例' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '返回用例' }));
    expect(await screen.findByDisplayValue('订单 A100 到哪了')).toBeTruthy();
    expect(requests.filter((request) => request.url.includes('case-drafts:generate'))).toHaveLength(1);
  } finally {
    restoreFetch();
  }
});

test('the plan can be reviewed first and adjusted again from the drafts', async () => {
  const requests = mockConsole(() => undefined);
  try {
    renderDialog(() => undefined);

    fireEvent.click(screen.getByRole('button', { name: '先看覆盖计划' }));
    const name = await screen.findByDisplayValue('查询订单');
    expect(requests.some((request) => request.url.includes('case-drafts:generate'))).toBe(false);

    fireEvent.change(name, { target: { value: '物流查询' } });
    fireEvent.click(screen.getByRole('button', { name: '生成 2 条用例' }));
    expect(await screen.findByDisplayValue('订单 A100 到哪了')).toBeTruthy();
    const sent = requests.find((request) => request.url.includes('case-drafts:generate'))?.body as {
      scenarios: Array<{ name: string }>;
    };
    expect(sent.scenarios[0].name).toBe('物流查询');

    fireEvent.click(screen.getByRole('button', { name: '调整计划' }));
    expect(await screen.findByDisplayValue('物流查询')).toBeTruthy();
  } finally {
    restoreFetch();
  }
});

const originalFetch = globalThis.fetch;

function restoreFetch() {
  globalThis.fetch = originalFetch;
}

test('closing during planning does not generate cases', async () => {
  let release: (response: Response) => void = () => undefined;
  const pending = new Promise<Response>((resolve) => {
    release = resolve;
  });
  const requests = mockConsole((url) => (url.includes('case-drafts:plan') ? pending : undefined));
  try {
    renderDialog(() => undefined);
    fireEvent.click(screen.getByRole('button', { name: '生成 20 条用例' }));
    expect(await screen.findByRole('status')).toBeTruthy();
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    release(Response.json(planResponse));
    await pending;
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull());
    expect(requests.filter((request) => request.url.includes('case-drafts:generate'))).toHaveLength(0);
    expect(screen.queryByDisplayValue('查询订单')).toBeNull();
  } finally {
    restoreFetch();
  }
});

test('previewing the plan shows progress until the model answers', async () => {
  const bodies = recordedPlan();
  try {
    renderDialog(() => undefined);
    fireEvent.click(screen.getByRole('button', { name: '先看覆盖计划' }));
    expect((await screen.findByRole('status')).textContent).toContain('正在生成覆盖计划');
    expect(screen.getAllByRole('button', { name: '正在规划…' }).length).toBeGreaterThan(0);
    expect((screen.getByLabelText('条数') as HTMLInputElement).disabled).toBe(true);
    bodies.release(Response.json(planResponse));
    expect(await screen.findByDisplayValue('查询订单')).toBeTruthy();
    expect(screen.queryByRole('status')).toBeNull();
  } finally {
    restoreFetch();
  }
});

function recordedPlan() {
  let release: (response: Response) => void = () => undefined;
  const pending = new Promise<Response>((resolve) => {
    release = resolve;
  });
  mockConsole((url) => (url.includes('case-drafts:plan') ? pending : undefined));
  return { release };
}

function mockConsole(override: (url: string) => Response | Promise<Response> | undefined) {
  const requests: Recorded[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    requests.push({ url, body: init?.body ? JSON.parse(String(init.body)) : null });
    const custom = override(url);
    if (custom) return custom;
    if (url.includes('case-drafts:plan')) return Response.json(planResponse);
    if (url.includes('case-drafts:generate')) return Response.json(draftsResponse);
    return Response.json({ data: [], next_page: null });
  }) as typeof fetch;
  return requests;
}

function renderDialog(onOpenChange: (open: boolean) => void) {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <WorkspaceContext.Provider value={scope}>
        <I18nProvider initialLocale="zh-CN">
          <CaseGenerateDialog open onOpenChange={onOpenChange} datasetId="evalds_1" />
        </I18nProvider>
      </WorkspaceContext.Provider>
    </QueryClientProvider>,
  );
}

function draft(query: string, trace: string[] | null) {
  return {
    query,
    expected_answer: '物流节点',
    expected_tool_trace: trace,
    polarity: 'positive',
    acceptance: '',
    scenario: '查询订单',
  };
}
