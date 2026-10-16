import { afterEach, expect, mock, test } from 'bun:test';
import { renderPlatformAdmin as render } from '../../test/platform-admin';
import { resetTestDom } from '../../test/setup';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PlatformAdminClient } from '../../shared/api/platform-admin';
import { PlatformAdminContext } from './context';
import { InvitationDialog } from './InvitationDialog';

const { cleanup, screen, fireEvent, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

for (const kind of ['create', 'resend', 'revoke'] as const) {
  test(`${kind} 按组织版本提交且只允许当前组织角色`, async () => {
    resetTestDom('https://oma.duck.ai/platform-admin');
    let path = '';
    let body: unknown;
    globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
      path = String(url);
      body = JSON.parse(String(options?.body));
      return Response.json({ uuid: 'organization', version: 12 });
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
          <InvitationDialog
            organization={{
              uuid: 'organization',
              name: '组织',
              website: '',
              industry: '',
              version: 11,
              admin_count: 1,
              member_count: 1,
              workspace_count: 1,
              created_at: '',
            }}
            action={
              kind === 'create'
                ? { kind }
                : {
                    kind,
                    invitation: {
                      uuid: 'invitation',
                      id: 'invite_external',
                      email: 'invited@example.com',
                      role: 'user',
                      status: 'pending',
                      invited_at: '',
                      expires_at: '',
                      delivery_status: 'failed',
                    },
                  }
            }
            onClose={() => {}}
            onSaved={saved}
          />
        </PlatformAdminContext.Provider>
      </QueryClientProvider>,
    );
    expect((screen.getByLabelText('受邀邮箱') as HTMLInputElement).disabled).toBe(kind !== 'create');
    if (kind === 'create') {
      fireEvent.change(screen.getByLabelText('受邀邮箱'), { target: { value: 'invited@example.com' } });
      const select = screen.getByRole('combobox', { name: '组织角色' });
      select.focus();
      fireEvent.keyDown(select, { key: 'ArrowDown' });
      const admin = await screen.findByRole('option', { name: /^管理员/ });
      expect(screen.getAllByRole('option')).toHaveLength(2);
      fireEvent.keyDown(admin, { key: 'Enter' });
    } else {
      expect(screen.getByRole('combobox', { name: '组织角色' }).hasAttribute('disabled')).toBe(true);
    }
    expect(screen.getByRole('button', { name: '确认', exact: true }).hasAttribute('disabled')).toBe(false);
    expect(screen.queryByLabelText('操作原因')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '确认', exact: true }));
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(1));
    expect(path).toBe(
      `/api/platform-admin/organizations/organization/invitations${kind === 'create' ? '' : `/invitation/${kind}`}`,
    );
    expect(body).toEqual({
      expected_version: 11,

      ...(kind === 'create' ? { email: 'invited@example.com', role: 'admin' } : {}),
    });
    queries.clear();
  });
}
