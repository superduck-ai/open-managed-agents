import { afterEach, expect, test, mock, spyOn } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import type { Channel } from './api';
import type { WizardValue } from './ChannelWizardForm';

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
const channel: Channel = {
  id: 'ch-test',
  name: '测试渠道',
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
const initialValue: WizardValue = { ...channel, channelType: 'feishu', agentId: '', environmentId: '' };
const authorizationUrl = 'https://open.feishu.cn/page/launcher?ticket=test';
function mount(saved: Channel | null = null, value = initialValue) {
  const navigated = mock(() => undefined);
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider initialLocale="zh-CN">
        <ChannelWizardFormPage
          mode={saved ? 'edit' : 'create'}
          workspaceId="workspace-test"
          initialAgentId={saved ? 'agent-test' : ''}
          initialChannel={saved}
          initialValue={value}
          onNavigate={navigated}
          title="渠道向导"
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return navigated;
}
function targets(url: string) {
  if (url.includes('/environments')) return { data: [{ id: 'env-test', name: '测试环境' }] };
  return { data: [{ id: 'agent-test', name: '测试助手' }] };
}
async function selectTargets() {
  fireEvent.click(await screen.findByText('选择智能体'));
  fireEvent.click(await screen.findByRole('option', { name: '测试助手 (agent-test)' }));
  fireEvent.click(screen.getByText('选择环境'));
  fireEvent.click(await screen.findByRole('option', { name: '测试环境' }));
}
function popupMock() {
  const popup = {
    location: { replace: mock(() => undefined) },
    close: mock(() => undefined),
    opener: window,
    document: { title: '', body: { textContent: '', style: { cssText: '' } } },
  };
  spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
  return popup;
}

test('授权失败关闭等待窗口，不创建渠道或保存运行环境', async () => {
  resetTestDom('/');
  const popup = popupMock();
  const posts: string[] = [];
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    if (init?.method === 'POST') {
      posts.push(url);
      return Response.json({ error: { message: '注册失败' } }, { status: 500 });
    }
    return Response.json(targets(url));
  }) as typeof fetch;
  mount();
  fireEvent.click(screen.getByRole('button', { name: '前往飞书授权创建' }));
  await waitFor(() => expect(popup.close).toHaveBeenCalled());
  expect(popup.document.body.textContent).toBe('正在打开飞书授权…');
  expect(posts).toHaveLength(1);
  expect(posts[0]).toContain('/v1/channels/authorizations');
  expect(screen.getByRole('alert')).toBeTruthy();
});

test('第一步独立完成授权，第三步选择目标后才能保存正式渠道', async () => {
  resetTestDom('/');
  const popup = popupMock();
  let poll: (() => void) | undefined;
  spyOn(window, 'setInterval').mockImplementation((callback) => {
    poll = callback as () => void;
    return 1;
  });
  spyOn(window, 'clearInterval').mockImplementation(() => undefined);
  const posts: { url: string; body: Record<string, unknown> }[] = [];
  let bound = false;
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    if (init?.method === 'POST') posts.push({ url, body: JSON.parse(String(init.body)) });
    if (url.includes('/v1/channels/authorizations'))
      return Response.json({
        id: 'authorization-test',
        status: bound ? 'bound' : 'pending',
        url: authorizationUrl,
        app_id: bound ? 'cli_authorized' : undefined,
      });
    if (init?.method === 'POST')
      return Response.json({ ...channel, binding_status: 'bound', app_id: 'cli_authorized' });
    return Response.json(targets(url));
  }) as typeof fetch;
  const navigated = mount();
  expect(screen.queryByText('选择智能体')).toBeNull();
  expect(screen.queryByText('选择环境')).toBeNull();
  expect(screen.queryByRole('radiogroup', { name: '渠道模式' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '前往飞书授权创建' }));
  await screen.findByText('等待飞书授权中');
  expect(popup.location.replace).toHaveBeenCalledWith(authorizationUrl);
  expect(screen.getByRole('link', { name: '重新打开授权页' }).getAttribute('href')).toBe(authorizationUrl);
  expect(screen.queryByText('等待飞书扫码确认')).toBeNull();
  expect(posts).toHaveLength(1);
  expect(posts[0].body).toEqual({ name: '测试渠道' });
  expect(screen.getByRole('button', { name: '下一步' }).hasAttribute('disabled')).toBe(true);
  bound = true;
  poll!();
  await screen.findByText('飞书绑定成功');
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(true);
  await selectTargets();
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(posts[1].url).toContain('/agents/agent-test/channels/settings');
  expect(posts[1].body).toEqual({ environment_id: 'env-test' });
  expect(posts[2].body).toEqual(expect.objectContaining({ authorization_id: 'authorization-test', mode: 'open' }));
  expect(posts[2].body.app_secret).toBeUndefined();
});

test('手动凭据在第一步输入，扫码入口不打开新窗口', async () => {
  resetTestDom('/');
  const open = spyOn(window, 'open');
  globalThis.fetch = (async (input) =>
    String(input).includes('/authorizations')
      ? Response.json({ id: 'authorization-test', status: 'pending', url: authorizationUrl })
      : Response.json(targets(String(input)))) as typeof fetch;
  mount();
  fireEvent.click(screen.getByRole('button', { name: '手动输入' }));
  fireEvent.change(screen.getByPlaceholderText('应用 ID'), { target: { value: 'cli_manual' } });
  fireEvent.change(screen.getByPlaceholderText('应用密钥'), { target: { value: 'manual-secret' } });
  expect(screen.getByRole('button', { name: '下一步' }).hasAttribute('disabled')).toBe(false);
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '飞书授权' }));
  fireEvent.click(screen.getByRole('button', { name: '使用飞书扫码授权' }));
  await screen.findByText('等待飞书扫码确认');
  expect(open).not.toHaveBeenCalled();
});

test('已有未绑定渠道在短暂断网后继续同步完整绑定状态', async () => {
  resetTestDom('/');
  let poll: (() => void) | undefined;
  spyOn(window, 'setInterval').mockImplementation((callback) => {
    poll = callback as () => void;
    return 1;
  });
  spyOn(window, 'clearInterval').mockImplementation(() => undefined);
  let attempts = 0;
  globalThis.fetch = (async (input) => {
    const url = String(input).split('?')[0];
    if (url.endsWith('/channels')) {
      attempts += 1;
      if (attempts === 1) throw new TypeError('暂时断网');
      return Response.json({ data: [{ ...channel, binding_status: 'bound', app_id: 'cli_resumed' }] });
    }
    return Response.json(targets(url));
  }) as typeof fetch;
  mount(channel, { ...initialValue, agentId: 'agent-test', environmentId: 'env-test' });
  await screen.findByRole('alert');
  poll!();
  await screen.findByText('飞书绑定成功');
  expect(screen.queryByRole('alert')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '手动输入' }));
  expect(screen.getByDisplayValue('cli_resumed')).toBeTruthy();
});
