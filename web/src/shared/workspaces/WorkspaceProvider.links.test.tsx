import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { StrictMode, useState } from 'react';
import { resetTestDom } from '../../test/setup';
import { AuthContext, useAuth, type AuthContextValue } from '../auth/context';
import type { AuthAccount } from '../auth/api';
import { getConsoleRequestContext, setConsoleRequestContext } from '../api/client';
import { useOrganizations, type OrganizationContextValue } from '../organizations/context';
import { readPlatformScope, savePlatformScope } from '../organizations/platform-scope';
import { useWorkspace, type WorkspaceContextValue } from './context';
import { WorkspaceProvider } from './WorkspaceProvider';
import { startupWorkspaceId } from './workspace-link';

const { act, cleanup, render, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const identity: AuthAccount = {
  uuid: 'operator',
  email_address: 'operator@example.com',
  default_organization_uuid: 'home',
  memberships: [{ organization: { uuid: 'home' }, role: 'user' }],
};
let organizations: OrganizationContextValue;
let workspace: WorkspaceContextValue;
let auth: AuthContextValue;
let authenticate: () => void;
let expire: () => void;
function currentLocationWorkspaceTarget() {
  return startupWorkspaceId(window.location);
}
let requests: Array<{ path: string; organization: string | null; workspace: string | null }>;
function Probe() {
  organizations = useOrganizations()!;
  workspace = useWorkspace();
  auth = useAuth();
  return null;
}
function mount(path: string, { admin = true, anonymous = false, strict = false } = {}) {
  resetTestDom(`https://oma.duck.ai${path}`);
  requests = [];
  globalThis.fetch = mock(async (input, options) => {
    const path = String(input);
    const headers = new Headers(options?.headers);
    const org = headers.get('X-Organization-UUID');
    requests.push({ path, organization: org, workspace: headers.get('X-Workspace-ID') });
    if (path.startsWith('/api/platform-admin/workspaces/')) {
      const id = decodeURIComponent(path.split('/')[4]!);
      if (id === 'missing') return Response.json({ error: { code: 'not_found' } }, { status: 404 });
      return Response.json({ workspace_id: id, organization_uuid: id === 'ws-home' ? 'home' : 'target' });
    }
    if (path === '/api/bootstrap')
      return Response.json({
        account: identity,
        csrf_token: `csrf-${org}`,
        organization_access: {
          source: admin ? 'platform_admin' : 'organization_member',
          organization: { uuid: org, name: org },
          organization_role: 'admin',
          workspace_role: 'workspace_admin',
        },
      });
    return Response.json([
      { id: `ws-${org}`, name: 'Default', is_default: true, effective_role: 'workspace_admin' },
      { id: 'ws-other', name: 'Other', effective_role: 'workspace_admin' },
    ]);
  }) as typeof fetch;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const navigate = mock(async () => {});
  function Harness() {
    const [account, setAccount] = useState<AuthAccount | null>(anonymous ? null : identity);
    authenticate = () => setAccount(identity);
    expire = () => setAccount(null);
    return (
      <AuthContext.Provider
        value={{
          account,
          status: account ? 'authenticated' : 'anonymous',
          csrfToken: 'csrf',
          platformAccess: { role: admin ? 'super_admin' : null, permissions: admin ? ['platform:access'] : [] },
          refresh: async () => ({ account: identity }),
          logout: async () => {},
        }}
      >
        <WorkspaceProvider navigateScope={navigate} currentWorkspaceTarget={currentLocationWorkspaceTarget}>
          <Probe />
        </WorkspaceProvider>
      </AuthContext.Provider>
    );
  }
  const content = (
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>
  );
  const view = render(strict ? <StrictMode>{content}</StrictMode> : content);
  return { ...view, client, navigate };
}
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  window.localStorage.clear();
  window.sessionStorage.clear();
});

test('无效深链保留原 URL 和空请求上下文，不切到默认空间', async () => {
  const path = '/workspaces/missing/sessions/sesn-test?event=event-7#message';
  const { navigate } = mount(path);
  await waitFor(() => expect(organizations.error).toBeTruthy());
  expect(workspace.activeWorkspaceId).toBe('');
  expect(getConsoleRequestContext()).toEqual({});
  expect(window.location.href).toBe(`https://oma.duck.ai${path}`);
  expect(navigate).not.toHaveBeenCalled();
  expect(requests.map((request) => request.path)).toEqual(['/api/platform-admin/workspaces/missing/scope']);
  expect(readPlatformScope(identity.uuid)).toBeUndefined();
});

