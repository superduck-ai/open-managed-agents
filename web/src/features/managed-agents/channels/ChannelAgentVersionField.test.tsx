import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';

const { render, screen, fireEvent, cleanup, waitFor } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { I18nProvider } = await import('../../../shared/i18n');
const { ChannelAgentVersionField } = await import('./ChannelAgentVersionField');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function mount(value: number | null, onChange = mock(() => undefined)) {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider initialLocale="zh-CN">
        <ChannelAgentVersionField workspaceId="workspace-test" agentId="agent-test" value={value} onChange={onChange} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return onChange;
}

test('版本读取失败保留已保存的固定版本并展示错误', async () => {
  resetTestDom('/');
  globalThis.fetch = (async () =>
    Response.json({ error: { message: '版本不可访问' } }, { status: 403 })) as typeof fetch;
  mount(7);
  await screen.findByRole('alert');
  expect(screen.getByRole('combobox', { name: '智能体版本' }).textContent).toContain('v7');
});

test('读取全部版本页并可选择固定版本或恢复默认最新', async () => {
  resetTestDom('/');
  const requests: string[] = [];
  globalThis.fetch = (async (input) => {
    const url = String(input);
    requests.push(url);
    return Response.json(
      url.includes('page=older')
        ? { data: [{ version: 2 }], next_page: null }
        : { data: [{ version: 3 }], next_page: 'older' },
    );
  }) as typeof fetch;
  const changed = mount(null);
  const select = screen.getByRole('combobox', { name: '智能体版本' });
  await waitFor(() => expect(select.hasAttribute('disabled')).toBe(false));
  fireEvent.click(select);
  fireEvent.click(await screen.findByRole('option', { name: 'v2' }));
  expect(changed).toHaveBeenLastCalledWith(2);
  fireEvent.click(select);
  fireEvent.click(await screen.findByRole('option', { name: '默认最新' }));
  expect(changed).toHaveBeenLastCalledWith(null);
  expect(requests).toHaveLength(2);
  expect(requests.every((url) => url.includes('/agents/agent-test/versions'))).toBe(true);
});
