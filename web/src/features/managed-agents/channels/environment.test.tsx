import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
const { render, screen, fireEvent, waitFor, cleanup } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { I18nProvider } = await import('../../../shared/i18n');
const { ChannelEnvironment } = await import('./ChannelEnvironment');
const { setConsoleRequestContext } = await import('../../../shared/api/client');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});
function mount(canEdit = true) {
  resetTestDom('/');
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider initialLocale="zh-CN">
        <ChannelEnvironment agentId="agent-a" workspaceId="workspace-a" canEdit={canEdit} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}
test('保存失败保留原环境，展示错误', async () => {
  globalThis.fetch = (async (input, init) => {
    if (String(input).includes('/environments'))
      return Response.json({ data: [{ id: 'env_a', name: '测试环境' }], has_more: false });
    if (init?.method === 'POST') return Response.json({ error: { message: '环境已归档' } }, { status: 400 });
    return Response.json({ environment_id: null });
  }) as typeof fetch;
  mount();
  const select = await screen.findByRole('combobox');
  await waitFor(() => expect(select.hasAttribute('disabled')).toBe(false));
  fireEvent.click(select);
  fireEvent.click(await screen.findByRole('option', { name: '测试环境' }));
  expect(await screen.findByText('环境已归档')).toBeTruthy();
  expect(select.textContent).toContain('选择环境');
});
test('仅展示可选环境并携带工作区和 CSRF保存选择', async () => {
  const posted: unknown[] = [];
  globalThis.fetch = (async (input, init) => {
    if (String(input).includes('/environments'))
      return Response.json({ data: [{ id: 'env_a', name: '测试环境' }], has_more: false });
    if (init?.method === 'POST') {
      const headers = new Headers(init.headers);
      expect(headers.get('x-workspace-id')).toBe('workspace-a');
      expect(headers.get('x-csrf-token')).toBe('csrf-a');
      const body = JSON.parse(String(init.body));
      posted.push(body);
      return Response.json(body);
    }
    return Response.json({ environment_id: null });
  }) as typeof fetch;
  setConsoleRequestContext({ csrfToken: 'csrf-a' });
  mount();
  const select = await screen.findByRole('combobox');
  await waitFor(() => expect(select.hasAttribute('disabled')).toBe(false));
  fireEvent.click(select);
  expect((await screen.findByRole('link', { name: '前往环境管理' })).getAttribute('href')).toBe(
    '/workspaces/workspace-a/environments',
  );
  expect(screen.queryByRole('option', { name: '使用预设或默认环境' })).toBeNull();
  fireEvent.click(await screen.findByRole('option', { name: '测试环境' }));
  await waitFor(() => expect(select.textContent).toContain('测试环境'));
  expect(posted).toEqual([{ environment_id: 'env_a' }]);
});

test('加载后续分页并在只读模式禁止修改', async () => {
  const pages: string[] = [];
  globalThis.fetch = (async (input) => {
    const url = new URL(String(input), 'http://localhost');
    if (url.pathname.includes('/environments')) {
      const page = url.searchParams.get('page') ?? '';
      pages.push(page);
      return Response.json(
        page
          ? { data: [{ id: 'env_b', name: '环境 B' }], next_page: null }
          : { data: [{ id: 'env_a', name: '环境 A' }], next_page: 'cursor-b' },
      );
    }
    return Response.json({ environment_id: 'env_b' });
  }) as typeof fetch;
  mount(false);
  const select = await screen.findByRole('combobox');
  await waitFor(() => expect(select.textContent).toContain('环境 B'));
  expect(pages).toEqual(['', 'cursor-b']);
  expect(select.hasAttribute('disabled')).toBe(true);
});
