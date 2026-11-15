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
function mount(
  saved: Channel | null = null,
  value = initialValue,
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
) {
  const navigated = mock(() => undefined);
  render(
    <QueryClientProvider client={client}>
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
  if (url.includes('/memory_stores')) return { data: [{ id: 'mem-test', name: '测试记忆' }] };
  if (url.includes('/vaults')) return { data: [{ id: 'vault-test', display_name: '测试凭证库' }] };
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
  fireEvent.click(await screen.findByRole('combobox', { name: '默认 Memory' }));
  fireEvent.click(await screen.findByRole('option', { name: '测试记忆' }));
  fireEvent.click(screen.getByRole('combobox', { name: '默认 Vault' }));
  fireEvent.click(await screen.findByRole('option', { name: '测试凭证库' }));
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(posts[1].url).toContain('/agents/agent-test/channels/settings');
  expect(posts[1].body).toEqual({ environment_id: 'env-test' });
  expect(posts[2].body).toEqual(
    expect.objectContaining({
      authorization_id: 'authorization-test',
      mode: 'open',
      memory_store_id: 'mem-test',
      vault_id: 'vault-test',
    }),
  );
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

test('编辑回显默认资源，并可分别切回不挂载后保存', async () => {
  resetTestDom('/');
  let body: Record<string, unknown> = {};
  globalThis.fetch = (async (input, init) => {
    if (init?.method === 'POST') {
      body = JSON.parse(String(init.body));
      return Response.json({ ...channel, ...body, binding_status: 'bound' });
    }
    return Response.json(targets(String(input)));
  }) as typeof fetch;
  const saved = {
    ...channel,
    binding_status: 'bound' as const,
    app_id: 'cli_test',
    memory_store_id: 'mem-test',
    vault_id: 'vault-test',
  };
  const navigated = mount(saved, { ...initialValue, ...saved, agentId: 'agent-test', environmentId: 'env-test' });
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  await screen.findByText('测试记忆');
  await screen.findByText('测试凭证库');
  for (const name of ['默认 Memory', '默认 Vault']) {
    fireEvent.click(screen.getByRole('combobox', { name }));
    fireEvent.click(await screen.findByRole('option', { name: '不挂载' }));
  }
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(body).toEqual(expect.objectContaining({ memory_store_id: null, vault_id: null }));
});

test('授权替换应用 ID 时清除旧密钥，手动路径需要重新填写', async () => {
  resetTestDom('/');
  let poll!: () => void;
  spyOn(window, 'setInterval').mockImplementation((callback) => {
    poll = callback as () => void;
    return 1;
  });
  spyOn(window, 'clearInterval').mockImplementation(() => undefined);
  globalThis.fetch = (async (input, init) =>
    String(input).includes('/authorizations')
      ? Response.json({
          id: 'auth-new',
          status: init?.method === 'POST' ? 'pending' : 'bound',
          app_id: 'cli_new',
          url: authorizationUrl,
        })
      : Response.json(targets(String(input)))) as typeof fetch;
  mount(null, { ...initialValue, app_id: 'cli_old', app_secret: 'old-secret' });
  fireEvent.click(screen.getByRole('button', { name: '使用飞书扫码授权' }));
  await screen.findByText('等待飞书扫码确认');
  poll();
  await screen.findByText('飞书绑定成功');
  fireEvent.click(screen.getByRole('button', { name: '手动输入' }));
  expect(screen.getByDisplayValue('cli_new')).toBeTruthy();
  expect((screen.getByPlaceholderText('应用密钥') as HTMLInputElement).value).toBe('');
  expect(screen.getByRole('button', { name: '下一步' }).hasAttribute('disabled')).toBe(true);
});

test('手动验证密钥与保存使用相同的首尾空白处理', async () => {
  resetTestDom('/');
  let body: Record<string, unknown> = {};
  globalThis.fetch = (async (input, init) => {
    if (init?.method === 'POST') {
      body = JSON.parse(String(init.body));
      return Response.json({ ok: true });
    }
    return Response.json(targets(String(input)));
  }) as typeof fetch;
  mount();
  fireEvent.click(screen.getByRole('button', { name: '手动输入' }));
  fireEvent.change(screen.getByPlaceholderText('应用 ID'), { target: { value: ' cli_manual ' } });
  fireEvent.change(screen.getByPlaceholderText('应用密钥'), { target: { value: ' secret-value ' } });
  fireEvent.click(screen.getByRole('button', { name: '验证 App Secret' }));
  await screen.findByText('应用密钥验证通过');
  expect(body).toEqual({ app_id: 'cli_manual', app_secret: 'secret-value' });
});

test('未填写名称时两个授权入口均不可点击', () => {
  resetTestDom('/');
  globalThis.fetch = (async (input) => Response.json(targets(String(input)))) as typeof fetch;
  mount(null, { ...initialValue, name: '' });
  for (const name of ['前往飞书授权创建', '使用飞书扫码授权']) {
    expect(screen.getByRole('button', { name }).hasAttribute('disabled')).toBe(true);
  }
});

test('旧渠道轮询在仍等待绑定时清除网络错误，手动输入和过期状态下停止', async () => {
  resetTestDom('/');
  const timers = new Map<number, () => void>();
  let nextId = 0;
  spyOn(window, 'setInterval').mockImplementation((callback) => {
    timers.set(++nextId, callback as () => void);
    return nextId;
  });
  spyOn(window, 'clearInterval').mockImplementation((id) => {
    timers.delete(Number(id));
  });
  let reads = 0;
  let status = 'pending';
  globalThis.fetch = (async (input) => {
    const url = String(input).split('?')[0];
    if (url.endsWith('/channels')) {
      if (++reads === 1) throw new TypeError('短暂断网');
      return Response.json({ data: [channel] });
    }
    if (url.endsWith('/qr')) return Response.json({ status, url: authorizationUrl });
    return Response.json(targets(url));
  }) as typeof fetch;
  mount(channel, { ...initialValue, agentId: 'agent-test', environmentId: 'env-test' });
  await screen.findByRole('alert');
  for (const tick of [...timers.values()]) tick();
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: '手动输入' }));
  expect(timers.size).toBe(0);
  status = 'expired';
  fireEvent.click(screen.getByRole('button', { name: '飞书授权' }));
  await screen.findByText('二维码已失效，请重新生成。');
  expect(timers.size).toBe(0);
});

