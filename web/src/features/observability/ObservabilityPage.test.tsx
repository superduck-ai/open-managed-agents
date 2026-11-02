import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';

const { cleanup, fireEvent, render, waitFor } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { WorkspaceContext } = await import('../../shared/workspaces/context');
const { defaultWorkspace } = await import('../../shared/workspaces/api');
const { I18nProvider } = await import('../../shared/i18n');
const { ObservabilityPage } = await import('./ObservabilityPage');
const originalFetch = globalThis.fetch;
const clients: InstanceType<typeof QueryClient>[] = [];

afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  globalThis.fetch = originalFetch;
  window.history.replaceState(null, '', '/');
});

function renderPage(
  orgUuid?: string,
  activeWorkspaceId = 'ws',
  locale: 'en' | 'zh-CN' = 'en',
  scope: { kind: 'workspace' } | { kind: 'agent'; agentId: string } = { kind: 'workspace' },
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale={locale}>
        <WorkspaceContext
          value={{
            orgUuid,
            activeWorkspaceId,
            activeWorkspace: defaultWorkspace,
            workspaces: [defaultWorkspace],
            isLoading: false,
            error: null,
            selectWorkspace: () => {},
            createWorkspace: async () => defaultWorkspace,
            refreshWorkspaces: async () => {},
          }}
        >
          <ObservabilityPage scope={scope} />
        </WorkspaceContext>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

test('missing organization or workspace shows a placeholder without querying', () => {
  let calls = 0;
  globalThis.fetch = (async () => {
    calls++;
    return Response.json({});
  }) as typeof fetch;
  const orgMissing = renderPage();
  expect(orgMissing.getByRole('status').textContent).toBe('Select a workspace to view observability.');
  orgMissing.unmount();
  const workspaceMissing = renderPage('org', '', 'zh-CN');
  expect(workspaceMissing.getByRole('status').textContent).toBe('请选择工作区以查看可观测数据。');
  expect(calls).toBe(0);
});

function dashboardFixture(tab: string) {
  return {
    tab,
    interval_ms: 3_600_000,
    filter_options: tab === 'models' ? ['model-a', 'model-b'] : [],
    panels: [
      {
        id: 'active_sessions',
        render: 'stat',
        unit: 'count',
        span: 3,
        compare: true,
        data: { value: 12, previous: 8 },
      },
      {
        id: 'interaction_duration',
        render: 'stat',
        unit: 'duration_ms',
        span: 3,
        data: { value: 1200, previous: null, secondary: 4800 },
      },
      { id: 'turns_per_session', render: 'percentiles', unit: 'count', span: 3, data: { p50: 1, p90: 4, p95: 6 } },
      {
        id: 'model_request_outcomes',
        render: 'series',
        unit: 'count',
        span: 6,
        chart: 'bar',
        stacked: true,
        data: [
          { key: 'succeeded', points: [{ t: 1_793_016_000_000, v: 3 }] },
          { key: 'failed', points: [{ t: 1_793_016_000_000, v: 1 }] },
        ],
      },
      { id: 'end_reasons', render: 'bars', unit: 'count', span: 6, data: [{ name: 'error', value: 2 }] },
      {
        id: 'model_table',
        render: 'table',
        span: 12,
        columns: [
          { key: 'model', unit: 'text' },
          { key: 'p95_ms', unit: 'duration_ms' },
        ],
        data: { rows: [{ model: 'model-a', p95_ms: 9000 }] },
      },
    ],
  };
}

function dashboardFetch(urls: URL[]) {
  return (async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'http://localhost');
    urls.push(url);
    const tab = url.pathname.match(/dashboards\/([a-z]+)$/)?.[1];
    if (tab) return Response.json(dashboardFixture(tab));
    if (url.pathname.endsWith('/trajectories')) return Response.json({ items: [], next_offset: null });
    return Response.json({ data: [], next_page: null });
  }) as typeof fetch;
}

