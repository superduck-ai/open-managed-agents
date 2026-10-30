import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { resetTestDom } from '../../test/setup';
import { AuthContext, useAuth } from '../auth/context';
import type { AuthAccount, BootstrapResponse } from '../auth/api';
import { useOrganizations, type OrganizationContextValue } from '../organizations/context';
import { readPlatformScope, savePlatformScope } from '../organizations/platform-scope';
import {
  getConsoleRequestContext,
  getScopeSignal,
  setConsoleRequestContext,
  reportApiAuthFailure,
} from '../api/client';
import { WorkspaceProvider } from './WorkspaceProvider';
import { useWorkspace } from './context';

const { act, cleanup, render, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
let organizations: OrganizationContextValue;
let currentAccount: AuthAccount | null;
let activeWorkspace: string;
let selectWorkspace: (id: string) => void;
let identity: AuthAccount = {
  uuid: 'account',
  email_address: 'admin@example.com',
  default_organization_uuid: 'home',
  memberships: [{ organization: { uuid: 'home' }, role: 'user' }],
};
const requests: Array<{ path: string; organization: string | null }> = [];
let rejectTarget = false;
let missingDefault = false;
function bootstrap(org: string): BootstrapResponse {
  return {
    account: { ...identity, permissions: org === 'home' ? [] : ['organization:manage', 'members:manage'] },
    csrf_token: 'csrf',
    organization_access: {
      source: org === 'home' ? 'organization_member' : 'platform_admin',
      organization: { uuid: org, name: org },
      organization_role: org === 'home' ? 'user' : 'admin',
      workspace_role: org === 'home' ? 'workspace_user' : 'workspace_admin',
    },
  };
}
function Probe() {
  organizations = useOrganizations()!;
  currentAccount = useAuth().account;
  const workspace = useWorkspace();
  activeWorkspace = workspace.activeWorkspaceId;
  selectWorkspace = workspace.selectWorkspace;
  return null;
}
function mount(navigateScope?: (workspaceId: string) => Promise<void>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={client}>
      <AuthContext.Provider
        value={{
          account: identity,
          status: 'authenticated',
          csrfToken: 'csrf',
          logout: async () => {},
          refresh: async () => ({ account: identity }),
        }}
      >
        <WorkspaceProvider navigateScope={navigateScope}>
          <Probe />
        </WorkspaceProvider>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
  return { ...view, client };
}
function setup() {
  resetTestDom('https://oma.duck.ai/');
  identity = {
    uuid: 'account',
    email_address: 'admin@example.com',
    default_organization_uuid: 'home',
    memberships: [{ organization: { uuid: 'home' }, role: 'user' }],
  };
  requests.length = 0;
  rejectTarget = false;
  missingDefault = false;
  globalThis.fetch = mock(async (input, options) => {
    const path = String(input);
    const organization = new Headers(options?.headers).get('X-Organization-UUID');
    requests.push({ path, organization });
    if (organization === 'target' && rejectTarget) return Response.json({ error: 'denied' }, { status: 403 });
    if (path === '/api/bootstrap') return Response.json(bootstrap(organization ?? 'home'));
    return Response.json(
      missingDefault && organization === 'target'
        ? []
        : [
            {
              id: `ws-${organization}`,
              name: 'Default',
              is_default: true,
              effective_role: organization === 'home' ? 'workspace_user' : 'workspace_admin',
            },
          ],
    );
  }) as typeof fetch;
}
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  window.sessionStorage.clear();
  window.localStorage.clear();
});

test('目标拒绝与缺失 Default 保留原作用域且不写进入偏好', async () => {
  setup();
  mount();
  await waitFor(() => expect(organizations.switching).toBe(false));
  for (const failure of ['authorization', 'default']) {
    rejectTarget = failure === 'authorization';
    missingDefault = failure === 'default';
    await act(async () => {
      await expect(organizations.enterOrganization!('target')).rejects.toBeTruthy();
    });
    expect(getConsoleRequestContext().organizationUuid).toBe('home');
    expect(organizations.orgUuid).toBe('home');
    expect(readPlatformScope(identity.uuid)).toBeUndefined();
  }
});

test('刷新恢复明确不匹配时清除平台偏好并回落普通组织', async () => {
  for (const failure of ['account', 'source', 'organization']) {
    setup();
    savePlatformScope({ accountUuid: identity.uuid, targetOrganizationUuid: 'target' });
    const fallbackFetch = globalThis.fetch;
    globalThis.fetch = mock(async (input, options) => {
      if (String(input) !== '/api/bootstrap' || new Headers(options?.headers).get('X-Organization-UUID') !== 'target')
        return fallbackFetch(input, options);
      const result = bootstrap('target');
      if (failure === 'account') result.account!.uuid = 'other';
      if (failure === 'source') result.organization_access!.source = 'organization_member';
      if (failure === 'organization') result.organization_access!.organization.uuid = 'other';
      return Response.json(result);
    }) as typeof fetch;
    const view = mount();
    await waitFor(() => expect(organizations.switching).toBe(false));
    expect(organizations.orgUuid).toBe('home');
    expect(organizations.error).toBeNull();
    expect(organizations.platformScope).toBeUndefined();
    expect(readPlatformScope(identity.uuid)).toBeUndefined();
    expect(getConsoleRequestContext().organizationUuid).toBe('home');
    view.unmount();
  }
});

