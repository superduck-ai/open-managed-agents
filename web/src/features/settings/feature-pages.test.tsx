import { afterEach, describe, expect, test } from 'bun:test';
import { useMemo, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SettingsShell } from '../../app/layout/ConsoleLayout';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import { ThemeProvider } from '../../shared/theme/ThemeProvider';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { BillingSettingsPage } from './BillingSettingsPage';
import { AdminKeysSettingsPage } from './AdminKeysSettingsPage';
import { IdentityAndAccessSettingsPage } from './IdentityAndAccessSettingsPage';
import { WorkloadIdentitySettingsPage } from './WorkloadIdentitySettingsPage';
import { AppearanceSettingsPage, ProfileSettingsPage, settingsSectionFromPath } from './feature-pages';

const testingLibrary = await import('@testing-library/react');
const { cleanup, render, screen } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe('Settings feature pages', () => {
  test('maps settings routes to the correct settings-shell sections', () => {
    expect(settingsSectionFromPath('/settings/profile')).toBe('profile');
    expect(settingsSectionFromPath('/settings/appearance')).toBe('appearance');
    expect(settingsSectionFromPath('/settings/workspaces')).toBe('workspaces');
    expect(settingsSectionFromPath('/settings/api-keys')).toBe('api-keys');
    expect(settingsSectionFromPath('/settings/workload-identity')).toBe('workload-identity');
    expect(settingsSectionFromPath('/settings/identity-and-access')).toBe('identity-and-access');
    expect(settingsSectionFromPath('/settings/privacy-controls')).toBe('privacy-controls');
    expect(settingsSectionFromPath('/settings/billing')).toBe('billing');
    expect(settingsSectionFromPath('/settings/unknown')).toBe('organization');
  });

  test('renders profile settings with account identity instead of the organization page', async () => {
    resetTestDom('https://oma.duck.ai/settings/profile');

    const { container } = renderSettingsFeature('/settings/profile', <ProfileSettingsPage />);

    expect(screen.getByText('Organization settings')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Profile' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Organization' })).toBeNull();
    expect((screen.getByLabelText('Display name') as HTMLInputElement).value).toBe('Ada Lovelace');
    expect((screen.getByLabelText('Email address') as HTMLInputElement).value).toBe('ada@example.com');
    expect((screen.getByLabelText('Organization role') as HTMLInputElement).value).toBe('Admin');
    expect(container.querySelector('.surface-card')).toBeNull();
  });

  test('renders appearance settings with shared language and theme selects', () => {
    resetTestDom('https://oma.duck.ai/settings/appearance');

    const { container } = renderSettingsFeature('/settings/appearance', <AppearanceSettingsPage />, {
      withThemeProvider: true,
    });

    expect(screen.getByRole('heading', { name: 'Appearance' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'Language' }).textContent).toContain('English');
    expect(screen.getByRole('combobox', { name: 'Theme' }).textContent).toContain('System');
    expect(container.querySelector('.surface-card')).toBeNull();
  });

  test('renders billing settings with shared links and configure actions', () => {
    resetTestDom('https://oma.duck.ai/settings/billing');

    const { container } = renderSettingsFeature('/settings/billing', <BillingSettingsPage />);

    expect(screen.getByRole('heading', { name: 'Billing' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'View cost dashboard' }).getAttribute('href')).toBe('/cost');
    expect(screen.getByRole('link', { name: 'View cost dashboard' }).getAttribute('data-slot')).toBe('button');
    expect(screen.getByRole('link', { name: 'Review rate limits' }).getAttribute('href')).toBe('/usage/limits');
    expect(screen.getByRole('link', { name: 'Review rate limits' }).getAttribute('data-slot')).toBe('button');
    expect(screen.getByRole('link', { name: 'Manage' }).getAttribute('href')).toBe('/settings/members');
    expect(screen.getByRole('link', { name: 'Manage' }).getAttribute('data-slot')).toBe('button');
    expect(screen.getAllByRole('button', { name: 'Configure' })).toHaveLength(3);
    expect(screen.getByText('Current default: Admins and billing members')).toBeTruthy();
    expect(screen.getByText('Current default: Billing members only')).toBeTruthy();
    expect(screen.getByText('Current default: Monthly')).toBeTruthy();
    expect(container.querySelector('.surface-card')).toBeNull();
  });

  test('renders admin keys with a real shared settings surface', () => {
    resetTestDom('https://oma.duck.ai/settings/admin-keys');

    const { container } = renderSettingsFeature('/settings/admin-keys', <AdminKeysSettingsPage />);

    expect(screen.getByRole('heading', { name: 'Admin keys' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Organization' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Create admin key' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Active' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Revoked' })).toBeTruthy();
    expect(container.querySelector('.surface-card')).toBeNull();
  });

  test('renders workload identity with a real shared settings surface', () => {
    resetTestDom('https://oma.duck.ai/settings/workload-identity');

    const { container } = renderSettingsFeature('/settings/workload-identity', <WorkloadIdentitySettingsPage />);

    expect(screen.getByRole('heading', { name: 'Workload identity' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'View service accounts' }).getAttribute('href')).toBe(
      '/settings/service-accounts',
    );
    expect(screen.getByRole('link', { name: 'View service accounts' }).getAttribute('data-slot')).toBe('button');
    expect(screen.queryByRole('heading', { name: 'Organization' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Create provider' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Active' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Disabled' })).toBeTruthy();
    expect(container.querySelector('.surface-card')).toBeNull();
  });

  test('renders identity and access with a real shared settings surface', () => {
    resetTestDom('https://oma.duck.ai/settings/identity-and-access');

    const { container } = renderSettingsFeature('/settings/identity-and-access', <IdentityAndAccessSettingsPage />);

    expect(screen.getByRole('heading', { name: 'Identity and access' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Manage' }).getAttribute('href')).toBe('/settings/members');
    expect(screen.getByRole('link', { name: 'Manage' }).getAttribute('data-slot')).toBe('button');
    expect(screen.getAllByRole('button', { name: 'Configure' })).toHaveLength(3);
    expect(screen.queryByRole('heading', { name: 'Organization' })).toBeNull();
    expect(container.querySelector('.surface-card')).toBeNull();
  });
});

function renderSettingsFeature(
  currentPath: string,
  children: ReactNode,
  options: { withThemeProvider?: boolean } = {},
) {
  const tree = (
    <SettingsFeatureHarness>
      <SettingsShell
        currentPath={currentPath}
        account={{
          uuid: 'acct_test',
          email_address: 'ada@example.com',
          display_name: 'Ada Lovelace',
          memberships: [{ organization: { uuid: 'org_test', name: 'default' }, role: 'admin' }],
        }}
        onLogout={() => undefined}
      >
        {children}
      </SettingsShell>
    </SettingsFeatureHarness>
  );

  return render(
    <I18nProvider initialLocale="en">
      {options.withThemeProvider ? <ThemeProvider>{tree}</ThemeProvider> : tree}
    </I18nProvider>,
  );
}

function SettingsFeatureHarness({ children }: { children: ReactNode }) {
  const queryClient = useMemo(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }), []);
  const authValue = useMemo<AuthContextValue>(
    () => ({
      account: {
        uuid: 'acct_test',
        email_address: 'ada@example.com',
        display_name: 'Ada Lovelace',
        memberships: [{ organization: { uuid: 'org_test', name: 'default' }, role: 'admin' }],
      },
      status: 'authenticated',
      csrfToken: 'csrf_test',
      refresh: async () => ({ account: { uuid: 'acct_test', email_address: 'ada@example.com' } }),
      logout: async () => undefined,
    }),
    [],
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
