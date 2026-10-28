import '../../test/setup';
import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query';
import { afterEach, describe, expect, test } from 'bun:test';
import { useMemo, type ReactNode } from 'react';
import { SettingsShell } from '../../app/layout/ConsoleLayout';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { OrganizationMembersPage } from './OrganizationMembersPage';
import type { OrganizationInvite, OrganizationMember } from './membersApi';

const testingLibrary = await import('@testing-library/react');
const { act, cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

const originalFetch = globalThis.fetch;
const originalOnline = onlineManager.isOnline();
const queryClients: QueryClient[] = [];

afterEach(() => {
  cleanup();
  queryClients.splice(0).forEach((client) => client.clear());
  onlineManager.setOnline(originalOnline);
  globalThis.fetch = originalFetch;
});

describe('Organization members settings', () => {
  test('distinguishes filtered results from an empty directory and restores members when search clears', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    const api = mockMembersApi();
    render(
      <OrganizationMembersHarness>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );
    await screen.findByText('Ada Lovelace');
    fireEvent.change(screen.getByRole('textbox', { name: 'Search members' }), {
      target: { value: 'unmatched-person' },
    });
    expect(await screen.findByText('No matching members')).toBeTruthy();
    expect(screen.queryByText('No members yet')).toBeNull();
    expect(screen.queryByText('Invite people to join this organization.')).toBeNull();
    expect(screen.queryByRole('columnheader', { name: 'Email' })).toBeNull();
    fireEvent.change(screen.getByRole('textbox', { name: 'Search members' }), { target: { value: '' } });
    expect(await screen.findByText('Ada Lovelace')).toBeTruthy();
    expect(screen.getByRole('columnheader', { name: 'Email' })).toBeTruthy();
    expect(api.inviteCreates).toHaveLength(0);
  });

  test('retains invitation input through cancellation and resets it before reopening', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    mockMembersApi();
    render(
      <OrganizationMembersHarness>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );
    fireEvent.click(await screen.findByRole('button', { name: 'Invite' }));
    const dialog = await screen.findByRole('dialog', { name: 'Invite' });
    fireEvent.change(within(dialog).getByLabelText('Email addresses'), { target: { value: 'person@example.test' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(document.querySelector<HTMLTextAreaElement>('#invite-emails')?.value).toBe('person@example.test');
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    fireEvent.click(screen.getByRole('button', { name: 'Invite' }));
    expect(within(await screen.findByRole('dialog')).getByLabelText('Email addresses')).toHaveProperty('value', '');
  });

  test('blocks closing a pending invitation and keeps its input after failure', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    mockMembersApi();
    const mockedFetch = globalThis.fetch;
    let finishRequest: (response: Response) => void = () => {};
    globalThis.fetch = (input, init) =>
      init?.method === 'POST'
        ? new Promise<Response>((resolve) => {
            finishRequest = resolve;
          })
        : mockedFetch(input, init);
    render(
      <OrganizationMembersHarness>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );
    fireEvent.click(await screen.findByRole('button', { name: 'Invite' }));
    const dialog = await screen.findByRole('dialog', { name: 'Invite' });
    fireEvent.change(within(dialog).getByLabelText('Email addresses'), { target: { value: 'person@example.test' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Invite', exact: true }));
    await waitFor(() =>
      expect(within(dialog).getByRole('button', { name: 'Close' }).hasAttribute('disabled')).toBe(true),
    );
    fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
    expect(screen.getByRole('dialog')).toBe(dialog);
    finishRequest(Response.json({ error: { message: 'Example invitation failed' } }, { status: 500 }));
    await within(dialog).findByText('Example invitation failed');
    expect(within(dialog).getByLabelText('Email addresses')).toHaveProperty('value', 'person@example.test');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });

  for (const cachedResource of [null, 'members', 'invites'] as const) {
    test(`does not show an empty directory while offline with ${cachedResource ?? 'no'} cached data and resumes online`, async () => {
      resetTestDom('https://oma.duck.ai/settings/members');
      const api = mockMembersApi();
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
      if (cachedResource) {
        const queryKey =
          cachedResource === 'members'
            ? ['console', 'organization-members', 'org_test']
            : ['console', 'organization-invites', 'org_test', 'pending'];
        queryClient.setQueryData(queryKey, []);
      }
      onlineManager.setOnline(false);

      const { container } = render(
        <OrganizationMembersHarness queryClient={queryClient}>
          <OrganizationMembersPage />
        </OrganizationMembersHarness>,
      );

      const pendingKey =
        cachedResource === 'members'
          ? ['console', 'organization-invites', 'org_test', 'pending']
          : ['console', 'organization-members', 'org_test'];
      expect(queryClient.getQueryState(pendingKey)).toMatchObject({ status: 'pending', fetchStatus: 'paused' });
      expect(api.memberListRequests).toBe(0);
      expect(api.inviteListRequests).toBe(0);
      expect(screen.queryByText('No members yet') === null).toBe(true);
      expect(screen.queryByText('0', { exact: true }) === null).toBe(true);
      expect(container.querySelector('[data-slot="skeleton"]') !== null).toBe(true);

      await act(async () => onlineManager.setOnline(true));

      const expectedCount = cachedResource === 'members' ? '1' : cachedResource === 'invites' ? '2' : '3';
      expect(await screen.findByText(expectedCount, { exact: true })).toBeTruthy();
      expect(screen.getByText(cachedResource === 'members' ? 'pending@example.com' : 'Ada Lovelace')).toBeTruthy();
      expect(screen.queryByText('No members yet')).toBeNull();
      expect(container.querySelector('[data-slot="skeleton"]')).toBeNull();
    });
  }

  test('shows zero and an empty directory only after both requests succeed with empty lists', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    globalThis.fetch = (async () => jsonResponse([])) as typeof fetch;

    const { container } = render(
      <OrganizationMembersHarness>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );

    expect(screen.queryByText('0', { exact: true }) === null).toBe(true);
    expect(container.querySelector('[data-slot="skeleton"]') !== null).toBe(true);
    expect(await screen.findByText('No members yet')).toBeTruthy();
    expect(screen.getByText('0', { exact: true })).toBeTruthy();
    expect(container.querySelector('[data-slot="skeleton"]')).toBeNull();
  });

  test('keeps the complete cached directory visible when a remount pauses its refresh offline', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    const api = mockMembersApi();
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const mount = () =>
      render(
        <OrganizationMembersHarness queryClient={queryClient}>
          <OrganizationMembersPage />
        </OrganizationMembersHarness>,
      );
    const view = mount();
    expect(await screen.findByText('3', { exact: true })).toBeTruthy();
    view.unmount();
    onlineManager.setOnline(false);

    const { container } = mount();

    expect(queryClient.getQueryState(['console', 'organization-members', 'org_test'])).toMatchObject({
      status: 'success',
      fetchStatus: 'paused',
    });
    expect(screen.getByText('Ada Lovelace')).toBeTruthy();
    expect(screen.getByText('pending@example.com')).toBeTruthy();
    expect(screen.getByText('3', { exact: true })).toBeTruthy();
    expect(container.querySelector('[data-slot="skeleton"]')).toBeNull();
    expect(api.memberListRequests).toBe(1);
    expect(api.inviteListRequests).toBe(1);
  });

  test('用户只读组织成员且不请求邀请目录', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    const api = mockMembersApi();
    render(
      <OrganizationMembersHarness permissions={['members:view']}>
        <I18nProvider initialLocale="en">
          <OrganizationMembersPage />
        </I18nProvider>
      </OrganizationMembersHarness>,
    );
    await screen.findByRole('table');
    await waitFor(() => expect(api.memberListRequests).toBe(1));
    expect(api.inviteListRequests).toBe(0);
    expect(screen.queryByRole('button', { name: 'Invite' })).toBeNull();
    expect(screen.queryByRole('combobox', { name: /Role for/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /More actions/ })).toBeNull();
  });

  test('shows permission guidance without requesting members for an invited user', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    const api = mockMembersApi();

    render(
      <OrganizationMembersHarness permissions={[]}>
        <I18nProvider initialLocale="zh-CN">
          <OrganizationMembersPage />
        </I18nProvider>
      </OrganizationMembersHarness>,
    );

    expect(await screen.findByText('无权限访问')).toBeTruthy();
    expect(
      screen.getByText('组织成员可以查看成员目录。只有组织管理员可以管理组织成员。请联系组织管理员。'),
    ).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
    expect(screen.queryByRole('button', { name: '邀请' })).toBeNull();
    expect(screen.queryByRole('button', { name: '重试' })).toBeNull();
    expect(screen.queryByText('0')).toBeNull();
    expect(api.memberListRequests).toBe(0);
    expect(api.inviteListRequests).toBe(0);
  });

  for (const forbiddenResource of ['members', 'invites'] as const) {
    test(`shows permission guidance when the server denies ${forbiddenResource} to a stale administrator`, async () => {
      resetTestDom('https://oma.duck.ai/settings/members');
      mockMembersApi({ forbiddenResource });

      render(
        <OrganizationMembersHarness>
          <OrganizationMembersPage />
        </OrganizationMembersHarness>,
      );

      expect(await screen.findByText('Access denied')).toBeTruthy();
      expect(
        screen.getByText(
          'Organization members can view this directory. Only organization administrators can manage organization members. Contact your organization administrator.',
        ),
      ).toBeTruthy();
      expect(screen.queryByText('Members could not be loaded.')).toBeNull();
      expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();
      expect(screen.queryByRole('button', { name: 'Invite' })).toBeNull();
      expect(screen.queryByRole('table')).toBeNull();
      expect(screen.queryByText('0')).toBeNull();
    });
  }

  test('hides cached administrator data when the current organization permissions change', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    mockMembersApi();

    const view = render(
      <OrganizationMembersHarness>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );
    expect(await screen.findByText('Ada Lovelace')).toBeTruthy();

    view.rerender(
      <OrganizationMembersHarness permissions={[]}>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );

    expect(await screen.findByText('Access denied')).toBeTruthy();
    expect(screen.queryByText('Ada Lovelace')).toBeNull();
    expect(screen.queryByText('pending@example.com')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Invite' })).toBeNull();
    expect(screen.queryByRole('table')).toBeNull();
  });

  test('renders the official members shell and table from the console API', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    mockMembersApi();

    render(
      <OrganizationMembersHarness>
        <SettingsShell
          currentPath="/settings/members"
          account={{
            uuid: 'acct_test',
            email_address: 'test@example.com',
            display_name: 'test',
            effectiveOrganizationUuid: 'org_test',
            permissions: ['organization:manage', 'members:manage', 'members:view'],
            memberships: [{ organization: { uuid: 'org_test', name: 'default' }, role: 'admin' }],
          }}
          onLogout={() => undefined}
        >
          <OrganizationMembersPage />
        </SettingsShell>
      </OrganizationMembersHarness>,
    );

    expect(
      screen
        .getAllByRole('button', { name: /Default/i })
        .some((button) => button.getAttribute('aria-label') === 'Default'),
    ).toBe(true);
    expect(screen.getByText('Organization settings')).toBeTruthy();
    expect(await screen.findByRole('heading', { name: 'Members' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Invite' })).toBeTruthy();

    const table = screen.getByRole('table', { name: 'Members' });
    expect(await within(table).findByRole('columnheader', { name: 'Name' })).toBeTruthy();
    expect(within(table).getByRole('columnheader', { name: 'Email' })).toBeTruthy();
    expect(within(table).getByRole('columnheader', { name: 'Role' })).toBeTruthy();
    expect(await within(table).findByText('Current User')).toBeTruthy();
    expect(within(table).getByText('test@example.com')).toBeTruthy();
    expect(within(table).getByText('Pending')).toBeTruthy();
    expect(within(table).getByText('pending@example.com')).toBeTruthy();
    expect(within(table).getByRole('button', { name: 'More actions' })).toBeTruthy();
    expect(within(table).getByText('Ada Lovelace')).toBeTruthy();
    expect(within(table).getByText('ada@example.com')).toBeTruthy();
    expect(screen.queryByRole('combobox', { name: 'Role for Current User' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'More actions for Current User' })).toBeNull();
    expect(screen.getByRole('button', { name: 'More actions for Ada Lovelace' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'Role for Ada Lovelace' })).toBeTruthy();
  });

  test('filters the member directory by search keyword', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    mockMembersApi();

    render(
      <OrganizationMembersHarness>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );

    expect(await screen.findByText('Ada Lovelace')).toBeTruthy();
    expect(screen.getByText('3')).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'Role' })).toBeTruthy();

    const table = screen.getByRole('table', { name: 'Members' });
    fireEvent.change(screen.getByRole('textbox', { name: 'Search members' }), { target: { value: 'Ada' } });
    expect(within(table).getByText('Ada Lovelace')).toBeTruthy();
    expect(within(table).queryByText('Current User')).toBeNull();
    expect(within(table).queryByText('pending@example.com')).toBeNull();

    fireEvent.change(screen.getByRole('textbox', { name: 'Search members' }), { target: { value: '' } });
    expect(within(table).getByText('Current User')).toBeTruthy();
    expect(within(table).getByText('pending@example.com')).toBeTruthy();
  });

  test('retries both members and pending invites after a shared table load failure', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    const api = mockMembersApi({ failMembersOnce: true, failInvitesOnce: true });

    render(
      <OrganizationMembersHarness>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText('Members could not be loaded.')).toBeTruthy();
    expect(within(alert).getByText('Try again.')).toBeTruthy();
    fireEvent.click(within(alert).getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('heading', { name: 'Members' })).toBeTruthy();
    expect(screen.getByText('pending@example.com')).toBeTruthy();
    await waitFor(() => expect(api.memberListRequests).toBe(2));
    await waitFor(() => expect(api.inviteListRequests).toBe(2));
  });

  test('does not render inline status chrome for invite actions', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    mockMembersApi();

    const { container } = render(
      <OrganizationMembersHarness>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );

    expect(await screen.findByRole('heading', { name: 'Members' })).toBeTruthy();
    expect(container.querySelector('[role="status"]')).toBeNull();
    expect(container.querySelector('.text-emerald-600')).toBeNull();
  });

  test('keeps an organization named Default as the stored name', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    mockMembersApi();

    render(
      <I18nProvider initialLocale="zh-CN">
        <OrganizationMembersHarness organizationName="Default">
          <OrganizationMembersPage />
        </OrganizationMembersHarness>
      </I18nProvider>,
    );

    expect(within(screen.getByTestId('organization-members-page')).getByText('Default')).toBeTruthy();
  });

  test('localizes the seeded workspace name when the organization name is missing', async () => {
    resetTestDom('https://oma.duck.ai/settings/members');
    mockMembersApi();

    render(
      <I18nProvider initialLocale="zh-CN">
        <OrganizationMembersHarness organizationName="">
          <OrganizationMembersPage />
        </OrganizationMembersHarness>
      </I18nProvider>,
    );

    expect(within(screen.getByTestId('organization-members-page')).getByText('默认')).toBeTruthy();
  });

  test('renders the no-organization empty state without legacy surface-card chrome', () => {
    resetTestDom('https://oma.duck.ai/settings/members');

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const authValue: AuthContextValue = {
      account: {
        uuid: 'acct_test',
        email_address: 'test@example.com',
        display_name: 'test',
        memberships: [],
      },
      status: 'authenticated',
      csrfToken: 'csrf_test',
      refresh: async () => ({ account: { uuid: 'acct_test', email_address: 'test@example.com' } }),
      logout: async () => undefined,
    };
    const workspaceValue: WorkspaceContextValue = {
      orgUuid: null,
      workspaces: [defaultWorkspace],
      activeWorkspace: defaultWorkspace,
      activeWorkspaceId: defaultWorkspace.id,
      isLoading: false,
      error: null,
      selectWorkspace: () => undefined,
      createWorkspace: async () => defaultWorkspace,
      refreshWorkspaces: async () => undefined,
    };

    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue}>
          <WorkspaceContext.Provider value={workspaceValue}>
            <SettingsShell currentPath="/settings/members" account={authValue.account} onLogout={() => undefined}>
              <OrganizationMembersPage />
            </SettingsShell>
          </WorkspaceContext.Provider>
        </AuthContext.Provider>
      </QueryClientProvider>,
    );

    expect(screen.getByRole('heading', { name: 'Members' })).toBeTruthy();
    expect(screen.getByText('No organization is available for this session.')).toBeTruthy();
    expect(container.querySelector('.surface-card')).toBeNull();
  });
});