test('dashboard storage failures show a localized message without backend implementation details', async () => {
  globalThis.fetch = (async () =>
    Response.json({ error: { message: 'OpenObserve is unavailable' } }, { status: 503 })) as typeof fetch;
  const view = renderPage('org');
  await waitFor(() =>
    expect(view.getByRole('alert').textContent).toBe('Could not load the dashboard. Please try again later.'),
  );
  expect(view.container.textContent).not.toContain('OpenObserve');
  view.unmount();
  const chinese = renderPage('org', 'ws', 'zh-CN');
  await waitFor(() => expect(chinese.getByRole('alert').textContent).toBe('暂时无法加载看板，请稍后重试。'));
});

test('overview requests one dashboard per tab and renders every panel type', async () => {
  const urls: URL[] = [];
  globalThis.fetch = dashboardFetch(urls);
  const view = renderPage('org', 'ws', 'en', { kind: 'agent', agentId: 'agent_1' });
  expect(await view.findByText('Active sessions')).toBeTruthy();
  const dashboards = urls.filter((url) => url.pathname.includes('/dashboards/'));
  expect(dashboards.map((url) => url.pathname)).toEqual(['/api/organizations/org/observability/dashboards/overview']);
  expect(dashboards[0].searchParams.get('agent_id')).toBe('agent_1');
  const span =
    Date.parse(dashboards[0].searchParams.get('end_time')!) - Date.parse(dashboards[0].searchParams.get('start_time')!);
  expect(span).toBeGreaterThan(7 * 86_400_000 - 1);
  expect(view.getByText('12')).toBeTruthy();
  expect(view.getByText('+50%')).toBeTruthy();
  expect(view.getByText('1.2 s')).toBeTruthy();
  expect(view.getByText('p95 4.8 s')).toBeTruthy();
  fireEvent.click(view.getByRole('button', { name: 'p90' }));
  expect(view.getByText('4')).toBeTruthy();
  expect(view.getByRole('button', { name: 'Failed' }).getAttribute('aria-pressed')).toBe('true');
  expect(view.getByRole('table', { name: 'Model statistics' }).textContent).toContain('9 s');
  expect(view.queryByRole('combobox', { name: 'Agent' })).toBeNull();
  fireEvent.click(view.getByRole('button', { name: 'Refresh' }));
  await waitFor(() => expect(urls.filter((url) => url.pathname.includes('/dashboards/')).length).toBe(2));
});

test('model tab passes the chosen model and lists filter options from the response', async () => {
  const urls: URL[] = [];
  globalThis.fetch = dashboardFetch(urls);
  const view = renderPage('org');
  fireEvent.click(await view.findByRole('tab', { name: 'Models' }));
  await waitFor(() => expect(urls.some((url) => url.pathname.endsWith('/dashboards/models'))).toBe(true));
  const models = urls.find((url) => url.pathname.endsWith('/dashboards/models'))!;
  expect(models.searchParams.get('agent_id')).toBeNull();
  expect(models.searchParams.get('model')).toBeNull();
  expect(await view.findByRole('combobox', { name: 'Model' })).toBeTruthy();
  expect(view.getByRole('combobox', { name: 'Agent' })).toBeTruthy();
});

