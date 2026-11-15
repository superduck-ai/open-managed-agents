import { I18nProvider, type Locale } from '../../shared/i18n';
import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../test/setup';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthContext } from '../../shared/auth/context';
import { OrganizationContext } from '../../shared/organizations/context';
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '../../shared/ui/dropdown-menu';
import { Button } from '../../shared/ui/button';
import { OrganizationMenu } from './OrganizationMenu';
import { setConsoleRequestContext } from '../../shared/api/client';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const invitation = {
  id: 'invite',
  organization_uuid: 'new-org',
  organization_name: '受邀组织',
  role: 'user',
  invited_at: '2026-01-01',
  expires_at: '2027-01-01',
};
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

function mount(
  memberships = [{ organization: { uuid: 'current', name: '当前组织' }, role: 'admin' }],
  locale: Locale = 'zh-CN',
) {
  resetTestDom('https://oma.duck.ai/dashboard');
  const switchOrganization = mock(async () => true);
  const refresh = mock(async () => ({ account: null }));
  const account = {
    uuid: 'account',
    email_address: 'a@example.com',
    memberships,
  };
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nProvider initialLocale={locale}>
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={{ account, status: 'authenticated', refresh, logout: async () => {} }}>
          <OrganizationContext.Provider
            value={{
              memberships: account.memberships,
              orgUuid: 'current',
              switching: false,
              error: null,
              switchOrganization,
              retry: async () => true,
            }}
          >
            <DropdownMenu defaultOpen>
              <DropdownMenuTrigger render={<Button />}>账号</DropdownMenuTrigger>
              <DropdownMenuContent>
                <OrganizationMenu />
              </DropdownMenuContent>
            </DropdownMenu>
          </OrganizationContext.Provider>
        </AuthContext.Provider>
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { switchOrganization, refresh, queryClient };
}

test('组织菜单显示当前 membership 的角色，邀请入口由顶部通知提供', async () => {
  globalThis.fetch = mock(async () => Response.json({ data: [invitation] })) as typeof fetch;
  mount();
  expect(await screen.findByRole('menuitemradio', { name: /当前组织/ })).toBeTruthy();
  expect(screen.getByText('管理员')).toBeTruthy();
  expect(screen.queryByRole('menuitem', { name: '切换组织' })).toBeNull();
  expect(screen.queryByRole('menuitem', { name: /组织邀请/ })).toBeNull();
  expect(globalThis.fetch).not.toHaveBeenCalled();
});

test('组织很多时菜单只保留当前组织，搜索后再切换', async () => {
  globalThis.fetch = mock(async () => Response.json({ data: [] })) as typeof fetch;
  const memberships = [
    { organization: { uuid: 'current', name: '集团-总部-财务中心-财务部' }, role: 'admin' },
    { organization: { uuid: 'other', name: '伙伴公司-星河-财务部' }, role: 'user' },
    ...Array.from({ length: 8 }, (_, index) => ({
      organization: { uuid: `org-${index}`, name: `集团-分部${index}` },
      role: 'admin',
    })),
  ];
  const { switchOrganization } = mount(memberships);
  expect(screen.queryByText('分部7')).toBeNull();
  expect(screen.getAllByRole('menuitemradio')).toHaveLength(3);
  expect(screen.getByText('财务中心 / 总部 / 集团')).toBeTruthy();
  fireEvent.click(screen.getByRole('menuitem', { name: '切换组织' }));
  expect(await screen.findByRole('option', { name: /星河/ })).toBeTruthy();
  expect(screen.queryByText(/还有 \d+ 个组织/)).toBeNull();
  fireEvent.change(screen.getByRole('combobox', { name: '搜索组织' }), { target: { value: '星河' } });
  fireEvent.click(await screen.findByRole('option', { name: /星河/ }));
  expect(switchOrganization).toHaveBeenCalledWith('other');
  fireEvent.click(screen.getByRole('menuitem', { name: '切换组织' }));
  expect(screen.getByRole('combobox', { name: '搜索组织' })).toHaveProperty('value', '');
  expect(await screen.findByRole('option', { name: /星河/ })).toBeTruthy();
  fireEvent.change(screen.getByRole('combobox', { name: '搜索组织' }), { target: { value: '不存在' } });
  expect(screen.queryByRole('option', { name: /星河/ })).toBeNull();
});

test('uses English organization search, counts and roles while preserving the selected identity', async () => {
  globalThis.fetch = mock(async () => Response.json({ data: [invitation] })) as typeof fetch;
  const memberships = [
    { organization: { uuid: 'current', name: 'Current organization' }, role: 'admin' },
    ...Array.from({ length: 45 }, (_, index) => ({
      organization: { uuid: `org-${index}`, name: `Research Department ${index}` },
      role: 'user',
    })),
  ];
  const { switchOrganization } = mount(memberships, 'en');
  expect(screen.queryByRole('menuitem', { name: /Organization invitations/ })).toBeNull();
  expect(screen.getByText('Admin')).toBeTruthy();
  fireEvent.click(screen.getByRole('menuitem', { name: 'Switch organization' }));
  const search = await screen.findByRole('combobox', { name: 'Search organizations' });
  expect(screen.getAllByRole('option')).toHaveLength(40);
  expect(screen.getByText(/6 more organizations/)).toBeTruthy();
  fireEvent.change(search, { target: { value: 'Research' } });
  expect(await screen.findByText(/5 more matches/)).toBeTruthy();
  expect(screen.getAllByRole('option')).toHaveLength(40);
  expect(screen.getAllByText('User').length).toBeGreaterThan(0);
  fireEvent.change(search, { target: { value: 'no matching organization' } });
  expect(await screen.findByText('No matching organizations.')).toBeTruthy();
  expect(switchOrganization).not.toHaveBeenCalled();
});

test('returns focus to the organization menu after dismissing search and clears the next search', async () => {
  globalThis.fetch = mock(async () => Response.json({ data: [invitation] })) as typeof fetch;
  const memberships = [
    { organization: { uuid: 'current', name: 'Current organization' }, role: 'admin' },
    ...Array.from({ length: 4 }, (_, index) => ({
      organization: { uuid: `org-${index}`, name: `Research Department ${index}` },
      role: 'user',
    })),
  ];
  const { switchOrganization } = mount(memberships, 'en');
  const trigger = await screen.findByRole('menuitem', { name: 'Switch organization' });
  trigger.focus();
  fireEvent.click(trigger);
  const search = await screen.findByRole('combobox', { name: 'Search organizations' });
  await waitFor(() => expect(document.activeElement).toBe(search));
  fireEvent.change(search, { target: { value: 'Research' } });
  fireEvent.keyDown(search, { key: 'Escape', code: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Switch organization' })).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(trigger));
  fireEvent.click(trigger);
  expect((await screen.findByRole('combobox', { name: 'Search organizations' })).getAttribute('value')).toBe('');
  expect(switchOrganization).not.toHaveBeenCalled();
});