function OrganizationMembersHarness({
  children,
  organizationName = 'default',
  permissions = ['members:view', 'members:manage'],
  queryClient: suppliedQueryClient,
}: {
  children: ReactNode;
  organizationName?: string;
  permissions?: string[];
  queryClient?: QueryClient;
}) {
  const queryClient = useMemo(() => {
    const client = suppliedQueryClient ?? new QueryClient({ defaultOptions: { queries: { retry: false } } });
    queryClients.push(client);
    return client;
  }, [suppliedQueryClient]);
  const authValue = useMemo<AuthContextValue>(
    () => ({
      account: {
        uuid: 'acct_test',
        email_address: 'test@example.com',
        display_name: 'test',
        permissions,
        effectiveOrganizationUuid: 'org_test',
        memberships: [{ organization: { uuid: 'org_test', name: organizationName }, role: 'admin' }],
      },
      status: 'authenticated',
      csrfToken: 'csrf_test',
      refresh: async () => ({ account: { uuid: 'acct_test', email_address: 'test@example.com' } }),
      logout: async () => undefined,
    }),
    [organizationName, permissions],
  );
  const workspaceValue = useMemo<WorkspaceContextValue>(
    () => ({
      orgUuid: 'org_test',
      workspaces: [defaultWorkspace],
      activeWorkspace: defaultWorkspace,
      activeWorkspaceId: defaultWorkspace.id,
      isLoading: false,
      error: null,
      selectWorkspace: () => undefined,
      createWorkspace: async () => defaultWorkspace,
      refreshWorkspaces: async () => undefined,
    }),
    [],
  );

  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue}>
        <WorkspaceContext.Provider value={workspaceValue}>{children}</WorkspaceContext.Provider>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

