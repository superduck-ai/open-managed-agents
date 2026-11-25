import { afterEach, expect, test, mock, spyOn } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import type { Channel, ChannelResource } from './api';
import type { WizardValue } from './ChannelWizardForm';
import { resourceFormValues } from '../resources/git-resource';
import type { Locale } from '../../../shared/i18n';

const { render, screen, fireEvent, cleanup, waitFor, within } = await import('@testing-library/react');
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
const initialValue: WizardValue = {
  ...channel,
  ...resourceFormValues([]),
  channelType: 'feishu',
  agentId: '',
  environmentId: '',
};
const authorizationUrl = 'https://open.feishu.cn/page/launcher?ticket=test';
function mount(
  saved: Channel | null = null,
  value = initialValue,
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  locale: Locale = 'zh-CN',
) {
  const navigated = mock(() => undefined);
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale={locale}>
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
  expect(screen.queryByRole('button', { name: '保存配置' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  expect(screen.queryByRole('button', { name: '保存配置' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(true);
  await selectTargets();
  fireEvent.click(await screen.findByRole('combobox', { name: '默认 Memory' }));
  fireEvent.click(await screen.findByRole('option', { name: '测试记忆' }));
  fireEvent.click(screen.getByRole('combobox', { name: '默认 Vault' }));
  fireEvent.click(await screen.findByRole('option', { name: /测试凭证库/ }));
  fireEvent.click(screen.getByRole('combobox', { name: '默认 Vault' }));
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(posts).toHaveLength(2);
  expect(posts[1].url).not.toContain('/settings');
  expect(posts[1].body).toEqual(
    expect.objectContaining({
      authorization_id: 'authorization-test',
      environment_id: 'env-test',
      mode: 'open',
      memory_store_id: 'mem-test',
      vault_ids: ['vault-test'],
    }),
  );
  expect(posts[1].body.app_secret).toBeUndefined();
});

test('手动凭据在第一步输入，扫码入口不打开新窗口', async () => {
  resetTestDom('/');
  const open = spyOn(window, 'open');
  globalThis.fetch = (async (input) =>
    String(input).includes('/authorizations')
      ? Response.json({ id: 'authorization-test', status: 'pending', url: authorizationUrl })
      : Response.json(targets(String(input)))) as typeof fetch;
  mount();
  const authorization = screen.getByRole('radio', { name: '飞书授权', checked: true });
  const credentials = screen.getByRole('radio', { name: '手动输入', checked: false });
  fireEvent.click(credentials);
  expect(credentials.getAttribute('aria-checked')).toBe('true');
  expect(authorization.getAttribute('aria-checked')).toBe('false');
  fireEvent.change(screen.getByPlaceholderText('应用 ID'), { target: { value: 'cli_manual' } });
  fireEvent.change(screen.getByPlaceholderText('应用密钥'), { target: { value: 'manual-secret' } });
  expect(screen.getByRole('button', { name: '下一步' }).hasAttribute('disabled')).toBe(false);
  expect(screen.queryByRole('button', { name: '保存配置' })).toBeNull();
  fireEvent.click(authorization);
  expect(authorization.getAttribute('aria-checked')).toBe('true');
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
  fireEvent.click(screen.getByRole('radio', { name: '手动输入' }));
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
    agent_version: 2,
    memory_store_id: 'mem-test',
    vault_id: 'vault-test',
  };
  const navigated = mount(saved, { ...initialValue, ...saved, agentId: 'agent-test', environmentId: 'env-test' });
  const authorization = screen.getByRole('radio', { name: '飞书授权', checked: false });
  expect(authorization.getAttribute('aria-disabled')).toBe('true');
  fireEvent.click(authorization);
  expect(screen.getByRole('radio', { name: '手动输入', checked: true })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  const side = within(screen.getByRole('complementary', { name: '进度' }));
  await side.findByText('测试记忆');
  await side.findByText('测试凭证库');
  fireEvent.click(screen.getByRole('combobox', { name: '默认 Memory' }));
  fireEvent.click(await screen.findByRole('option', { name: '不挂载' }));
  fireEvent.click(screen.getByRole('combobox', { name: '默认 Vault' }));
  fireEvent.click(await screen.findByRole('option', { name: /测试凭证库/ }));
  fireEvent.click(screen.getByRole('combobox', { name: '默认 Vault' }));
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(body).toEqual(expect.objectContaining({ agent_version: 2, memory_store_id: null, vault_ids: [] }));
});

test('资源卡片按资源类型使用与菜单一致的图标', async () => {
  resetTestDom('/');
  globalThis.fetch = (async (input) => Response.json(targets(String(input)))) as typeof fetch;
  mount(null, {
    ...initialValue,
    agentId: 'agent-test',
    environmentId: 'env-test',
    memory_store_id: 'mem-test',
    vault_id: 'vault-test',
    gitResources: [
      {
        url: 'https://git.example.com/team/knowledge.git',
        authorizationToken: '',
        checkoutType: '',
        checkoutValue: '',
        mountPath: '/workspace/knowledge',
      },
    ],
  });
  const side = within(await screen.findByRole('complementary', { name: '进度' }));
  const expected = [
    { kind: 'environment', label: '测试环境', icon: 'lucide-server' },
    { kind: 'memory', label: '测试记忆', icon: 'lucide-brain' },
    { kind: 'vault', label: '测试凭证库', icon: 'lucide-vault' },
    { kind: 'git', label: 'https://git.example.com/team/knowledge.git', icon: 'lucide-git-branch' },
  ];
  for (const item of expected) {
    const row = (await side.findByText(item.label)).closest('[data-resource-kind]');
    if (!row || row.getAttribute('data-resource-kind') !== item.kind) {
      throw new Error(`资源卡片缺少 ${item.kind} 行`);
    }
    if (!row.querySelector(`svg.${item.icon}`)) {
      throw new Error(`${item.kind} 行的图标应与菜单一致（${item.icon}）`);
    }
  }
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
  fireEvent.click(screen.getByRole('radio', { name: '手动输入' }));
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
  fireEvent.click(screen.getByRole('radio', { name: '手动输入' }));
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
  fireEvent.click(screen.getByRole('radio', { name: '手动输入' }));
  expect(timers.size).toBe(0);
  status = 'expired';
  fireEvent.click(screen.getByRole('radio', { name: '飞书授权' }));
  await screen.findByText('二维码已失效，请重新生成。');
  await waitFor(() => expect(timers.size).toBe(0));
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
  expect(screen.queryByRole('button', { name: '添加资源' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '添加资源' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Git 仓库' }));
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(true);
  fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://github.com/octocat/Hello-World.git' } });
  fireEvent.change(screen.getByLabelText('挂载路径（可选）'), { target: { value: '/workspace/demo' } });
  expect(screen.getByRole('button', { name: '保存配置' }).hasAttribute('disabled')).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: '移除 Git 仓库 1' }));
  expect(screen.queryByLabelText('URL')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '添加资源' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Git 仓库' }));
  fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://github.com/octocat/Hello-World.git' } });
  fireEvent.change(screen.getByLabelText('授权令牌（公开仓库可不填）'), { target: { value: 'test-token' } });
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(client.getQueryData(['channel-wizard', 'workspace-test', 'agent-test', channel.id])).toMatchObject({
    channel: { resources: [{ url: 'https://github.com/octocat/Hello-World.git' }] },
    environmentId: 'env-test',
  });
  expect(body.resources).toEqual([
    {
      type: 'github_repository',
      url: 'https://github.com/octocat/Hello-World.git',
      authorization_token: 'test-token',
    },
  ]);
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
  fireEvent.click(screen.getByRole('radio', { name: '手动输入' }));
  expect(screen.queryByRole('alert')).toBeNull();
});

test.each([true, false])('已有 Git 渠道编辑保存保留检出、路径与令牌状态（响应含类型：%s）', async (hasType) => {
  resetTestDom('/');
  const { ChannelWizardContent } = await import('./ChannelWizardPage');
  const gitRepository = {
    ...(hasType ? { type: 'github_repository' } : {}),
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
  expect(body.resources).toEqual([{ ...expected, type: 'github_repository' }]);
  expect(body.git_repository).toBeUndefined();
});

test.each([
  { mode: 'pairing', thinking: false, tools: false },
  { mode: 'open', thinking: true, tools: false },
  { mode: 'open', thinking: false, tools: true },
] as const)('策略控件独立保存配对模式、思考和工具展示：%j', async ({ mode, thinking, tools }) => {
  resetTestDom('/');
  const saved = { ...channel, binding_status: 'bound' as const, app_id: 'cli_test' };
  let body: Record<string, unknown> = {};
  globalThis.fetch = (async (input, init) => {
    if (init?.method === 'POST') {
      body = JSON.parse(String(init.body));
      return Response.json({ ...saved, ...body });
    }
    return Response.json(targets(String(input)));
  }) as typeof fetch;
  const navigated = mount(null, {
    ...initialValue,
    app_id: saved.app_id,
    app_secret: 'test_secret',
    agentId: 'agent-test',
    environmentId: 'env-test',
  });
  fireEvent.click(await screen.findByRole('radio', { name: '手动输入' }));
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  if (mode === 'pairing') {
    fireEvent.click(screen.getByLabelText('渠道模式'));
    fireEvent.click(await screen.findByRole('option', { name: '配对模式' }));
  }
  if (!thinking) fireEvent.click(screen.getByRole('switch', { name: '展示思考过程' }));
  if (!tools) fireEvent.click(screen.getByRole('switch', { name: '展示工具调用' }));
  expect(screen.queryByText('User')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(body.mode).toBe(mode);
  expect(body.include_thinking).toBe(thinking);
  expect(body.include_tool_calls).toBe(tools);
});

test('已有渠道模式只读，避免编辑时提交后端禁止的模式变更', async () => {
  resetTestDom('/');
  globalThis.fetch = (async (input) => Response.json(targets(String(input)))) as typeof fetch;
  mount(
    { ...channel, binding_status: 'bound', app_id: 'cli_test' },
    { ...initialValue, app_id: 'cli_test', agentId: 'agent-test', environmentId: 'env-test' },
  );
  fireEvent.click(await screen.findByRole('button', { name: '下一步' }));
  expect((screen.getByLabelText('渠道模式') as HTMLButtonElement).disabled).toBe(true);
});

test('渠道保存失败时只发送一份含环境的请求，不先修改环境设置', async () => {
  resetTestDom('/');
  const saved = { ...channel, binding_status: 'bound' as const, app_id: 'cli_test' };
  const posts: { path: string; body: Record<string, unknown> }[] = [];
  globalThis.fetch = (async (input, init) => {
    if (init?.method === 'POST') {
      posts.push({ path: String(input), body: JSON.parse(String(init.body)) });
      return Response.json({ error: { type: 'api_error', message: '渠道保存失败' } }, { status: 500 });
    }
    return Response.json(targets(String(input)));
  }) as typeof fetch;
  const navigated = mount(saved, {
    ...initialValue,
    app_id: 'cli_test',
    agentId: 'agent-test',
    environmentId: 'env-new',
  });
  fireEvent.click(await screen.findByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await screen.findByText('渠道保存失败');
  expect(posts).toHaveLength(1);
  expect(posts[0].path).not.toContain('/settings');
  expect(posts[0].body.environment_id).toBe('env-new');
  expect(navigated).not.toHaveBeenCalled();
});

test.each([
  {
    locale: 'zh-CN',
    saveLabel: '保存配置',
    expected: '飞书 App ID 已绑定到其他渠道，请使用未绑定的飞书应用。',
  },
  {
    locale: 'en',
    saveLabel: 'Save configuration',
    expected: 'This Feishu App ID is already bound to another channel. Use an unused Feishu app.',
  },
] as const)('渠道绑定冲突使用界面语言 $locale', async ({ locale, saveLabel, expected }) => {
  resetTestDom('/');
  globalThis.fetch = (async (input, init) => {
    if (init?.method === 'POST') {
      return Response.json(
        { error: { type: 'conflict_error', code: 'channel_app_already_bound', message: '服务端冲突文案' } },
        { status: 409 },
      );
    }
    return Response.json(targets(String(input)));
  }) as typeof fetch;
  mount(
    { ...channel, binding_status: 'bound', app_id: 'cli_test' },
    { ...initialValue, app_id: 'cli_test', agentId: 'agent-test', environmentId: 'env-test' },
    undefined,
    locale,
  );
  fireEvent.click(screen.getByRole('button', { name: saveLabel }));
  expect((await screen.findByRole('alert')).textContent).toBe(expected);
});

test('多Vault编辑完整回显，取消其中一项后只提交剩余列表', async () => {
  resetTestDom('/');
  let body: Record<string, unknown> = {};
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    if (init?.method === 'POST') {
      body = JSON.parse(String(init.body));
      return Response.json({ ...channel, ...body, binding_status: 'bound' });
    }
    if (url.includes('/credentials')) return Response.json({ data: [] });
    if (url.includes('/vaults'))
      return Response.json({
        data: [
          { id: 'vault-test', display_name: '凭证库甲' },
          { id: 'vault-second', display_name: '凭证库乙' },
        ],
      });
    return Response.json(targets(url));
  }) as typeof fetch;
  const saved = {
    ...channel,
    binding_status: 'bound' as const,
    app_id: 'cli_test',
    vault_ids: ['vault-test', 'vault-second'],
  };
  const navigated = mount(saved, { ...initialValue, ...saved, agentId: 'agent-test', environmentId: 'env-test' });
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  const side = within(screen.getByRole('complementary', { name: '进度' }));
  await side.findByText('凭证库甲');
  await side.findByText('凭证库乙');
  fireEvent.click(screen.getByRole('combobox', { name: '默认 Vault' }));
  fireEvent.click(await screen.findByRole('option', { name: /凭证库甲/ }));
  fireEvent.click(screen.getByRole('combobox', { name: '默认 Vault' }));
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(body.vault_ids).toEqual(['vault-second']);
  expect(body.vault_id).toBeUndefined();
});

test('两个文件与两个仓库完整回显，无修改保存保留顺序与各仓库令牌状态', async () => {
  resetTestDom('/');
  const resources: ChannelResource[] = [
    {
      type: 'github_repository',
      url: 'https://gitlab.example/a.git',
      mount_path: '/workspace/a',
      tokenConfigured: true,
    },
    { type: 'file', file_id: 'file_a', mount_path: '/uploads/a.txt' },
    {
      type: 'github_repository',
      url: 'https://gitlab.example/b.git',
      mount_path: '/workspace/b',
      checkout: { type: 'branch', name: 'main' },
      tokenConfigured: true,
    },
    { type: 'file', file_id: 'file_b', mount_path: '/uploads/b.txt' },
  ];
  const saved = { ...channel, binding_status: 'bound' as const, app_id: 'cli_test', resources };
  let body: Record<string, unknown> = {};
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    if (init?.method === 'POST') {
      body = JSON.parse(String(init.body));
      return Response.json(saved);
    }
    if (url.includes('/v1/files'))
      return Response.json({
        data: [
          { id: 'file_a', filename: 'a.txt', size_bytes: 1, mime_type: 'text/plain' },
          { id: 'file_b', filename: 'b.txt', size_bytes: 1, mime_type: 'text/plain' },
        ],
        has_more: false,
      });
    return Response.json(targets(url));
  }) as typeof fetch;
  const navigated = mount(saved, {
    ...initialValue,
    ...saved,
    ...resourceFormValues(resources),
    agentId: 'agent-test',
    environmentId: 'env-test',
  });
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  expect(screen.getAllByLabelText('URL').map((element) => (element as HTMLInputElement).value)).toEqual([
    'https://gitlab.example/a.git',
    'https://gitlab.example/b.git',
  ]);
  expect(screen.getAllByPlaceholderText('已配置，留空保持不变')).toHaveLength(2);
  const side = within(screen.getByRole('complementary', { name: '进度' }));
  await side.findByText('file_a');
  await side.findByText('file_b');
  fireEvent.click(screen.getByRole('button', { name: '保存配置' }));
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(body.resources).toEqual(
    resources.map((value) => {
      const { tokenConfigured: _configured, ...input } = value as ChannelResource & { tokenConfigured?: boolean };
      return input;
    }),
  );
  expect(body.git_repository).toBeUndefined();
});

test('已绑定渠道在第一步直接保存并保留原应用凭据', async () => {
  resetTestDom('/');
  const posts: { url: string; body: Record<string, unknown> }[] = [];
  const saved = { ...channel, binding_status: 'bound' as const, app_id: 'cli_test' };
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    if (init?.method === 'POST') {
      posts.push({ url, body: JSON.parse(String(init.body)) });
      return Response.json(saved);
    }
    return Response.json(targets(url));
  }) as typeof fetch;
  const navigated = mount(saved, { ...initialValue, ...saved, agentId: 'agent-test', environmentId: 'env-test' });
  const save = screen.getByRole('button', { name: '保存配置' });
  expect(save.hasAttribute('disabled')).toBe(false);
  fireEvent.click(save);
  await waitFor(() => expect(navigated).toHaveBeenCalled());
  expect(posts).toHaveLength(1);
  expect(posts[0].url).toContain('/channels/ch-test');
  expect(posts[0].body).toMatchObject({ name: saved.name, environment_id: 'env-test' });
  expect(posts[0].body.app_secret).toBeUndefined();
});
