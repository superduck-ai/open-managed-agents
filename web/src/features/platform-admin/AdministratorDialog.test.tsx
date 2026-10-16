import { afterEach, expect, mock, test } from 'bun:test';
import { renderPlatformAdmin as render } from '../../test/platform-admin';
import { resetTestDom } from '../../test/setup';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PlatformAdminClient } from '../../shared/api/platform-admin';
import { PlatformAdminContext } from './context';
import { AdministratorDialog } from './AdministratorDialog';

const { cleanup, screen, fireEvent } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('修改角色提交目标组织、成员及版本，服务端拒绝后保留表单', async () => {
  resetTestDom('https://oma.duck.ai/platform-admin');
  let path = '';
  let body: unknown;
  globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
    path = String(url);
    body = JSON.parse(String(options?.body));
    return Response.json(
      { error: { code: 'last_organization_admin', message: '必须保留至少一位组织管理员' } },
      { status: 409 },
    );
  }) as unknown as typeof fetch;
  const queries = new QueryClient();
  const client = new PlatformAdminClient(() => {});
  const saved = mock();
  render(
    <QueryClientProvider client={queries}>
      <PlatformAdminContext.Provider
        value={{
          client,
          bootstrap: {
            account: { uuid: 'operator', name: '', email: '' },
            platform_access: { role: 'super_admin', permissions: ['platform:access'] },
            csrf_token: '',
          },
        }}
      >
        <AdministratorDialog
          organization={{
            uuid: 'target-organization',
            name: '目标组织',
            website: '',
            industry: '',
            version: 9,
            admin_count: 2,
            member_count: 2,
            workspace_count: 1,
            created_at: '',
          }}
          action={{
            kind: 'change-role',
            member: { uuid: 'target-member', email: 'admin@example.com', name: '', role: 'admin' },
          }}
          onClose={() => {}}
          onSaved={saved}
        />
      </PlatformAdminContext.Provider>
    </QueryClientProvider>,
  );
  expect(screen.getByRole('button', { name: '确认变更' }).hasAttribute('disabled')).toBe(true);
  const select = screen.getByRole('combobox', { name: '组织角色' });
  select.focus();
  fireEvent.keyDown(select, { key: 'ArrowDown' });
  fireEvent.keyDown(await screen.findByRole('option', { name: /^User/ }), { key: 'Enter' });
  fireEvent.click(screen.getByRole('button', { name: '确认变更' }));
  expect((await screen.findByRole('alert')).textContent).toContain('必须保留至少一位组织管理员');
  expect(path).toBe('/api/platform-admin/organizations/target-organization/administrators/target-member');
  expect(body).toEqual({ expected_version: 9, role: 'user' });
  expect(screen.queryByLabelText('操作原因')).toBeNull();
  expect(saved).not.toHaveBeenCalled();
  queries.clear();
});