test('session picker lists sessions from the range, accepts a pasted ID and scopes dashboards', async () => {
  const urls: URL[] = [];
  const base = dashboardFetch(urls);
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'http://localhost');
    if (url.pathname.endsWith('/trajectories')) {
      urls.push(url);
      return Response.json({
        items: [{ session_id: 'sesn_titled', title: 'Quarterly report' }],
        next_offset: null,
      });
    }
    return base(input);
  }) as typeof fetch;
  const view = renderPage('org');
  expect(await view.findByText('Active sessions')).toBeTruthy();
  fireEvent.click(view.getByRole('combobox', { name: 'Session' }));
  fireEvent.click(await view.findByRole('option', { name: /Quarterly report/ }));
  await waitFor(() =>
    expect(
      urls.some(
        (url) => url.pathname.endsWith('/dashboards/overview') && url.searchParams.get('session_id') === 'sesn_titled',
      ),
    ).toBe(true),
  );
  expect(urls.find((url) => url.pathname.endsWith('/trajectories'))!.searchParams.get('session_id')).toBeNull();
  expect(view.getByRole('combobox', { name: 'Session' }).textContent).toContain('Quarterly report');
  fireEvent.click(view.getByRole('combobox', { name: 'Session' }));
  fireEvent.change(await view.findByPlaceholderText('Search or paste a session ID (sesn_…)'), {
    target: { value: 'sesn_pasted' },
  });
  fireEvent.click(await view.findByRole('option', { name: 'Use sesn_pasted' }));
  await waitFor(() => expect(new URLSearchParams(window.location.search).get('session_id')).toBe('sesn_pasted'));
  const clear = view.getByRole('button', { name: 'Clear Session' });
  expect(clear.closest('[role="combobox"]')).toBeNull();
  fireEvent.click(clear);
  await waitFor(() => expect(new URLSearchParams(window.location.search).get('session_id')).toBeNull());
});

test('panels without data show the empty state instead of placeholder values', async () => {
  globalThis.fetch = (async () =>
    Response.json({
      tab: 'overview',
      interval_ms: 3_600_000,
      filter_options: [],
      panels: [
        { id: 'interactions', render: 'stat', unit: 'count', span: 3, compare: true, data: { value: null } },
        { id: 'turns_per_session', render: 'percentiles', span: 3, data: { p50: null, p90: null, p95: null } },
        { id: 'session_activity', render: 'series', span: 6, data: [{ key: 'sessions', points: [{ t: 0, v: null }] }] },
        { id: 'model_request_outcomes', render: 'series', span: 6, chart: 'bar', stacked: true, data: [] },
      ],
    })) as typeof fetch;
  const view = renderPage('org');
  await waitFor(() => expect(view.getAllByText('No data in this range.')).toHaveLength(4));
  expect(view.queryByText('—')).toBeNull();
  expect(view.queryByRole('group', { name: 'Percentile' })).toBeNull();
});

test('zero values are data: all-zero series draw a chart and only a zero previous period hides the change', async () => {
  globalThis.fetch = (async () =>
    Response.json({
      tab: 'overview',
      interval_ms: 3_600_000,
      filter_options: [],
      panels: [
        {
          id: 'active_sessions',
          render: 'stat',
          unit: 'count',
          span: 3,
          compare: true,
          data: { value: 0, previous: 0 },
        },
        {
          id: 'model_requests',
          render: 'stat',
          unit: 'count',
          span: 3,
          compare: true,
          data: { value: 0, previous: 4 },
        },
        {
          id: 'session_activity',
          render: 'series',
          span: 6,
          data: [
            {
              key: 'sessions',
              points: [
                { t: 0, v: 0 },
                { t: 3_600_000, v: 0 },
              ],
            },
          ],
        },
      ],
    })) as typeof fetch;
  const view = renderPage('org');
  expect(await view.findByRole('img', { name: 'Session activity' })).toBeTruthy();
  expect(view.queryByText('No data in this range.')).toBeNull();
  const change = (title: string) => view.getByLabelText(title).textContent;
  expect(change('Active sessions')).toContain('vs previous period —');
  expect(change('Model requests')).toContain('-100%');
});

function dashboardUrls(urls: URL[]) {
  return urls.filter((url) => url.pathname.includes('/dashboards/'));
}

function dashboardSpans(urls: URL[]) {
  return dashboardUrls(urls).map(
    (url) => Date.parse(url.searchParams.get('end_time')!) - Date.parse(url.searchParams.get('start_time')!),
  );
}

