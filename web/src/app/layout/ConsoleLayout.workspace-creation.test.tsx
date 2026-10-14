import { afterEach, expect, mock, test } from 'bun:test';
import { useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { resetTestDom } from '../../test/setup';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { WorkspaceProvider } from '../../shared/workspaces/WorkspaceProvider';
import { useScopeUnsavedChanges } from '../../shared/organizations/unsaved';
import { setConsoleRequestContext } from '../../shared/api/client';
import { ConsoleShell } from './ConsoleLayout';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const auth: AuthContextValue = {
  account: {
    uuid: 'account',
    email_address: 'admin@example.com',
    default_organization_uuid: 'a',
    memberships: [{ role: 'admin', organization: { uuid: 'a' }, user_id: 'admin-a' }],
  },
  csrfToken: 'csrf',
  status: 'authenticated',
  logout: async () => {},
  refresh: async () => ({ account: auth.account }),
};

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

function mount(dirty = false) {
  resetTestDom('https://oma.duck.ai/workspaces/ws-a/environments/env-old');
  const created = { id: 'ws-new', name: 'new', type: 'workspace', effective_role: 'workspace_admin' };
  const createRequest = mock(async () => Response.json(created));
  globalThis.fetch = mock(async (_input, options) =>
    options?.method === 'POST'
      ? createRequest()
      : Response.json([
          { id: 'ws-a', name: 'Default', type: 'workspace', is_default: true, effective_role: 'workspace_user' },
        ]),
  ) as typeof fetch;
  const scopeNavigation = mock(async (_id: string) => {});
  const shellNavigation = mock(async (_href: string) => {});
  function Page() {
    const [path, setPath] = useState('/workspaces/ws-a/environments/env-old');
    useScopeUnsavedChanges(dirty);
    return (
      <WorkspaceProvider
        navigateScope={async (id) => {
          await scopeNavigation(id);
          setPath(`/workspaces/${id}/environments`);
        }}
      >
        <ConsoleShell
          account={auth.account}
          currentPath={path}
          onLogout={auth.logout}
          onNavigate={async (href) => {
            await shellNavigation(href);
            setPath(href);
          }}
        >
          <output>{path}</output>
          <input aria-label="Draft" defaultValue="unsaved draft" />
        </ConsoleShell>
      </WorkspaceProvider>
    );
  }
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <AuthContext.Provider value={auth}>
        <Page />
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
  return { createRequest, scopeNavigation, shellNavigation };
}

async function openCreate() {
  await waitFor(() => expect(screen.getByRole('button', { name: 'Default', exact: true })).toBeTruthy());
  fireEvent.click(screen.getByRole('button', { name: 'Default', exact: true }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Create workspace' }));
}

test('创建工作区前确认未保存内容，取消保持草稿且不创建或导航', async () => {
  const { createRequest, scopeNavigation, shellNavigation } = mount(true);
  await openCreate();
  expect(screen.getByRole('alertdialog')).toBeTruthy();
  expect(screen.queryByLabelText('Name')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '继续编辑' }));
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
  expect(screen.getByLabelText('Draft').getAttribute('value')).toBe('unsaved draft');
  expect(createRequest).not.toHaveBeenCalled();
  expect(scopeNavigation).not.toHaveBeenCalled();
  expect(shellNavigation).not.toHaveBeenCalled();
});

test.each([false, true])('创建成功只导航一次并移除旧详情ID，未保存内容：%s', async (dirty) => {
  const { createRequest, scopeNavigation, shellNavigation } = mount(dirty);
  await openCreate();
  if (dirty) fireEvent.click(screen.getByRole('button', { name: '放弃更改并切换' }));
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'new' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create', exact: true }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'new', exact: true })).toBeTruthy());
  expect(createRequest).toHaveBeenCalledTimes(1);
  expect(scopeNavigation).toHaveBeenCalledTimes(1);
  expect(scopeNavigation).toHaveBeenCalledWith('ws-new');
  expect(shellNavigation).not.toHaveBeenCalled();
  expect(screen.getByText('/workspaces/ws-new/environments')).toBeTruthy();
});

test('创建失败保留当前工作区和草稿，重试成功后只导航一次', async () => {
  const { createRequest, scopeNavigation, shellNavigation } = mount(true);
  createRequest.mockImplementationOnce(async () =>
    Response.json({ error: { message: 'create unavailable' } }, { status: 503 }),
  );
  await openCreate();
  fireEvent.click(screen.getByRole('button', { name: '放弃更改并切换' }));
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'new' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create', exact: true }));
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('create unavailable'));
  expect(screen.getByRole('button', { name: 'Default', exact: true, hidden: true })).toBeTruthy();
  expect(screen.getByLabelText('Draft').getAttribute('value')).toBe('unsaved draft');
  expect(scopeNavigation).not.toHaveBeenCalled();
  expect(shellNavigation).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Create', exact: true }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'new', exact: true })).toBeTruthy());
  expect(createRequest).toHaveBeenCalledTimes(2);
  expect(scopeNavigation).toHaveBeenCalledTimes(1);
  expect(shellNavigation).not.toHaveBeenCalled();
});