test('解析临时失败后重试仍解析原目标并清除旧缓存', async () => {
  const { client, navigate } = mount('/workspaces/ws-target/agents/agent-7?view=raw#details');
  const fallbackFetch = globalThis.fetch;
  globalThis.fetch = mock(async () => Response.json({}, { status: 503 })) as typeof fetch;
  client.setQueryData(['agents', 'old'], ['stale']);
  await waitFor(() => expect(organizations.error).toBeTruthy());
  expect(client.getQueryData(['agents', 'old'])).toBeUndefined();
  expect(getConsoleRequestContext()).toEqual({});
  globalThis.fetch = fallbackFetch;
  await act(async () => expect(await organizations.retry()).toBe(true));
  expect(workspace.activeWorkspaceId).toBe('ws-target');
  expect(navigate).not.toHaveBeenCalled();
});

test('目标工作区不在有效列表时失败且不采用目标组织默认空间', async () => {
  const { navigate } = mount('/workspaces/ws-archived/agents');
  await waitFor(() => expect(organizations.error).toBeTruthy());
  expect(workspace.activeWorkspaceId).toBe('');
  expect(getConsoleRequestContext()).toEqual({});
  expect(navigate).not.toHaveBeenCalled();
});

for (const path of [
  '/workspaces/ws-target/sessions/sesn-7?event=event-3&view=raw#message',
  '/settings/workspaces/ws-target/keys?filter=active#key-2',
]) {
  test(`非成员超管从原始 URL 恢复精确作用域并保留深链 ${path}`, async () => {
    const { navigate } = mount(path);
    await waitFor(() => expect(workspace.activeWorkspaceId).toBe('ws-target'));
    expect(organizations.orgUuid).toBe('target');
    expect(organizations.platformScope?.targetWorkspaceId).toBe('ws-target');
    expect(getConsoleRequestContext()).toEqual({
      organizationUuid: 'target',
      workspaceId: 'ws-target',
      csrfToken: 'csrf-target',
    });
    expect(window.location.href).toBe(`https://oma.duck.ai${path}`);
    expect(navigate).not.toHaveBeenCalled();
    expect(requests).toEqual([
      { path: '/api/platform-admin/workspaces/ws-target/scope', organization: null, workspace: null },
      { path: '/api/bootstrap', organization: 'target', workspace: null },
      { path: '/api/console/organizations/target/workspaces', organization: 'target', workspace: null },
    ]);
  });
}

test('同组织超管深链保持普通模式，刷新不进入平台模式', async () => {
  mount('/workspaces/ws-home/sessions/sesn-home');
  await waitFor(() => expect(workspace.activeWorkspaceId).toBe('ws-home'));
  expect(organizations.platformScope).toBeUndefined();
  expect(readPlatformScope(identity.uuid)).toBeUndefined();
  expect(window.localStorage.getItem('oma.workspace.operator.home')).toBe('ws-home');
  await act(async () => {
    await auth.refresh();
    await workspace.refreshWorkspaces();
  });
  expect(organizations.platformScope).toBeUndefined();
  expect(workspace.activeWorkspaceId).toBe('ws-home');
});

test('普通成员和 default 别名不调用平台解析接口', async () => {
  for (const [admin, id] of [
    [false, 'ws-home'],
    [true, 'default'],
  ] as const) {
    const view = mount(`/workspaces/${id}/agents`, { admin });
    await waitFor(() => expect(workspace.activeWorkspaceId).toBe('ws-home'));
    expect(requests.some((request) => request.path.startsWith('/api/platform-admin'))).toBe(false);
    expect(organizations.platformScope).toBeUndefined();
    view.unmount();
  }
});

test('原始目标覆盖同标签已保存平台组织和工作区', async () => {
  const path = '/workspaces/ws-target/agents/agent-7';
  resetTestDom(`https://oma.duck.ai${path}`);
  savePlatformScope({ accountUuid: identity.uuid, targetOrganizationUuid: 'old', targetWorkspaceId: 'ws-old' });
  const { navigate } = mount(path);
  await waitFor(() => expect(workspace.activeWorkspaceId).toBe('ws-target'));
  expect(requests.some((request) => request.organization === 'old')).toBe(false);
  expect(readPlatformScope(identity.uuid)?.targetOrganizationUuid).toBe('target');
  expect(navigate).not.toHaveBeenCalled();
});

for (const anonymous of [false, true]) {
  test(`登录页 fresh load 从 returnTo 恢复原目标 ${anonymous ? '登录后' : '已登录'}`, async () => {
    const target = '/workspaces/ws-target/sessions/sesn-7?event=event-3#message';
    mount(`/login?returnTo=${encodeURIComponent(target)}`, { anonymous });
    if (anonymous) {
      expect(requests).toHaveLength(0);
      await act(async () => authenticate());
    }
    await waitFor(() => expect(workspace.activeWorkspaceId).toBe('ws-target'));
    expect(organizations.platformScope?.targetOrganizationUuid).toBe('target');
  });
}

