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
import { AppearanceSettingsPage, ProfileSettingsPage, settingsSectionFromPath } from './feature-pages';

const testingLibrary = await import('@testing-library/react');
const { cleanup, render, screen } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe('Settings feature pages', () => {
  test('keeps only working organization and personal settings routes', () => {
    for (const section of ['profile', 'appearance', 'organization', 'llm', 'members', 'workspaces', 'api-keys']) {
      expect(settingsSectionFromPath(`/settings/${section}`)).toBe(section);
    }
    for (const section of [
      'billing',
      'limits',
      'admin-keys',
      'service-accounts',
      'workload-identity',
      'privacy-controls',
      'identity-and-access',
      'unknown',
    ]) {
      expect(settingsSectionFromPath(`/settings/${section}`)).toBe('retired');
    }
  });

  test('renders profile settings with account identity instead of the organization page', async () => {
    resetTestDom('https://oma.duck.ai/settings/profile');

    const { container } = renderSettingsFeature('/settings/profile', <ProfileSettingsPage />);

    expect(screen.getByText('Personal settings')).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Organization' })).toBeNull();
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

  test('organization navigation includes a separate LLM configuration section', () => {
    resetTestDom('https://oma.duck.ai/settings/organization');
    renderSettingsFeature('/settings/organization', <div>Organization form</div>);
    const nav = screen.getByRole('navigation', { name: 'Settings navigation' });
    expect(Array.from(nav.querySelectorAll('a')).map((link) => link.getAttribute('href'))).toEqual([
      '/settings/organization',
      '/settings/llm',
      '/settings/members',
      '/settings/workspaces',
      '/settings/telemetry',
    ]);
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
