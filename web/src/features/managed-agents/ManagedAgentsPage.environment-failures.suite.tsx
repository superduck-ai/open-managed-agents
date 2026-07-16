import { expect, test } from 'bun:test';
import {
  fireEvent,
  mock,
  mockManagedResourceApi,
  renderManagedAgentsPage,
  resetTestDom,
  screen,
  waitFor,
  within,
} from './ManagedAgentsPage.test-utils';

function requestUrl(input: RequestInfo | URL) {
  return typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
}

function requestMethod(input: RequestInfo | URL, init?: RequestInit) {
  return init?.method ?? (input instanceof Request ? input.method : 'GET');
}

function errorResponse(message: string, status = 400) {
  return new Response(JSON.stringify({ error: { message } }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

export function registerManagedAgentsEnvironmentFailureTests() {
  test('localizes every known Environment update error from the server response path', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    mockManagedResourceApi();
    const baseFetch = globalThis.fetch;
    let responseMessage = 'Environment name already exists';
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (requestUrl(input) === '/v1/environments/env_one123456?beta=true' && requestMethod(input, init) === 'POST') {
        return errorResponse(responseMessage, responseMessage === 'Environment name already exists' ? 409 : 400);
      }
      return baseFetch(input, init);
    }) as typeof fetch;
    renderManagedAgentsPage('environments', 'zh-CN');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '编辑' }));
    fireEvent.change(screen.getByRole('textbox', { name: '环境名称' }), { target: { value: '服务器校验' } });
    const knownErrors = [
      ['Environment name already exists', '已存在同名环境。'],
      ['name is required', '请输入环境名称。'],
      ['config.packages.pip entries must be non-empty strings', '软件包值必须包含非空的软件包标记。'],
      ['config.packages.pip entries must be at most 255 characters', '每个软件包标记经 UTF-8 编码后最多为 255 字节。'],
      ['metadata may contain at most 16 entries', '元数据最多包含 16 项。'],
      ['metadata keys must be between 1 and 64 characters', '元数据键经 UTF-8 编码后必须为 1 至 64 字节。'],
      ['metadata values must be at most 512 characters', '元数据值经 UTF-8 编码后最多为 512 字节。'],
      ['metadata must be an object', '服务器拒绝了元数据配置。'],
      ['config.packages must be an object', '服务器拒绝了软件包配置。'],
      ['config.networking must be an object', '服务器拒绝了网络访问配置。'],
      ['Environment not found', '未找到环境。'],
      ['Environments API requires beta=true', '缺少 Environments API beta 标头。'],
    ] as const;
    for (const [message, expected] of knownErrors) {
      responseMessage = message;
      fireEvent.click(screen.getByRole('button', { name: '保存更改' }));
      expect(await screen.findByText(expected)).toBeTruthy();
    }

    responseMessage = 'upstream exploded';
    fireEvent.click(screen.getByRole('button', { name: '保存更改' }));
    expect(await screen.findByText('无法保存环境。 服务器详情：upstream exploded')).toBeTruthy();
    expect((screen.getByRole('textbox', { name: '环境名称' }) as HTMLInputElement).value).toBe('服务器校验');
  });

  test('rejects invalid Environment edits before sending an update', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    const api = mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Environment name' }), { target: { value: '   ' } });
    fireEvent.change(screen.getByRole('textbox', { name: 'Package value 1' }), {
      target: { value: 'x'.repeat(256) },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Add metadata entry' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Metadata key 2' }), { target: { value: 'Owner' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));

    expect(screen.getByText('Enter an environment name.')).toBeTruthy();
    expect(screen.getByText('Each package token must be 255 UTF-8 bytes or fewer.')).toBeTruthy();
    expect(screen.getAllByText('Metadata keys must be unique.')).toHaveLength(2);
    expect(screen.getByText('Enter a metadata value, or remove the row to delete this entry.')).toBeTruthy();

    fireEvent.change(screen.getByRole('textbox', { name: 'Environment name' }), { target: { value: 'Valid name' } });
    fireEvent.change(screen.getByRole('textbox', { name: 'Package value 1' }), {
      target: { value: '包'.repeat(86) },
    });
    fireEvent.change(screen.getByRole('textbox', { name: 'Metadata key 2' }), {
      target: { value: '键'.repeat(22) },
    });
    fireEvent.change(screen.getByRole('textbox', { name: 'Metadata value 2' }), {
      target: { value: '值'.repeat(171) },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    expect(screen.getByText('Each package token must be 255 UTF-8 bytes or fewer.')).toBeTruthy();
    expect(screen.getByText('Metadata keys must be between 1 and 64 UTF-8 bytes.')).toBeTruthy();
    expect(screen.getByText('Metadata values must be 512 UTF-8 bytes or fewer.')).toBeTruthy();
    expect(
      api.requests.some(
        (request) => request.url === '/v1/environments/env_one123456?beta=true' && request.method === 'POST',
      ),
    ).toBe(false);
  });

  test('guards dirty Environment edits across cancel and ordinary internal navigation', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Environment name' }), {
      target: { value: 'Environment changed' },
    });
    expect(window.dispatchEvent(new window.Event('beforeunload', { cancelable: true }))).toBe(false);
    const breadcrumb = screen.getByRole('link', { name: 'Environments' });
    fireEvent.click(breadcrumb);
    let discardDialog = await screen.findByRole('alertdialog', { name: 'Discard unsaved changes?' });
    fireEvent.click(within(discardDialog).getByRole('button', { name: 'Continue editing' }));
    expect(window.location.pathname).toBe('/workspaces/default/environments/env_one123456');

    fireEvent.click(breadcrumb);
    discardDialog = await screen.findByRole('alertdialog', { name: 'Discard unsaved changes?' });
    fireEvent.click(within(discardDialog).getByRole('button', { name: 'Discard changes' }));
    await waitFor(() => expect(window.location.pathname).toBe('/workspaces/default/environments'));
  });

  test('guards dirty Environment edits across browser Back navigation', async () => {
    const detailHref = '/workspaces/default/environments/env_one123456';
    const listHref = '/workspaces/default/environments';
    resetTestDom(`https://oma.duck.ai${detailHref}`);
    mockManagedResourceApi();
    const { router } = renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    router.history.replace(listHref);
    router.history.flush();
    router.history.push(detailHref);
    router.history.flush();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Environment name' }), {
      target: { value: 'Environment changed' },
    });
    router.history.back();

    const discardDialog = await screen.findByRole('alertdialog', { name: 'Discard unsaved changes?' });
    fireEvent.click(within(discardDialog).getByRole('button', { name: 'Discard changes' }));
    await waitFor(() => expect(router.history.location.pathname).toBe(listHref));
  });

  test('guards dirty Environment edits across browser Forward navigation', async () => {
    const detailHref = '/workspaces/default/environments/env_one123456';
    const listHref = '/workspaces/default/environments';
    resetTestDom(`https://oma.duck.ai${detailHref}`);
    mockManagedResourceApi();
    const { router } = renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    router.history.push(listHref);
    router.history.flush();
    await new Promise<void>((resolve) => {
      window.addEventListener('popstate', () => resolve(), { once: true });
      router.history.back({ ignoreBlocker: true });
    });
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Environment name' }), {
      target: { value: 'Environment changed' },
    });
    router.history.forward();

    const discardDialog = await screen.findByRole('alertdialog', { name: 'Discard unsaved changes?' });
    fireEvent.click(within(discardDialog).getByRole('button', { name: 'Discard changes' }));
    await waitFor(() => expect(router.history.location.pathname).toBe(listHref));
  });

  test('guards dirty Environment edits across programmatic router navigation', async () => {
    const detailHref = '/workspaces/default/environments/env_one123456';
    const listHref = '/workspaces/default/environments';
    resetTestDom(`https://oma.duck.ai${detailHref}`);
    mockManagedResourceApi();
    const { router } = renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Environment name' }), {
      target: { value: 'Environment changed' },
    });
    void router.navigate({
      to: '/workspaces/$workspaceId/environments',
      params: { workspaceId: 'default' },
    });

    const discardDialog = await screen.findByRole('alertdialog', { name: 'Discard unsaved changes?' });
    fireEvent.click(within(discardDialog).getByRole('button', { name: 'Discard changes' }));
    await waitFor(() => expect(router.history.location.pathname).toBe(listHref));
  });

  test('does not intercept modified Environment navigation while dirty', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Environment name' }), {
      target: { value: 'Environment changed' },
    });
    expect(fireEvent.click(screen.getByRole('link', { name: 'Environments' }), { metaKey: true })).toBe(true);
    expect(screen.queryByRole('alertdialog', { name: 'Discard unsaved changes?' })).toBeNull();
  });

  test('keeps Environment creation open and disables close controls while saving', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments');
    mockManagedResourceApi();
    const baseFetch = globalThis.fetch;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (requestUrl(input) === '/v1/environments?beta=true' && requestMethod(input, init) === 'POST') {
        return new Promise<Response>(() => undefined);
      }
      return baseFetch(input, init);
    }) as typeof fetch;
    renderManagedAgentsPage('environments');

    expect(await screen.findByText('Environment one')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Create environment' }));
    const dialog = screen.getByRole('dialog', { name: 'Create environment' });
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Name' }), { target: { value: 'Pending create' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }));

    expect((within(dialog).getByRole('button', { name: 'Cancel' }) as HTMLButtonElement).disabled).toBe(true);
    expect((within(dialog).getByRole('button', { name: 'Close' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.getByRole('dialog', { name: 'Create environment' })).toBeTruthy();
  });

  test('localizes Environment list, load, and work operation failures', async () => {
    const cases = [
      {
        url: 'https://oma.duck.ai/workspaces/default/environments',
        match: (url: string, method: string) => url.startsWith('/v1/environments?') && method === 'GET',
        message: 'Could not list environment',
        expected: '无法加载环境列表。',
      },
      {
        url: 'https://oma.duck.ai/workspaces/default/environments/env_one123456',
        match: (url: string, method: string) => url === '/v1/environments/env_one123456?beta=true' && method === 'GET',
        message: 'Could not retrieve environment',
        expected: '无法加载环境。',
      },
      {
        url: 'https://oma.duck.ai/workspaces/default/environments/env_one123456',
        match: (url: string, method: string) => url.includes('/environments/env_one123456/work?') && method === 'GET',
        message: 'work unavailable',
        expected: '无法加载工作队列。 服务器详情：work unavailable',
      },
    ];
    for (const failure of cases) {
      resetTestDom(failure.url);
      mockManagedResourceApi();
      const baseFetch = globalThis.fetch;
      globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = requestUrl(input);
        const method = requestMethod(input, init);
        return failure.match(url, method) ? errorResponse(failure.message, 500) : baseFetch(input, init);
      }) as typeof fetch;
      renderManagedAgentsPage('environments', 'zh-CN');
      expect(await screen.findByText(failure.expected)).toBeTruthy();
    }
  });

  test('localizes the Environment create operation failure', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments');
    mockManagedResourceApi();
    const baseFetch = globalThis.fetch;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (requestUrl(input) === '/v1/environments?beta=true' && requestMethod(input, init) === 'POST') {
        return errorResponse('Could not create environment', 500);
      }
      return baseFetch(input, init);
    }) as typeof fetch;
    renderManagedAgentsPage('environments', 'zh-CN');

    expect(await screen.findByText('Environment one')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '创建环境' }));
    const dialog = screen.getByRole('dialog', { name: '创建环境' });
    fireEvent.change(within(dialog).getByRole('textbox', { name: '名称' }), { target: { value: '失败环境' } });
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }));
    expect(await within(dialog).findByText('无法创建环境。')).toBeTruthy();
  });

  test('localizes the Environment archive operation failure', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments');
    mockManagedResourceApi();
    const baseFetch = globalThis.fetch;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (requestUrl(input).endsWith('/archive?beta=true') && requestMethod(input, init) === 'POST') {
        return errorResponse('Could not archive environment', 500);
      }
      return baseFetch(input, init);
    }) as typeof fetch;
    renderManagedAgentsPage('environments', 'zh-CN');

    expect(await screen.findByText('Environment one')).toBeTruthy();
    fireEvent.click(screen.getAllByRole('button', { name: '更多操作' })[0]);
    fireEvent.click(screen.getByRole('menuitem', { name: '归档环境' }));
    fireEvent.click(screen.getByRole('button', { name: '归档' }));
    expect(await screen.findByText('无法归档环境。')).toBeTruthy();
  });

  test('localizes the Environment delete operation failure', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments');
    mockManagedResourceApi();
    const baseFetch = globalThis.fetch;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (requestUrl(input) === '/v1/environments/env_one123456?beta=true' && requestMethod(input, init) === 'DELETE') {
        return errorResponse('Could not delete environment', 500);
      }
      return baseFetch(input, init);
    }) as typeof fetch;
    renderManagedAgentsPage('environments', 'zh-CN');

    expect(await screen.findByText('Environment one')).toBeTruthy();
    fireEvent.click(screen.getAllByRole('button', { name: '更多操作' })[0]);
    fireEvent.click(screen.getByRole('menuitem', { name: '删除环境' }));
    fireEvent.click(screen.getByRole('button', { name: '删除' }));
    expect(await screen.findByText('无法删除环境。')).toBeTruthy();
  });

  test('localizes the active-work Environment delete rejection', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    mockManagedResourceApi();
    const baseFetch = globalThis.fetch;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (requestUrl(input) === '/v1/environments/env_one123456?beta=true' && requestMethod(input, init) === 'DELETE') {
        return errorResponse('Environment has active work');
      }
      return baseFetch(input, init);
    }) as typeof fetch;
    renderManagedAgentsPage('environments', 'zh-CN');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '更多操作' }));
    fireEvent.click(screen.getByRole('menuitem', { name: '删除' }));
    const dialog = await screen.findByRole('alertdialog', { name: /删除环境/ });
    fireEvent.click(within(dialog).getByRole('button', { name: '删除' }));
    expect(await screen.findByText('此环境仍有活跃工作项，无法删除。')).toBeTruthy();
  });
}
