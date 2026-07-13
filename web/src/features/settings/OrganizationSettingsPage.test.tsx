import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, mock, test } from 'bun:test';
import { useMemo, type ReactNode } from 'react';
import { SettingsShell } from '../../app/layout/ConsoleLayout';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { type Organization, type OrganizationProfile } from '../../shared/organization/api';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { OrganizationSettingsContent } from './OrganizationSettingsPage';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor } = testingLibrary;

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

describe('Organization settings', () => {
  test('renders the official organization shell with persisted organization data', async () => {
    resetTestDom('https://oma.duck.ai/settings/organization');
    mockOrganizationSettingsApi({
      profile: {
        physical_address: {
          line1: '1 Main St',
          line2: null,
          country: 'US',
          state: 'CA',
          city: 'San Francisco',
          postal_code: '94105',
        },
        website: null,
        industry: null,
        tax_id: null,
        bill_to: null,
      },
    });

    render(
      <OrganizationSettingsHarness>
        <SettingsShell
          currentPath="/settings/organization"
          account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
          onLogout={() => undefined}
        >
          <OrganizationSettingsContent />
        </SettingsShell>
      </OrganizationSettingsHarness>,
    );

    expect(
      screen
        .getAllByRole('button', { name: /Default/i })
        .some((button) => button.getAttribute('aria-label') === 'Default'),
    ).toBe(true);
    expect(screen.getByText('Back to app')).toBeTruthy();
    expect(screen.getByText('Organization settings')).toBeTruthy();
    expect(screen.getByText('Workload identity')).toBeTruthy();

    const organizationName = (await screen.findByLabelText('Organization name')) as HTMLInputElement;
    await waitFor(() => expect(organizationName.value).toBe('default'));
    await waitFor(() =>
      expect((screen.getByLabelText('Primary business address line 1') as HTMLInputElement).value).toBe('1 Main St'),
    );
    expect(screen.getByTestId('organization-settings-page').querySelector('.surface-card')).toBeNull();
    expect(screen.getByRole('heading', { name: 'Organization' })).toBeTruthy();
    expect(screen.getByText(/Organization ID: org_test/)).toBeTruthy();
    expect(screen.getByRole('switch', { name: /Allow creating new API keys/i }).getAttribute('aria-checked')).toBe(
      'true',
    );
    expect(screen.queryByRole('button', { name: /Save changes/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /Cancel/i })).toBeNull();
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  test('renders the country field as a shared combobox with the expected closed placeholder state', async () => {
    resetTestDom('https://oma.duck.ai/settings/organization');
    mockOrganizationSettingsApi();

    render(
      <OrganizationSettingsHarness>
        <OrganizationSettingsContent />
      </OrganizationSettingsHarness>,
    );

    await screen.findByLabelText('Organization name');
    const countrySelect = screen.getByRole('combobox', { name: 'Country' });
    expect(countrySelect.getAttribute('aria-expanded')).toBe('false');
    expect(countrySelect.textContent).toContain('Select');
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  test('validates and saves organization name plus address through official APIs', async () => {
    resetTestDom('https://oma.duck.ai/settings/organization');
    const api = mockOrganizationSettingsApi({
      profile: {
        physical_address: {
          line1: '',
          line2: null,
          country: 'US',
          state: '',
          city: '',
          postal_code: '',
        },
        website: null,
        industry: null,
        tax_id: null,
        bill_to: null,
      },
    });

    render(
      <OrganizationSettingsHarness>
        <OrganizationSettingsContent />
      </OrganizationSettingsHarness>,
    );

    const organizationName = (await screen.findByLabelText('Organization name')) as HTMLInputElement;
    fireEvent.change(organizationName, { target: { value: ' Open Managed Agents Labs ' } });
    fireEvent.change(screen.getByLabelText('Primary business address line 1'), {
      target: { value: '1 Main St' },
    });

    expect(screen.getByRole('button', { name: /Save changes/i }).hasAttribute('disabled')).toBe(true);

    fireEvent.change(screen.getByLabelText('State or province'), { target: { value: 'CA' } });
    fireEvent.change(screen.getByLabelText('City'), { target: { value: 'San Francisco' } });
    fireEvent.change(screen.getByLabelText('Postal code'), { target: { value: '94105' } });

    const saveButton = screen.getByRole('button', { name: /Save changes/i });
    expect(saveButton.hasAttribute('disabled')).toBe(false);
    fireEvent.click(saveButton);

    await waitFor(() => expect(api.organizationPuts.length).toBe(1));
    await waitFor(() => expect(api.profilePuts.length).toBe(1));
    expect(api.organizationPuts[0]).toEqual({ name: 'Open Managed Agents Labs' });
    expect(api.profilePuts[0]).toEqual({
      physical_address: {
        line1: '1 Main St',
        line2: null,
        country: 'US',
        state: 'CA',
        city: 'San Francisco',
        postal_code: '94105',
      },
    });
  });

  test('persists the API key creation switch immediately', async () => {
    resetTestDom('https://oma.duck.ai/settings/organization');
    const api = mockOrganizationSettingsApi();

    render(
      <OrganizationSettingsHarness>
        <OrganizationSettingsContent />
      </OrganizationSettingsHarness>,
    );

    const toggle = await screen.findByRole('switch', { name: /Allow creating new API keys/i });
    fireEvent.click(toggle);

    await waitFor(() => expect(api.organizationPuts.length).toBe(1));
    expect(api.organizationPuts[0]).toEqual({
      default_workspace_settings: {
        enable_api_keys: false,
      },
    });
    expect(toggle.getAttribute('aria-checked')).toBe('false');
  });
});

function OrganizationSettingsHarness({ children }: { children: ReactNode }) {
  const queryClient = useMemo(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }), []);
  const authValue = useMemo<AuthContextValue>(
    () => ({
      account: {
        uuid: 'acct_test',
        email_address: 'test@example.com',
        display_name: 'test',
        memberships: [{ organization: { uuid: 'org_test', name: 'default' }, role: 'admin' }],
      },
      status: 'authenticated',
      csrfToken: 'csrf_test',
      refresh: async () => ({ account: { uuid: 'acct_test', email_address: 'test@example.com' } }),
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

function mockOrganizationSettingsApi(overrides: { profile?: OrganizationProfile } = {}) {
  let organization: Organization = {
    uuid: 'org_test',
    name: 'default',
    settings: {
      default_workspace_settings: {
        enable_api_keys: true,
      },
    },
  };
  let profile: OrganizationProfile = overrides.profile ?? {
    physical_address: null,
    website: null,
    industry: null,
    tax_id: null,
    bill_to: null,
  };
  const organizationPuts: Record<string, unknown>[] = [];
  const profilePuts: Record<string, unknown>[] = [];

  const fetchMock = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const method = init?.method ?? 'GET';
    const body = parseBody(init?.body);

    if (url.endsWith('/api/organizations/org_test') && method === 'GET') {
      return jsonResponse(organization);
    }

    if (url.endsWith('/api/organizations/org_test') && method === 'PUT') {
      organizationPuts.push(body);
      if (typeof body.name === 'string') {
        organization = { ...organization, name: body.name.trim() };
      }
      const defaultWorkspaceSettings = body.default_workspace_settings as { enable_api_keys?: boolean } | undefined;
      if (defaultWorkspaceSettings) {
        organization = {
          ...organization,
          settings: {
            ...organization.settings,
            default_workspace_settings: {
              ...organization.settings?.default_workspace_settings,
              ...defaultWorkspaceSettings,
            },
          },
        };
      }
      return jsonResponse(organization);
    }

    if (url.endsWith('/api/organizations/org_test/profile') && method === 'GET') {
      return jsonResponse(profile);
    }

    if (url.endsWith('/api/organizations/org_test/profile') && method === 'PUT') {
      profilePuts.push(body);
      if ('physical_address' in body) {
        profile = {
          ...profile,
          physical_address: body.physical_address as OrganizationProfile['physical_address'],
        };
      }
      return jsonResponse(profile);
    }

    return jsonResponse({ error: 'not found' }, 404);
  });

  globalThis.fetch = fetchMock as unknown as typeof fetch;
  return { organizationPuts, profilePuts };
}

function parseBody(body: BodyInit | null | undefined) {
  if (typeof body !== 'string' || body === '') {
    return {} as Record<string, unknown>;
  }
  return JSON.parse(body) as Record<string, unknown>;
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': 'application/json',
    },
  });
}