test('刷新恢复遇到临时网络或服务故障保留进入偏好并可重试', async () => {
  for (const failure of ['network', 'service']) {
    setup();
    savePlatformScope({ accountUuid: identity.uuid, targetOrganizationUuid: 'target' });
    const fallbackFetch = globalThis.fetch;
    globalThis.fetch = mock(async () => {
      if (failure === 'network') throw new Error('offline');
      return Response.json({ error: 'unavailable' }, { status: 503 });
    }) as typeof fetch;
    const view = mount();
    await waitFor(() => expect(organizations.error).toBeTruthy());
    expect(organizations.platformScope?.targetOrganizationUuid).toBe('target');
    expect(readPlatformScope(identity.uuid)?.targetOrganizationUuid).toBe('target');
    expect(getConsoleRequestContext().organizationUuid).toBeUndefined();
    globalThis.fetch = fallbackFetch;
    await act(async () => {
      await organizations.retry();
    });
    expect(organizations.orgUuid).toBe('target');
    view.unmount();
  }
});

test('撤权停止目标请求并允许退出，不自动回退或重放写入', async () => {
  setup();
  mount();
  await waitFor(() => expect(organizations.switching).toBe(false));
  await act(async () => {
    await organizations.enterOrganization!('target');
  });
  const signal = getScopeSignal();
  act(() => reportApiAuthFailure(403, getConsoleRequestContext()));
  expect(signal.aborted).toBe(true);
  expect(getConsoleRequestContext()).toEqual({});
  expect(organizations.error).toBeTruthy();
  expect(currentAccount?.permissions).toEqual([]);
  expect(organizations.platformScope).toBeTruthy();
  await act(async () => {
    await organizations.exitOrganization!();
  });
  expect(getConsoleRequestContext().organizationUuid).toBeUndefined();
  expect(organizations.error).toBeNull();
});

test('非成员进入按目标 bootstrap 后加载工作区，普通偏好不污染，退出取消旧请求且不恢复原组织', async () => {
  setup();
  mount();
  await waitFor(() => expect(organizations.switching).toBe(false));
  const oldSignal = getScopeSignal();
  await act(async () => {
    await organizations.enterOrganization!('target');
  });
  expect(oldSignal.aborted).toBe(true);
  expect(getConsoleRequestContext().organizationUuid).toBe('target');
  expect(activeWorkspace).toBe('ws-target');
  expect(currentAccount?.permissions).toContain('organization:manage');
  expect(currentAccount?.memberships).toEqual([]);
  expect(organizations.memberships).toEqual(identity.memberships);
  expect(window.sessionStorage.getItem('oma.organization.account')).toBe('home');
  expect(requests.filter((request) => request.organization === 'target').map((request) => request.path)).toEqual([
    '/api/bootstrap',
    '/api/console/organizations/target/workspaces',
  ]);
  const targetSignal = getScopeSignal();
  await act(async () => {
    await organizations.enterOrganization!('another');
  });
  expect(organizations.platformScope?.targetOrganizationUuid).toBe('another');
  const exitSignal = getScopeSignal();
  const requestCount = requests.length;
  await act(async () => {
    await organizations.exitOrganization!();
  });
  expect(targetSignal.aborted).toBe(true);
  expect(getConsoleRequestContext().organizationUuid).toBeUndefined();
  expect(exitSignal.aborted).toBe(true);
  expect(getConsoleRequestContext().workspaceId).toBeUndefined();
  expect(organizations.orgUuid).toBeUndefined();
  expect(organizations.platformScope).toBeUndefined();
  expect(activeWorkspace).toBe('');
  expect(currentAccount?.permissions).toEqual([]);
  expect(requests).toHaveLength(requestCount);
  expect(readPlatformScope(identity.uuid)).toBeUndefined();
});

test('同账号刷新恢复标签目标，其他账号不能复用偏好', async () => {
  setup();
  savePlatformScope({ accountUuid: identity.uuid, targetOrganizationUuid: 'target' });
  const view = mount();
  await waitFor(() => expect(organizations.orgUuid).toBe('target'));
  expect(requests[0]).toEqual({ path: '/api/bootstrap', organization: 'target' });
  view.unmount();
  identity = { ...identity, uuid: 'other' };
  mount();
  await waitFor(() => expect(organizations.orgUuid).toBe('home'));
  expect(organizations.platformScope).toBeUndefined();
});

test('无真实组织仍可进入并退出为空组织', async () => {
  setup();
  identity = { ...identity, memberships: [] };
  mount();
  await waitFor(() => expect(organizations.switching).toBe(false));
  await act(async () => {
    await organizations.enterOrganization!('target');
  });
  await act(async () => {
    await organizations.exitOrganization!();
  });
  expect(organizations.orgUuid).toBeUndefined();
  expect(getConsoleRequestContext().organizationUuid).toBeUndefined();
});