test('an invalid range in the URL shows the problem and skips dashboard queries', async () => {
  const urls: URL[] = [];
  globalThis.fetch = dashboardFetch(urls);
  window.history.replaceState(null, '', '/?from=now&to=now-1h');
  const view = renderPage('org');
  expect(view.getByRole('alert').textContent).toBe('"From" must be before "To".');
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(dashboardSpans(urls)).toEqual([]);
});

test('time picker rejects bad absolute input, then applies a quick range and writes it to the URL', async () => {
  const urls: URL[] = [];
  globalThis.fetch = dashboardFetch(urls);
  window.history.replaceState(null, '', '/?tab=observability');
  const view = renderPage('org');
  expect(await view.findByText('Active sessions')).toBeTruthy();
  fireEvent.click(view.getByRole('button', { name: 'Time range' }));
  fireEvent.change(await view.findByLabelText('From'), { target: { value: 'last tuesday' } });
  fireEvent.click(view.getByRole('button', { name: 'Apply time range' }));
  expect(view.getByText('Use a time such as now-6h or 2026-01-31 08:00:00.')).toBeTruthy();
  fireEvent.click(view.getByRole('option', { name: 'Last 1 hour' }));
  await waitFor(() => expect(dashboardSpans(urls).at(-1)).toBe(3_600_000));
  const search = new URLSearchParams(window.location.search);
  expect(search.get('from')).toBe('now-1h');
  expect(search.get('tab')).toBe('observability');
  expect(view.getByRole('button', { name: 'Time range' }).textContent).toContain('Last 1 hour');
});

test('moving the range backwards pushes an absolute range of the same length', async () => {
  const urls: URL[] = [];
  globalThis.fetch = dashboardFetch(urls);
  window.history.replaceState(null, '', '/?from=now-6h&to=now');
  const view = renderPage('org');
  await waitFor(() => expect(dashboardSpans(urls)).toEqual([6 * 3_600_000]));
  const before = Date.parse(dashboardUrls(urls)[0].searchParams.get('end_time')!);
  fireEvent.click(view.getByRole('button', { name: 'Move time range backwards' }));
  await waitFor(() => expect(dashboardSpans(urls).length).toBe(2));
  const shifted = dashboardUrls(urls).at(-1)!;
  expect(Date.parse(shifted.searchParams.get('end_time')!)).toBeLessThanOrEqual(before - 3 * 3_600_000 + 1000);
  expect(dashboardSpans(urls).at(-1)).toBe(6 * 3_600_000);
  expect(new URLSearchParams(window.location.search).get('from')).toMatch(/^\d+$/);
  fireEvent.click(view.getByRole('button', { name: 'Zoom out time range' }));
  await waitFor(() => expect(dashboardSpans(urls).at(-1)).toBe(12 * 3_600_000));
});

test('a viewed panel from the URL is shown alone, Escape restores the grid and inspect lists its data', async () => {
  const urls: URL[] = [];
  globalThis.fetch = dashboardFetch(urls);
  window.history.replaceState(null, '', '/?view_panel=end_reasons');
  const view = renderPage('org');
  expect(await view.findByText('Turn end reasons')).toBeTruthy();
  expect(view.queryByText('Active sessions')).toBeNull();
  fireEvent.keyDown(window, { key: 'Escape' });
  expect(await view.findByText('Active sessions')).toBeTruthy();
  expect(new URLSearchParams(window.location.search).get('view_panel')).toBeNull();
  fireEvent.click(view.getByRole('button', { name: 'Menu for Turn end reasons' }));
  fireEvent.click(await view.findByRole('menuitem', { name: /Inspect data/ }));
  const dialog = await view.findByRole('dialog');
  expect(dialog.textContent).toContain('Failed');
  expect(dialog.textContent).toContain('Download CSV');
});

