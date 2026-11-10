import { afterEach, expect, test, mock, spyOn } from 'bun:test';
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
  mock.restore();
});

test('扫码创建前可选择配对模式，创建成功后锁定且重试保留模式', async () => {
  resetTestDom('/');
  const openWindow = spyOn(window, 'open');
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
    if (init?.method?.toUpperCase() !== 'POST') {
      if (url.endsWith('/qr')) return Response.json({ status: 'expired', url: 'https://example.com/qr' });
      return Response.json({ data: [channel] });
    }
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
  fireEvent.click(screen.getByRole('button', { name: '使用飞书扫码授权' }));
  await screen.findByText('二维码已失效，请重新生成。');
  expect(requests[1].body).toEqual(expect.objectContaining({ mode: 'pairing' }));
  expect(screen.getByRole('radio', { name: /配对模式/ }).getAttribute('aria-disabled')).toBe('true');
  fireEvent.click(screen.getByRole('radio', { name: /开放模式/ }));
  expect(pairing.getAttribute('aria-checked')).toBe('true');
  fireEvent.click(screen.getByRole('button', { name: '使用飞书扫码授权' }));
  await waitFor(() => expect(requests.filter((request) => request.url.endsWith('/qr'))).toHaveLength(2));
  expect(requests[4].url).toEndWith('/channels/ch-pairing');
  expect(requests[4].body).toEqual(expect.objectContaining({ mode: 'pairing' }));
  expect(openWindow).not.toHaveBeenCalled();
});

test('重新打开未绑定渠道时自动同步绑定结果，短暂断网后继续轮询', async () => {
  resetTestDom('/');
  let poll: (() => void) | undefined;
  spyOn(window, 'setInterval').mockImplementation((callback) => {
    poll = callback as () => void;
    return 1;
  });
  spyOn(window, 'clearInterval').mockImplementation(() => undefined);
  const channel: Channel = {
    id: 'ch-resume',
    name: '恢复授权渠道',
    mode: 'open',
    source: 'console',
    enabled: true,
    connected: false,
    binding_status: 'unbound',
    include_thinking: true,
    include_tool_calls: true,
    reply_in_thread: true,
    pending_pairings: 0,
  };
  let attempts = 0;
  globalThis.fetch = (async (input) => {
    const url = String(input).split('?')[0];
    if (url.endsWith('/channels')) {
      attempts += 1;
      if (attempts === 1) throw new TypeError('暂时断网');
      return Response.json({ data: [{ ...channel, binding_status: 'bound', app_id: 'cli_resumed' }] });
    }
    return Response.json({ data: [] });
  }) as typeof fetch;
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider initialLocale="zh-CN">
        <ChannelWizardFormPage
          mode="edit"
          workspaceId="workspace-test"
          initialAgentId="agent-test"
          initialChannel={channel}
          initialValue={{ ...channel, agentId: 'agent-test', environmentId: 'env-test', channelType: 'feishu' }}
          onNavigate={() => undefined}
          title="编辑渠道"
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  await screen.findByRole('alert');
  expect(screen.getByRole('button', { name: '下一步' }).hasAttribute('disabled')).toBe(true);
  poll!();
  await screen.findByText('飞书绑定成功');
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.getByRole('button', { name: '下一步' }).hasAttribute('disabled')).toBe(false);
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: '手动输入' }));
  expect(screen.getByDisplayValue('cli_resumed')).toBeTruthy();
  expect(screen.getByRole('button', { name: '下一步' }).hasAttribute('disabled')).toBe(false);
});

test.each([true, false])('打开飞书授权窗口，注册成功=%s', async (success) => {
  resetTestDom('/');
  const replace = mock(() => undefined);
  const close = mock(() => undefined);
  const popup = { location: { replace }, close, opener: window };
  const openWindow = spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
  const authorizationUrl = 'https://open.feishu.cn/page/launcher?ticket=test';
  globalThis.fetch = (async (input, init) => {
    if (init?.method?.toUpperCase() !== 'POST') {
      if (String(input).includes('/qr')) return Response.json({ status: 'pending', url: authorizationUrl });
      return Response.json({ data: [] });
    }
    expect(openWindow).toHaveBeenCalledWith('about:blank', '_blank');
    if (!success) return Response.json({ error: { message: '注册失败' } }, { status: 500 });
    if (String(input).includes('/qr')) return Response.json({ status: 'pending', url: authorizationUrl });
    return Response.json({ id: 'ch-window', binding_status: 'unbound' });
  }) as typeof fetch;
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider initialLocale="zh-CN">
        <ChannelWizardFormPage
          mode="create"
          workspaceId="workspace-test"
          initialAgentId="agent-test"
          initialChannel={null}
          initialValue={{
            name: '窗口授权渠道',
            mode: 'open',
            include_thinking: true,
            include_tool_calls: true,
            reply_in_thread: true,
            agentId: 'agent-test',
            environmentId: 'env-test',
            channelType: 'feishu',
          }}
          onNavigate={() => undefined}
          title="创建渠道"
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: '前往飞书授权创建' }));
  if (success) {
    await waitFor(() => expect(replace).toHaveBeenCalledWith(authorizationUrl));
    expect(close).not.toHaveBeenCalled();
    expect(screen.getByText('等待飞书扫码确认')).toBeTruthy();
  } else {
    await waitFor(() => expect(close).toHaveBeenCalled());
    expect(replace).not.toHaveBeenCalled();
    expect(screen.getByRole('alert')).toBeTruthy();
  }
  expect(popup.opener).toBeNull();
});
