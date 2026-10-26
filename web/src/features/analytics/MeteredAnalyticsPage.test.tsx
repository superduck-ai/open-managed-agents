import { I18nProvider, type Locale } from '@/shared/i18n';
import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WorkspaceContext } from '@/shared/workspaces/context';
import { defaultWorkspace } from '@/shared/workspaces/api';
import { resetTestDom } from '@/test/setup';
import { CostPage, UsagePage } from './MeteredAnalyticsPage';
import type { UsageMetrics, UsageReport } from './usage-api';

const { cleanup, render, screen, fireEvent, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  for (const client of clients) client.clear();
  clients.length = 0;
  globalThis.fetch = originalFetch;
});

function report(cents = '125'): UsageReport {
  const metrics: UsageMetrics = {
    requests: 2,
    input_tokens: 100,
    output_tokens: 50,
    cache_read_input_tokens: 200,
    cache_creation_input_tokens: 300,
    cache_write_5m_tokens: 100,
    cache_write_1h_tokens: 200,
    web_search_requests: 2,
    model_cost_cents: cents,
    search_cost_cents: '2',
    subtotal_cents: String(BigInt(cents) + 2n),
    unpriced_requests: 0,
    incomplete_requests: 0,
  };
  return {
    currency: 'USD',
    cost_basis: 'list_price_model_and_search',
    billing_version: 'request_cents_v1',
    included_components: ['model', 'web_search'],
    excluded_components: ['code_execution', 'session_runtime'],
    as_of: '2026-10-14T00:00:00Z',
    starting_at: '2026-10-01T00:00:00Z',
    ending_at: '2026-11-01T00:00:00Z',
    timezone: 'UTC',
    summary: metrics,
    daily: [{ key: '2026-10-01', metrics }],
    options: {
      models: ['real-model', 'other-model'],
      api_keys: [],
      workspaces: [{ value: 'workspace-real', label: 'Real workspace' }],
      first_seen: '2026-09-01T00:00:00Z',
      sources: ['api'],
    },
  };
}

function mount(cost: boolean, locale: Locale = 'en') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  return render(
    <I18nProvider initialLocale={locale}>
      <QueryClientProvider client={client}>
        <WorkspaceContext.Provider
          value={{
            orgUuid: 'org_test',
            workspaces: [defaultWorkspace],
            activeWorkspace: defaultWorkspace,
            activeWorkspaceId: 'default',
            isLoading: false,
            error: null,
            selectWorkspace: () => {},
            createWorkspace: async () => defaultWorkspace,
            refreshWorkspaces: async () => {},
          }}
        >
          {cost ? <CostPage /> : <UsagePage />}
        </WorkspaceContext.Provider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

test('查询失败不显示零费用', async () => {
  resetTestDom('https://oma.duck.ai/cost?month=2026-10');
  globalThis.fetch = (async () => new Response('{}', { status: 403 })) as typeof fetch;
  mount(true);
  await screen.findByText('Could not load costs. Check your access and retry.');
  expect(screen.queryByText('USD 0.00')).toBeNull();
  expect(screen.queryByText('Model and search subtotal')).toBeNull();
});

test('真实缓存用量不重复计数', async () => {
  resetTestDom('https://oma.duck.ai/usage?month=2026-10');
  globalThis.fetch = (async () => Response.json(report())) as typeof fetch;
  mount(false);
  await screen.findByText('600');
  expect(screen.getAllByText('600').length).toBeGreaterThan(0);
  expect(screen.queryByText('Cache read tokens')).toBeNull();
  expect(screen.queryByRole('table')).toBeNull();
  expect(screen.queryByRole('textbox', { name: 'Session ID' })).toBeNull();
  expect(screen.queryByText('request_cents_v1')).toBeNull();
  expect(screen.queryByText('June 2026')).toBeNull();
});

test('非零费用随模型筛选重新查询且保留 URL', async () => {
  resetTestDom('https://oma.duck.ai/cost?month=2026-10');
  const requests: URL[] = [];
  globalThis.fetch = (async (input) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    requests.push(url);
    return Response.json(report(url.searchParams.get('model') === 'other-model' ? '250' : '125'));
  }) as typeof fetch;
  mount(true);
  await screen.findByText('USD 1.25');
  expect(screen.getByText('Total cost')).toBeTruthy();
  expect(screen.getByText('Total code execution cost')).toBeTruthy();
  expect(screen.getByText('Total session runtime cost')).toBeTruthy();
  expect(screen.getByText('USD 1.27')).toBeTruthy();
  expect(screen.getAllByText('USD 0.00')).toHaveLength(2);
  expect(screen.queryByText('—')).toBeNull();
  expect(screen.queryByText('Model and search subtotal')).toBeNull();
  expect(screen.getAllByRole('article')).toHaveLength(5);
  fireEvent.click(screen.getByRole('combobox', { name: 'Model: All' }));
  const option = screen.getByRole('option', { name: 'other-model' });
  fireEvent.pointerDown(option);
  fireEvent.mouseDown(option);
  fireEvent.pointerUp(option);
  fireEvent.mouseUp(option);
  fireEvent.click(option);
  await waitFor(() => expect(requests.map((url) => url.searchParams.get('model'))).toContain('other-model'));
  await screen.findByText('USD 2.50');
  expect(screen.getByText('USD 2.52')).toBeTruthy();
  expect(requests.some((url) => url.searchParams.get('model') === 'other-model')).toBe(true);
  await waitFor(() => expect(window.location.search).toContain('model=other-model'));
});