test('Escape leaves a viewed panel even when focus stayed inside the closed panel menu', async () => {
  globalThis.fetch = dashboardFetch([]);
  const view = renderPage('org');
  fireEvent.click(await view.findByRole('button', { name: 'Menu for Turn end reasons' }));
  fireEvent.click(await view.findByRole('menuitem', { name: /View/ }));
  await waitFor(() => expect(view.queryByText('Active sessions')).toBeNull());
  expect(new URLSearchParams(window.location.search).get('view_panel')).toBe('end_reasons');
  const closingMenu = document.createElement('div');
  closingMenu.setAttribute('role', 'menu');
  const item = document.createElement('button');
  closingMenu.append(item);
  document.body.append(closingMenu);
  item.focus();
  fireEvent.keyDown(item, { key: 'Escape' });
  expect(await view.findByText('Active sessions')).toBeTruthy();
  expect(new URLSearchParams(window.location.search).get('view_panel')).toBeNull();
  closingMenu.remove();
});

test('an out-of-range expression in the URL reports an invalid range instead of crashing', async () => {
  const urls: URL[] = [];
  globalThis.fetch = dashboardFetch(urls);
  window.history.replaceState(null, '', '/?from=now-999999999999M&to=now');
  const view = renderPage('org');
  expect(view.getByRole('alert').textContent).toBe('Use a time such as now-6h or 2026-01-31 08:00:00.');
  expect(view.getByRole('button', { name: 'Time range' }).textContent).toBe('Invalid time range');
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(dashboardSpans(urls)).toEqual([]);
  view.unmount();
  window.history.replaceState(null, '', '/?from=99999999999999999999&to=now');
  const chinese = renderPage('org', 'ws', 'zh-CN');
  expect(chinese.getByRole('button', { name: '时间范围' }).textContent).toBe('无效时间范围');
});

test('time picker marks only the invalid field and forgets the error when reopened', async () => {
  globalThis.fetch = dashboardFetch([]);
  const view = renderPage('org');
  expect(await view.findByText('Active sessions')).toBeTruthy();
  fireEvent.click(view.getByRole('button', { name: 'Time range' }));
  fireEvent.change(await view.findByLabelText('From'), { target: { value: 'last tuesday' } });
  fireEvent.click(view.getByRole('button', { name: 'Apply time range' }));
  expect(view.getByLabelText('From').getAttribute('aria-invalid')).toBe('true');
  expect(view.getByLabelText('To').getAttribute('aria-invalid')).toBeNull();
  fireEvent.keyDown(view.getByLabelText('From'), { key: 'Escape' });
  await waitFor(() => expect(view.queryByLabelText('From')).toBeNull());
  fireEvent.click(view.getByRole('button', { name: 'Time range' }));
  expect(((await view.findByLabelText('From')) as HTMLInputElement).value).toBe('now-7d');
  expect(view.queryByText('Use a time such as now-6h or 2026-01-31 08:00:00.')).toBeNull();
});

test('zooming out to a range without a shortcut names it as a relative span', async () => {
  globalThis.fetch = dashboardFetch([]);
  const view = renderPage('org');
  expect(await view.findByText('Active sessions')).toBeTruthy();
  fireEvent.click(view.getByRole('button', { name: 'Zoom out time range' }));
  await waitFor(() => expect(new URLSearchParams(window.location.search).get('from')).toBe('now-14d'));
  expect(view.getByRole('button', { name: 'Time range' }).textContent).toContain('Last 14 days');
  view.unmount();
  const chinese = renderPage('org', 'ws', 'zh-CN');
  await waitFor(() => expect(chinese.getByRole('button', { name: '时间范围' }).textContent).toContain('最近 14 天'));
});

test('choosing an auto refresh interval, including off, closes the menu', async () => {
  globalThis.fetch = dashboardFetch([]);
  window.history.replaceState(null, '', '/?refresh=30s');
  const view = renderPage('org');
  expect(await view.findByText('Active sessions')).toBeTruthy();
  fireEvent.click(view.getByRole('button', { name: 'Auto refresh' }));
  fireEvent.click(await view.findByRole('menuitemradio', { name: 'Off' }));
  await waitFor(() => expect(view.queryByRole('menuitemradio', { name: 'Off' })).toBeNull());
  expect(new URLSearchParams(window.location.search).get('refresh')).toBeNull();
});

