import { afterEach, describe, expect, mock, test } from 'bun:test';
import type { ReactNode } from 'react';
import { SettingsShell } from '../../app/layout/ConsoleLayout';
import { I18nProvider } from '../../shared/i18n';
import { defaultWorkspace, type CreateWorkspaceInput, type Workspace } from '../../shared/workspaces/api';
import { buildCreateWorkspaceInput } from '../../shared/workspaces/presentation';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { WorkspacesSettingsPage } from './WorkspacesSettingsPage';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe('Workspaces settings page', () => {
  test('renders the settings-shell workspace table with current badge and action links', () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces');

    const fooWorkspace: Workspace = {
      id: 'wrkspc_foo',
      type: 'workspace',
      name: 'foo',
      display_color: '#8CCDB5',
      color: '#8CCDB5',
    };

    const { container } = renderWorkspacesSettings({
      workspaceValue: {
        orgUuid: 'org_test',
        workspaces: [defaultWorkspace, fooWorkspace],
        activeWorkspace: fooWorkspace,
        activeWorkspaceId: fooWorkspace.id,
        isLoading: false,
        error: null,
        selectWorkspace: () => undefined,
        createWorkspace: async () => fooWorkspace,
        refreshWorkspaces: async () => undefined,
      },
    });

    expect(screen.getByRole('heading', { name: 'Workspaces' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Create workspace' })).toBeTruthy();
    const table = screen.getByRole('table', { name: 'Workspaces' });
    expect(within(table).getByRole('columnheader', { name: 'Workspace' })).toBeTruthy();
    expect(within(table).getByText('Default')).toBeTruthy();
    expect(within(table).getByText('foo')).toBeTruthy();
    expect(within(table).getByText('Current')).toBeTruthy();
    const apiKeyLinks = within(table).getAllByRole('link', { name: 'API keys' });
    const webhookLinks = within(table).getAllByRole('link', { name: 'Webhooks' });
    expect(apiKeyLinks[0].getAttribute('href')).toBe('/settings/workspaces/default/keys');
    expect(apiKeyLinks[1].getAttribute('href')).toBe('/settings/workspaces/wrkspc_foo/keys');
    expect(webhookLinks[0].getAttribute('href')).toBe('/settings/workspaces/default/webhooks');
    expect(webhookLinks[1].getAttribute('href')).toBe('/settings/workspaces/wrkspc_foo/webhooks');
    expect(apiKeyLinks[0].getAttribute('data-slot')).toBe('button');
    expect(webhookLinks[0].getAttribute('data-slot')).toBe('button');
    expect(container.querySelector('.surface-card')).toBeNull();
  });

  test('builds the shared workspace create payload with name and color', () => {
    expect(buildCreateWorkspaceInput('bar', '#D8D2A6')).toEqual({
      name: 'bar',
      display_color: '#D8D2A6',
    });
  });

  test('renders a retry alert when loading workspaces fails', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces');
    const refreshWorkspaces = mock(async () => undefined);

    renderWorkspacesSettings({
      workspaceValue: {
        orgUuid: 'org_test',
        workspaces: [defaultWorkspace],
        activeWorkspace: defaultWorkspace,
        activeWorkspaceId: defaultWorkspace.id,
        isLoading: false,
        error: new Error('workspace service unavailable'),
        selectWorkspace: () => undefined,
        createWorkspace: async () => defaultWorkspace,
        refreshWorkspaces,
      },
    });

    const alert = screen.getByRole('alert');
    expect(within(alert).getByText('Workspaces could not be loaded.')).toBeTruthy();
    expect(within(alert).getByText('workspace service unavailable')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(refreshWorkspaces).toHaveBeenCalled());
  });
});

function renderWorkspacesSettings({
  workspaceValue,
  withSettingsShell = true,
}: {
  workspaceValue: WorkspaceContextValue;
  withSettingsShell?: boolean;
}) {
  const content = withSettingsShell ? (
    <SettingsShell
      currentPath="/settings/workspaces"
      account={{
        uuid: 'acct_test',
        email_address: 'ada@example.com',
        display_name: 'Ada Lovelace',
        memberships: [{ organization: { uuid: 'org_test', name: 'default' }, role: 'admin' }],
      }}
      onLogout={() => undefined}
    >
      <WorkspacesSettingsPage />
    </SettingsShell>
  ) : (
    <WorkspacesSettingsPage />
  );

  return render(
    <I18nProvider initialLocale="en">
      <SettingsHarness workspaceValue={workspaceValue}>{content}</SettingsHarness>
    </I18nProvider>,
  );
}

function SettingsHarness({ children, workspaceValue }: { children: ReactNode; workspaceValue: WorkspaceContextValue }) {
  return <WorkspaceContext.Provider value={workspaceValue}>{children}</WorkspaceContext.Provider>;
}