test('手动切换后刷新保持最新选择，不再次应用启动深链', async () => {
  mount('/workspaces/ws-target/sessions/sesn-7?event=event-3');
  await waitFor(() => expect(workspace.activeWorkspaceId).toBe('ws-target'));
  await act(async () => workspace.selectWorkspace('ws-other'));
  await act(async () => {
    await auth.refresh();
    await workspace.refreshWorkspaces();
  });
  expect(workspace.activeWorkspaceId).toBe('ws-other');
  expect(readPlatformScope(identity.uuid)?.targetWorkspaceId).toBe('ws-other');
  await act(async () => expect(await organizations.switchOrganization('home')).toBe(true));
  await act(async () => workspace.refreshWorkspaces());
  expect(workspace.activeWorkspaceId).toBe('ws-home');
  expect(organizations.platformScope).toBeUndefined();
  expect(requests.filter((request) => request.path.startsWith('/api/platform-admin'))).toHaveLength(1);
});

test('手动选择使迟到解析失效，不能覆盖新组织或请求上下文', async () => {
  mount('/workspaces/ws-target/agents');
  const fallbackFetch = globalThis.fetch;
  let complete!: (response: Response) => void;
  globalThis.fetch = mock((input, options) =>
    String(input).startsWith('/api/platform-admin')
      ? new Promise<Response>((resolve) => {
          complete = resolve;
        })
      : fallbackFetch(input, options),
  ) as typeof fetch;
  await waitFor(() => expect(complete).toBeDefined());
  await act(async () => expect(await organizations.switchOrganization('home')).toBe(true));
  await act(async () => complete(Response.json({ workspace_id: 'ws-target', organization_uuid: 'target' })));
  expect(workspace.activeWorkspaceId).toBe('ws-home');
  expect(getConsoleRequestContext().organizationUuid).toBe('home');
  expect(readPlatformScope(identity.uuid)).toBeUndefined();
  expect(requests.some((request) => request.organization === 'target')).toBe(false);
});

for (const failure of ['resolver', 'account', 'organization', 'access', 'source', 'archived']) {
  test(`深链拒绝无效服务端范围并保持空请求上下文 ${failure}`, async () => {
    mount('/workspaces/ws-target/agents/agent-7');
    const fallbackFetch = globalThis.fetch;
    globalThis.fetch = mock(async (input, options) => {
      const path = String(input);
      const response = await fallbackFetch(input, options);
      if (failure === 'resolver' && path.startsWith('/api/platform-admin'))
        return Response.json({ workspace_id: 'different', organization_uuid: 'target' });
      if (path === '/api/bootstrap') {
        const body = await response.json();
        if (failure === 'source') body.organization_access.source = 'organization_member';
        if (failure === 'account') body.account.uuid = 'another-account';
        if (failure === 'organization') body.organization_access.organization.uuid = 'another-org';
        if (failure === 'access') return Response.json({ error: 'forbidden' }, { status: 403 });
        return Response.json(body);
      }
      if (failure === 'archived' && path.endsWith('/workspaces'))
        return Response.json([{ id: 'ws-target', archived_at: '2026-11-01T00:00:00Z', is_default: true }]);
      return response;
    }) as typeof fetch;
    await waitFor(() => expect(organizations.error).toBeTruthy());
    expect(workspace.activeWorkspaceId).toBe('');
    expect(getConsoleRequestContext()).toEqual({});
    expect(readPlatformScope(identity.uuid)).toBeUndefined();
  });
}

for (const stage of ['bootstrap', 'list']) {
  test(`手动组织选择使迟到深链${stage}失效`, async () => {
    mount('/workspaces/ws-target/agents');
    const fallbackFetch = globalThis.fetch;
    let complete!: (response: Response) => void;
    let pendingResponse!: Response;
    globalThis.fetch = mock(async (input, options) => {
      const path = String(input);
      const org = new Headers(options?.headers).get('X-Organization-UUID');
      const response = await fallbackFetch(input, options);
      if (org === 'target' && (stage === 'bootstrap' ? path === '/api/bootstrap' : path.endsWith('/workspaces'))) {
        pendingResponse = response;
        return new Promise<Response>((resolve) => {
          complete = resolve;
        });
      }
      return response;
    }) as typeof fetch;
    await waitFor(() => expect(complete).toBeDefined());
    await act(async () => expect(await organizations.switchOrganization('home')).toBe(true));
    await act(async () => complete(pendingResponse));
    expect(workspace.activeWorkspaceId).toBe('ws-home');
    expect(getConsoleRequestContext().organizationUuid).toBe('home');
    expect(organizations.platformScope).toBeUndefined();
    expect(organizations.error).toBeNull();
  });
}