test('going back to a relative range resolves it against the current time without copying router state', async () => {
  const urls: URL[] = [];
  globalThis.fetch = dashboardFetch(urls);
  window.history.replaceState({ key: 'router' }, '', '/?from=now-6h');
  const view = renderPage('org');
  await waitFor(() => expect(dashboardSpans(urls)).toEqual([6 * 3_600_000]));
  fireEvent.click(view.getByRole('button', { name: 'Time range' }));
  fireEvent.click(await view.findByRole('option', { name: 'Last 1 hour' }));
  await waitFor(() => expect(new URLSearchParams(window.location.search).get('from')).toBe('now-1h'));
  expect(window.history.state).toBeNull();
  await new Promise((resolve) => setTimeout(resolve, 30));
  const backAt = Date.now();
  window.history.replaceState(null, '', '/?from=now-6h');
  window.dispatchEvent(new window.Event('popstate'));
  await waitFor(() => expect(dashboardSpans(urls).at(-1)).toBe(6 * 3_600_000));
  expect(Date.parse(dashboardUrls(urls).at(-1)!.searchParams.get('end_time')!)).toBeGreaterThanOrEqual(backAt);
});

test('the agent filter searches the server so agents beyond the first page can be chosen', async () => {
  const urls: URL[] = [];
  const base = dashboardFetch(urls);
  const agent = (index: number) => ({ id: `agent_${index}`, name: `Agent ${index}`, version: 1 });
  const searches: string[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://localhost');
    if (url.pathname === '/v1/agents:search') {
      const { name } = JSON.parse(String(init?.body)) as { name: string };
      searches.push(name);
      return Response.json({ data: name === 'Agent 25' ? [agent(25)] : [], next_page: null });
    }
    if (url.pathname === '/v1/agents/agent_25') return Response.json(agent(25));
    if (url.pathname === '/v1/agents')
      return Response.json({ data: Array.from({ length: 20 }, (_, index) => agent(index + 1)), next_page: 'page_2' });
    return base(input);
  }) as typeof fetch;
  const view = renderPage('org');
  fireEvent.click(await view.findByRole('combobox', { name: 'Agent' }));
  expect(await view.findByRole('option', { name: /Agent 20/ })).toBeTruthy();
  expect(view.queryByRole('option', { name: /Agent 25/ })).toBeNull();
  fireEvent.change(view.getByPlaceholderText('Search agents'), { target: { value: 'Agent 25' } });
  fireEvent.click(await view.findByRole('option', { name: /Agent 25/ }));
  await waitFor(() => expect(new URLSearchParams(window.location.search).get('agent_id')).toBe('agent_25'));
  expect(searches).toEqual(['Agent 25']);
  expect(view.getByRole('combobox', { name: 'Agent' }).textContent).toContain('Agent 25');
  fireEvent.click(view.getByRole('combobox', { name: 'Agent' }));
  expect(await view.findByRole('option', { name: /Agent 20/ })).toBeTruthy();
  fireEvent.change(view.getByPlaceholderText('Search agents'), { target: { value: 'nobody' } });
  expect(view.getByRole('option', { name: /Agent 20/ })).toBeTruthy();
  expect(view.queryByText('Loading...')).toBeNull();
  expect((await view.findByRole('status', { name: '' }, { timeout: 2000 })).textContent).toBe('No agents found');
  expect(view.queryByRole('option', { name: /Agent 20/ })).toBeNull();
  expect(searches).toEqual(['Agent 25', 'nobody']);
  expect(view.getByRole('combobox', { name: 'Agent' }).textContent).toContain('Agent 25');
});