test('第三步可选挂载 Git，未填完整不能保存，移除后恢复可保存', async () => {
  resetTestDom('/');
  let body: Record<string, unknown> = {};
  globalThis.fetch = (async (input, init) => {
    if (init?.method === 'POST') {
      body = JSON.parse(String(init.body));
      return Response.json({ ...channel, ...body, binding_status: 'bound' });
    }
    return Response.json(targets(String(input)));
  }) as typeof fetch;
  const saved = { ...channel, binding_status: 'bound' as const, app_id: 'cli_test' };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const navigated = mount(
    saved,
    { ...initialValue, ...saved, agentId: 'agent-test', environmentId: 'env-test' },
    client,
  );
  expect(screen.queryByRole('button', { name: '添加 Git 仓库' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '添加 Git 仓库' }));
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(true);
  fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://github.com/octocat/Hello-World.git' } });
  fireEvent.change(screen.getByLabelText('挂载路径（可选）'), { target: { value: '/workspace/demo' } });
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: '移除 Git 仓库 1' }));
  expect(screen.queryByLabelText('URL')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '添加 Git 仓库' }));
  fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://github.com/octocat/Hello-World.git' } });
  fireEvent.change(screen.getByLabelText('授权令牌（公开仓库可不填）'), { target: { value: 'test-token' } });
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(client.getQueryData(['channel-wizard', 'workspace-test', 'agent-test', channel.id])).toMatchObject({
    channel: { git_repository: { url: 'https://github.com/octocat/Hello-World.git' } },
    environmentId: 'env-test',
  });
  expect(body.git_repository).toEqual({
    type: 'github_repository',
    url: 'https://github.com/octocat/Hello-World.git',
    authorization_token: 'test-token',
  });
});

test.each(['agents', 'environments'])('向导展示 %s 选项查询失败', async (resource) => {
  resetTestDom('/');
  globalThis.fetch = (async (input) => {
    const url = String(input);
    if (url.includes(`/v1/${resource}?`))
      return Response.json({ error: { type: 'api_error', message: `${resource} 查询失败` } }, { status: 503 });
    return Response.json(targets(url));
  }) as typeof fetch;
  mount();
  expect((await screen.findByRole('alert')).textContent).toContain(`${resource} 查询失败`);
});

test('切到手动接入立即清除旧渠道轮询错误', async () => {
  resetTestDom('/');
  globalThis.fetch = (async (input) => {
    const url = String(input).split('?')[0];
    if (url.endsWith('/channels'))
      return Response.json({ error: { type: 'api_error', message: '旧渠道暂时不可达' } }, { status: 503 });
    return Response.json(targets(url));
  }) as typeof fetch;
  mount(channel, { ...initialValue, agentId: 'agent-test', environmentId: 'env-test' });
  expect((await screen.findByRole('alert')).textContent).toContain('旧渠道暂时不可达');
  fireEvent.click(screen.getByRole('button', { name: '手动输入' }));
  expect(screen.queryByRole('alert')).toBeNull();
});

test('已有 Git 渠道打开编辑后原样保存，保留检出与路径且不回传存量令牌', async () => {
  resetTestDom('/');
  const { ChannelWizardContent } = await import('./ChannelWizardPage');
  const gitRepository = {
    type: 'github_repository',
    url: 'https://github.com/octocat/Hello-World.git',
    checkout: { type: 'branch', name: 'master' },
    mount_path: '/workspace/demo',
    tokenConfigured: true,
  };
  const saved = { ...channel, binding_status: 'bound' as const, app_id: 'cli_test', git_repository: gitRepository };
  const navigated = mock(() => undefined);
  let body: Record<string, unknown> = {};
  globalThis.fetch = (async (input, init) => {
    const url = String(input).split('?')[0];
    if (init?.method === 'POST') {
      body = JSON.parse(String(init.body));
      return Response.json(saved);
    }
    if (url.endsWith('/channels')) return Response.json({ data: [saved] });
    if (url.endsWith('/settings')) return Response.json({ environment_id: 'env-test' });
    return Response.json(targets(url));
  }) as typeof fetch;
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider initialLocale="zh-CN">
        <ChannelWizardContent
          mode="edit"
          workspaceId="workspace-test"
          initialAgentId="agent-test"
          channelId={saved.id}
          onNavigate={navigated}
          title="编辑渠道"
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(await screen.findByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  expect((screen.getByLabelText('URL') as HTMLInputElement).value).toBe(gitRepository.url);
  expect((screen.getByLabelText('分支名称') as HTMLInputElement).value).toBe('master');
  expect((screen.getByLabelText('挂载路径（可选）') as HTMLInputElement).value).toBe('/workspace/demo');
  expect((screen.getByPlaceholderText('已配置，留空保持不变') as HTMLInputElement).value).toBe('');
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  const { tokenConfigured: _configured, ...expected } = gitRepository;
  expect(body.git_repository).toEqual(expected);
});