for (const saved of [false, true]) {
  test(`StrictMode 已有 bootstrap 时恢复精确工作区与平台模式 ${saved ? '已保存' : '首次'}`, async () => {
    const path = '/workspaces/ws-home/sessions/sesn-home?event=event-3#message';
    resetTestDom(`https://oma.duck.ai${path}`);
    if (saved)
      savePlatformScope({ accountUuid: identity.uuid, targetOrganizationUuid: 'home', targetWorkspaceId: 'ws-other' });
    const { navigate } = mount(path, { strict: true });
    await waitFor(() => expect(workspace.activeWorkspaceId).toBe('ws-home'));
    expect(Boolean(organizations.platformScope)).toBe(saved);
    expect(Boolean(readPlatformScope(identity.uuid))).toBe(saved);
    expect(organizations.error).toBeNull();
    expect(navigate).not.toHaveBeenCalled();
    expect(window.location.href).toBe(`https://oma.duck.ai${path}`);
  });
}

test('会话失效后按当前登录 returnTo 恢复，不复用已消费的启动目标', async () => {
  mount('/workspaces/ws-target/sessions/sesn-old');
  await waitFor(() => expect(workspace.activeWorkspaceId).toBe('ws-target'));
  await act(async () => workspace.selectWorkspace('ws-other'));
  await act(async () => expire());
  expect(getConsoleRequestContext()).toEqual({});
  window.history.replaceState(
    null,
    '',
    '/login?returnTo=' + encodeURIComponent('/settings/workspaces/ws-home/keys?filter=active#key-3'),
  );
  await act(async () => authenticate());
  await waitFor(() => expect(workspace.activeWorkspaceId).toBe('ws-home'));
  expect(organizations.orgUuid).toBe('home');
  expect(organizations.platformScope).toBeUndefined();
  expect(
    requests.filter((request) => request.path.startsWith('/api/platform-admin')).map((request) => request.path),
  ).toEqual(['/api/platform-admin/workspaces/ws-target/scope', '/api/platform-admin/workspaces/ws-home/scope']);
});

test('已保存成员组织的平台模式在临时错误和明确重试后保持', async () => {
  const path = '/workspaces/ws-home/sessions/sesn-home';
  resetTestDom(`https://oma.duck.ai${path}`);
  savePlatformScope({ accountUuid: identity.uuid, targetOrganizationUuid: 'home', targetWorkspaceId: 'ws-other' });
  mount(path);
  const fallbackFetch = globalThis.fetch;
  globalThis.fetch = mock(async () => Response.json({}, { status: 503 })) as typeof fetch;
  await waitFor(() => expect(organizations.error).toBeTruthy());
  expect(organizations.orgUuid).toBeUndefined();
  expect(getConsoleRequestContext()).toEqual({});
  expect(readPlatformScope(identity.uuid)?.targetOrganizationUuid).toBe('home');
  globalThis.fetch = fallbackFetch;
  await act(async () => expect(await organizations.retry()).toBe(true));
  expect(workspace.activeWorkspaceId).toBe('ws-home');
  expect(organizations.platformScope?.targetOrganizationUuid).toBe('home');
  expect(readPlatformScope(identity.uuid)?.targetWorkspaceId).toBe('ws-home');
});

for (const saved of [false, true]) {
  test(`default 别名保留详情 URL 并选择${saved ? '已保存平台组织' : '普通组织'}真实 Default`, async () => {
    const path = '/workspaces/default/sessions/sesn-7?event=event-3#message';
    resetTestDom(`https://oma.duck.ai${path}`);
    if (saved)
      savePlatformScope({
        accountUuid: identity.uuid,
        targetOrganizationUuid: 'target',
        targetWorkspaceId: 'ws-other',
      });
    const { navigate } = mount(path);
    await waitFor(() => expect(workspace.activeWorkspaceId).toBe(saved ? 'ws-target' : 'ws-home'));
    expect(requests.some((request) => request.path.startsWith('/api/platform-admin'))).toBe(false);
    expect(navigate).not.toHaveBeenCalled();
    expect(window.location.href).toBe(`https://oma.duck.ai${path}`);
    expect(Boolean(organizations.platformScope)).toBe(saved);
  });
}