function mockMembersApi(
  options: { failMembersOnce?: boolean; failInvitesOnce?: boolean; forbiddenResource?: 'members' | 'invites' } = {},
) {
  let members: OrganizationMember[] = [
    {
      id: 'acct_test',
      type: 'user',
      name: 'Current User',
      email: 'test@example.com',
      role: 'admin',
      added_at: '2026-01-01T00:00:00Z',
    },
    {
      id: 'user_ada',
      type: 'user',
      name: 'Ada Lovelace',
      email: 'ada@example.com',
      role: 'user',
      added_at: '2026-01-02T00:00:00Z',
    },
  ];
  const roleUpdates: Array<{ body: Record<string, unknown>; csrfToken: string | null }> = [];
  const inviteCreates: Array<{ body: Record<string, unknown>; csrfToken: string | null }> = [];
  const inviteResends: Array<{ inviteId: string; csrfToken: string | null }> = [];
  const inviteDeletes: Array<{ inviteId: string; csrfToken: string | null }> = [];
  let memberListRequests = 0;
  let inviteListRequests = 0;
  let remainingMemberFailures = options.failMembersOnce ? 1 : 0;
  let remainingInviteFailures = options.failInvitesOnce ? 1 : 0;
  const invites: OrganizationInvite[] = [
    {
      id: 'invite_pending',
      type: 'invite',
      email: 'pending@example.com',
      role: 'user',
      status: 'pending',
      invited_at: '2026-06-24T00:00:00Z',
      expires_at: '2026-07-15T00:00:00Z',
    },
  ];

  const fetchMock = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const requestUrl = new URL(url, 'https://oma.duck.ai');
    const method = init?.method ?? 'GET';

    if (requestUrl.pathname === '/api/console/organizations/org_test/members' && method === 'GET') {
      memberListRequests += 1;
      if (options.forbiddenResource === 'members') {
        return jsonResponse({ error: { type: 'permission_error', message: 'Action not allowed' } }, 403);
      }
      if (remainingMemberFailures > 0) {
        remainingMemberFailures -= 1;
        return jsonResponse({ error: 'members unavailable' }, 500);
      }
      return jsonResponse(members);
    }

    if (requestUrl.pathname === '/api/console/organizations/org_test/members/user_ada' && method === 'POST') {
      const body = parseBody(init?.body);
      roleUpdates.push({ body, csrfToken: headerValue(init?.headers, 'X-CSRF-Token') });
      members = members.map((member) => (member.id === 'user_ada' ? { ...member, role: String(body.role) } : member));
      return jsonResponse(members.find((member) => member.id === 'user_ada'));
    }

    if (requestUrl.pathname === '/api/console/organizations/org_test/invites' && method === 'GET') {
      inviteListRequests += 1;
      if (options.forbiddenResource === 'invites') {
        return jsonResponse({ error: { type: 'permission_error', message: 'Action not allowed' } }, 403);
      }
      if (remainingInviteFailures > 0) {
        remainingInviteFailures -= 1;
        return jsonResponse({ error: 'invites unavailable' }, 500);
      }
      const status = requestUrl.searchParams.get('status');
      return jsonResponse(status ? invites.filter((invite) => invite.status === status) : invites);
    }

    if (requestUrl.pathname === '/api/console/organizations/org_test/invites' && method === 'POST') {
      const body = parseBody(init?.body);
      inviteCreates.push({ body, csrfToken: headerValue(init?.headers, 'X-CSRF-Token') });
      const invite: OrganizationInvite = {
        id: `invite_${inviteCreates.length}`,
        type: 'invite',
        email: String(body.email).toLowerCase(),
        role: String(body.role),
        status: 'pending',
        invited_at: '2026-06-25T00:00:00Z',
        expires_at: '2026-07-16T00:00:00Z',
      };
      invites.push(invite);
      return jsonResponse(invite);
    }

    const inviteActionPrefix = '/api/console/organizations/org_test/invites/';
    if (requestUrl.pathname.startsWith(inviteActionPrefix)) {
      const inviteId = decodeURIComponent(requestUrl.pathname.slice(inviteActionPrefix.length));
      const inviteIndex = invites.findIndex((invite) => invite.id === inviteId);

      if (method === 'PUT') {
        inviteResends.push({ inviteId, csrfToken: headerValue(init?.headers, 'X-CSRF-Token') });
        if (inviteIndex < 0) {
          return jsonResponse({ error: 'invite not found' }, 404);
        }
        invites[inviteIndex] = {
          ...invites[inviteIndex],
          status: 'pending',
          invited_at: '2026-06-26T00:00:00Z',
          expires_at: '2026-07-17T00:00:00Z',
        };
        return jsonResponse(invites[inviteIndex]);
      }

      if (method === 'DELETE') {
        inviteDeletes.push({ inviteId, csrfToken: headerValue(init?.headers, 'X-CSRF-Token') });
        if (inviteIndex < 0) {
          return jsonResponse({ error: 'invite not found' }, 404);
        }
        invites[inviteIndex] = { ...invites[inviteIndex], status: 'deleted' };
        return jsonResponse({ id: inviteId, type: 'invite_deleted' });
      }
    }

    return jsonResponse({ error: 'not found' }, 404);
  };

  globalThis.fetch = fetchMock as unknown as typeof fetch;
  return {
    inviteCreates,
    inviteDeletes,
    inviteResends,
    roleUpdates,
    get memberListRequests() {
      return memberListRequests;
    },
    get inviteListRequests() {
      return inviteListRequests;
    },
  };
}

function parseBody(body: BodyInit | null | undefined) {
  if (typeof body !== 'string' || body === '') {
    return {} as Record<string, unknown>;
  }
  return JSON.parse(body) as Record<string, unknown>;
}

function headerValue(headers: HeadersInit | undefined, name: string) {
  if (!headers) {
    return null;
  }
  return new Headers(headers).get(name);
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': 'application/json',
    },
  });
}