test('工作区路径初始化筛选且允许切换到全部工作区', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/cost?month=2026-10&session_id=sesn_real');
  const requests: URL[] = [];
  globalThis.fetch = (async (input) => {
    requests.push(new URL(String(input), 'https://oma.duck.ai'));
    return Response.json(report());
  }) as typeof fetch;
  mount(true);
  await screen.findByText('USD 1.25');
  expect(requests[0].searchParams.get('workspace_id')).toBe('default');
  expect(requests[0].searchParams.get('session_id')).toBe('sesn_real');
  const workspace = screen.getByRole('combobox', { name: /Workspace:/ });
  expect((workspace as HTMLButtonElement).disabled).toBe(false);
  fireEvent.click(workspace);
  const all = screen.getByRole('option', { name: 'All', exact: true });
  fireEvent.pointerDown(all);
  fireEvent.mouseDown(all);
  fireEvent.pointerUp(all);
  fireEvent.mouseUp(all);
  fireEvent.click(all);
  await waitFor(() => expect(requests.at(-1)?.searchParams.get('workspace_id')).toBe(''));
  expect(requests.at(-1)?.searchParams.has('session_id')).toBe(false);
  await waitFor(() => expect(new URLSearchParams(window.location.search).get('workspace_id')).toBe(''));
});

test('工作区路径刷新保留显式选择的全部工作区', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/cost?workspace_id=&month=2026-10');
  const requests: URL[] = [];
  globalThis.fetch = (async (input) => {
    requests.push(new URL(String(input), 'https://oma.duck.ai'));
    return Response.json(report());
  }) as typeof fetch;
  mount(true);
  await screen.findByText('USD 1.25');
  expect(requests[0].searchParams.get('workspace_id')).toBe('');
});

for (const cost of [false, true]) {
  test(`${cost ? '成本' : '用量'}页时间和 API Key 筛选触发查询并更新汇总`, async () => {
    resetTestDom(`https://oma.duck.ai/${cost ? 'cost' : 'usage'}`);
    const requests: URL[] = [];
    globalThis.fetch = (async (input) => {
      const url = new URL(String(input), 'https://oma.duck.ai');
      requests.push(url);
      const result = report(url.searchParams.has('api_key_id') ? '250' : '125');
      result.options.api_keys = ['key-real'];
      if (url.searchParams.has('api_key_id')) result.summary.output_tokens = 75;
      return Response.json(result);
    }) as typeof fetch;
    mount(cost);
    await screen.findByText(cost ? 'USD 1.25' : '600');
    async function select(name: string, value: string) {
      fireEvent.click(screen.getByRole('combobox', { name }));
      const option = screen.getByRole('option', { name: value, exact: true });
      fireEvent.pointerDown(option);
      fireEvent.mouseDown(option);
      fireEvent.pointerUp(option);
      fireEvent.mouseUp(option);
      fireEvent.click(option);
    }
    await select('Range: Last 30 days', 'Last 7 days');
    await waitFor(() => {
      const params = requests.at(-1)!.searchParams;
      expect(new Date(params.get('ending_at')!).getTime() - new Date(params.get('starting_at')!).getTime()).toBe(
        7 * 86400000,
      );
    });
    await select('API key: All', 'key-real');
    await waitFor(() => expect(requests.at(-1)!.searchParams.get('api_key_id')).toBe('key-real'));
    await screen.findByText(cost ? 'USD 2.50' : '75');
    expect(new URLSearchParams(window.location.search).get('api_key_id')).toBe('key-real');
    expect(screen.queryByRole('combobox', { name: /Source:/ })).toBeNull();
    expect(screen.queryByRole('table')).toBeNull();
    expect(screen.queryByText('request_cents_v1')).toBeNull();
  });
}

for (const cost of [false, true]) {
  test(`中文${cost ? '成本' : '用量'}页面没有英文回退文案`, async () => {
    resetTestDom(`https://oma.duck.ai/${cost ? 'cost' : 'usage'}?month=2026-10`);
    globalThis.fetch = (async () => Response.json(report())) as typeof fetch;
    mount(cost, 'zh-CN');
    await screen.findByText(cost ? '总费用' : '总输入 token');
    expect(screen.getByRole('combobox', { name: /工作区/ })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: /范围/ })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: /模型/ })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: /API 密钥/ })).toBeTruthy();
    expect(screen.queryByText('Model and search subtotal')).toBeNull();
  });
}

test('默认工作区沿用控制台 ID 并显示本地化名称', async () => {
  resetTestDom('https://oma.duck.ai/cost?workspace_id=default');
  const result = report();
  result.options.workspaces = [{ value: 'default', label: 'default' }];
  globalThis.fetch = (async () => Response.json(result)) as typeof fetch;
  mount(true, 'zh-CN');
  await screen.findByRole('combobox', { name: '工作区: 默认' });
});