test('退出无需网络且清除业务缓存，不恢复原组织或跳转原工作区', async () => {
  setup();
  const navigate = mock(async () => {});
  const view = mount(navigate);
  await waitFor(() => expect(organizations.switching).toBe(false));
  await act(async () => {
    await organizations.enterOrganization!('target');
  });
  view.client.setQueryData(['agents', 'target'], ['target-agent']);
  const pendingQuery = view.client
    .fetchQuery({
      queryKey: ['sessions', 'target'],
      queryFn: ({ signal }) =>
        new Promise((_, reject) => {
          signal.addEventListener('abort', () => reject(new Error('aborted')));
        }),
    })
    .catch(() => undefined);
  const requestCount = requests.length;
  navigate.mockClear();
  globalThis.fetch = mock(async () => {
    throw new Error('offline');
  }) as typeof fetch;
  await act(async () => {
    expect(await organizations.exitOrganization!()).toBe(true);
    await pendingQuery;
  });
  expect(view.client.getQueryData(['agents', 'target'])).toBeUndefined();
  expect(view.client.getQueryState(['sessions', 'target'])).toBeUndefined();
  expect(organizations.orgUuid).toBeUndefined();
  expect(organizations.error).toBeNull();
  expect(organizations.switching).toBe(false);
  expect(getConsoleRequestContext().organizationUuid).toBeUndefined();
  expect(readPlatformScope(identity.uuid)).toBeUndefined();
  expect(requests).toHaveLength(requestCount);
  expect(globalThis.fetch).not.toHaveBeenCalled();
  expect(navigate).not.toHaveBeenCalled();
});

test('进入响应中的其他账号或非平台权限不能安装目标', async () => {
  setup();
  mount();
  await waitFor(() => expect(organizations.switching).toBe(false));
  const fallbackFetch = globalThis.fetch;
  for (const failure of ['account', 'source', 'organization']) {
    globalThis.fetch = mock(async (input, options) => {
      if (String(input) !== '/api/bootstrap') return fallbackFetch(input, options);
      const result = bootstrap('target');
      if (failure === 'account') result.account!.uuid = 'other';
      if (failure === 'source') result.organization_access!.source = 'organization_member';
      if (failure === 'organization') result.organization_access!.organization.uuid = 'other';
      return Response.json(result);
    }) as typeof fetch;
    await act(async () => {
      await expect(organizations.enterOrganization!('target')).rejects.toBeTruthy();
    });
    expect(organizations.orgUuid).toBe('home');
    expect(readPlatformScope(identity.uuid)).toBeUndefined();
  }
});

test('退出使仍在等待的进入响应失效', async () => {
  setup();
  mount();
  await waitFor(() => expect(organizations.switching).toBe(false));
  const fallbackFetch = globalThis.fetch;
  let complete!: (response: Response) => void;
  globalThis.fetch = mock((input, options) =>
    new Headers(options?.headers).get('X-Organization-UUID') === 'target'
      ? new Promise<Response>((resolve) => {
          complete = resolve;
        })
      : fallbackFetch(input, options),
  ) as typeof fetch;
  let enter!: Promise<boolean>;
  act(() => {
    enter = organizations.enterOrganization!('target');
  });
  await waitFor(() => expect(complete).toBeDefined());
  await act(async () => {
    await organizations.exitOrganization!();
  });
  await act(async () => {
    complete(Response.json(bootstrap('target')));
    await enter.catch(() => false);
  });
  expect(organizations.orgUuid).toBeUndefined();
  expect(organizations.switching).toBe(false);
  expect(readPlatformScope(identity.uuid)).toBeUndefined();
});

test('目标工作区选择保存在标签偏好和当前模式，权限恢复重试保留该选择', async () => {
  setup();
  const fallbackFetch = globalThis.fetch;
  globalThis.fetch = mock(async (input, options) => {
    if (String(input).endsWith('/target/workspaces'))
      return Response.json([
        { id: 'ws-target', name: 'Default', is_default: true, effective_role: 'workspace_admin' },
        { id: 'ws-custom', name: 'Custom', effective_role: 'workspace_admin' },
      ]);
    return fallbackFetch(input, options);
  }) as typeof fetch;
  mount();
  await waitFor(() => expect(organizations.switching).toBe(false));
  await act(async () => {
    await organizations.enterOrganization!('target');
  });
  await act(async () => {
    selectWorkspace('ws-custom');
  });
  expect(organizations.platformScope?.targetWorkspaceId).toBe('ws-custom');
  expect(readPlatformScope(identity.uuid)?.targetWorkspaceId).toBe('ws-custom');
  act(() => reportApiAuthFailure(403, getConsoleRequestContext()));
  await act(async () => {
    await organizations.retry();
  });
  expect(activeWorkspace).toBe('ws-custom');
});
