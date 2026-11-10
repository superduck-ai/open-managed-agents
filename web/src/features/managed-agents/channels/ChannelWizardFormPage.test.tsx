import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import type { Channel } from './api';

const { render, screen, fireEvent, cleanup, waitFor } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { I18nProvider } = await import('../../../shared/i18n');
const { ChannelWizardFormPage } = await import('./ChannelWizardFormPage');

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('扫码创建前可选择配对模式，创建成功后锁定且重试保留模式', async () => {
  resetTestDom('/');
  const requests: { url: string; body: unknown }[] = [];
  const channel: Channel = {
    id: 'ch-pairing',
    name: '配对扫码渠道',
    mode: 'pairing',
    source: 'console',
    enabled: true,
    connected: false,
    binding_status: 'unbound',
    include_thinking: true,
    include_tool_calls: true,
    reply_in_thread: true,
    pending_pairings: 0,
  };
  globalThis.fetch = (async (input, init) => {
    const url = String(input).split('?')[0];
    if (init?.method?.toUpperCase() !== 'POST') return Response.json({ data: [] });
    requests.push({ url, body: JSON.parse(String(init.body)) });
    if (url.endsWith('/qr')) return Response.json({ status: 'expired', url: 'https://example.com/qr' });
    if (url.endsWith('/settings')) return Response.json({ environment_id: 'env-test' });
    return Response.json(channel);
  }) as typeof fetch;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="zh-CN">
        <ChannelWizardFormPage
          mode="create"
          workspaceId="workspace-test"
          initialAgentId="agent-test"
          initialChannel={null}
          initialValue={{ ...channel, agentId: 'agent-test', environmentId: 'env-test', channelType: 'feishu' }}
          onNavigate={() => undefined}
          title="创建渠道"
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  const pairing = screen.getByRole('radio', { name: /配对模式/ });
  fireEvent.click(screen.getByRole('radio', { name: /开放模式/ }));
  fireEvent.click(pairing);
  expect(pairing.getAttribute('aria-checked')).toBe('true');
  expect(screen.getByRole('button', { name: '下一步' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '前往飞书授权创建' }));
  await screen.findByText('二维码已失效，请重新生成。');
  expect(requests[1].body).toEqual(expect.objectContaining({ mode: 'pairing' }));
  expect(screen.getByRole('radio', { name: /配对模式/ }).getAttribute('aria-disabled')).toBe('true');
  fireEvent.click(screen.getByRole('radio', { name: /开放模式/ }));
  expect(pairing.getAttribute('aria-checked')).toBe('true');
  fireEvent.click(screen.getByRole('button', { name: '重新生成二维码' }));
  await waitFor(() => expect(requests.filter((request) => request.url.endsWith('/qr'))).toHaveLength(2));
  expect(requests[4].url).toEndWith('/channels/ch-pairing');
  expect(requests[4].body).toEqual(expect.objectContaining({ mode: 'pairing' }));
});
