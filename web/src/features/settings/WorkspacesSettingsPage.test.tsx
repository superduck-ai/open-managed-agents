import { afterEach, describe, expect, mock, test } from 'bun:test';
import type { ReactNode } from 'react';
import { SettingsShell } from '../../app/layout/ConsoleLayout';
import { I18nProvider } from '../../shared/i18n';
import { defaultWorkspace, type CreateWorkspaceInput, type Workspace } from '../../shared/workspaces/api';
import { buildCreateWorkspaceInput } from '../../shared/workspaces/presentation';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { filterWorkspaces, WorkspacesSettingsPage } from './WorkspacesSettingsPage';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe('Workspaces settings page', () => {
  test('renders the settings-shell workspace table with current badge and action links', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces');

    const fooWorkspace: Workspace = {
      id: 'wrkspc_foo',
      type: 'workspace',
      name: 'foo',
      created_at: '2026-02-03T07:12:00Z',
      api_keys_count: 2,
      display_color: '#8CCDB5',
      color: '#8CCDB5',
      data_residency: {
        workspace_geo: 'us',
        default_inference_geo: 'global',
      },
    };

    const { container } = renderWorkspacesSettings({
      workspaceValue: {
        orgUuid: 'org_test',
        canManageWorkspaces: true,
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

    expect(screen.getByRole('heading', { name: 'Workspaces 2' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Create workspace' })).toBeTruthy();
    const table = screen.getByRole('table', { name: 'Workspaces' });
    expect(within(table).getByRole('columnheader', { name: 'Workspace' })).toBeTruthy();
    expect(within(table).getByRole('columnheader', { name: 'ID' })).toBeTruthy();
    expect(within(table).getByRole('columnheader', { name: 'Created' })).toBeTruthy();
    expect(within(table).getByRole('columnheader', { name: 'API keys' })).toBeTruthy();
    expect(within(table).queryByRole('columnheader', { name: 'Residency' })).toBeNull();
    expect(within(table).getByText('Default')).toBeTruthy();
    expect(screen.getByRole('button', { name: /collaborative spaces/i })).toBeTruthy();
    expect(
      within(table).getByRole('button', { name: 'The default workspace is not editable and cannot be removed' }),
    ).toBeTruthy();
    const overviewTrigger = screen.getByRole('button', { name: /collaborative spaces/i });
    expect(overviewTrigger.closest('[data-slot="tooltip-trigger"]')).toBeTruthy();
    expect(
      within(table)
        .getByRole('button', { name: 'The default workspace is not editable and cannot be removed' })
        .closest('[data-slot="tooltip-trigger"]'),
    ).toBeTruthy();
    expect(within(table).getByText('foo')).toBeTruthy();
    expect(within(table).getByText('Current')).toBeTruthy();
    expect(within(table).getByText('wrkspc_foo')).toBeTruthy();
    expect(within(table).getAllByText(/2026/).length).toBeGreaterThan(0);
    expect(within(table).getByText('2')).toBeTruthy();
    const actionButtons = within(table).getAllByRole('button', { name: 'Workspace actions' });
    expect(actionButtons.length).toBe(2);
    expect(actionButtons[1].getAttribute('aria-haspopup')).toBe('menu');
    expect(container.querySelector('.surface-card')).toBeNull();
  });

  test('filters workspaces by search keyword and archived status', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces');

    const fooWorkspace: Workspace = {
      id: 'wrkspc_foo',
      type: 'workspace',
      name: 'foo',
      created_at: '2026-02-03T07:12:00Z',
      api_keys_count: 2,
    };
    const archivedWorkspace: Workspace = {
      id: 'wrkspc_archived',
      type: 'workspace',
      name: 'legacy',
      created_at: '2025-06-01T00:00:00Z',
      api_keys_count: 0,
      archived_at: '2026-01-01T00:00:00Z',
    };

    renderWorkspacesSettings({
      withSettingsShell: false,
      workspaceValue: {
        orgUuid: 'org_test',
        canManageWorkspaces: true,
        workspaces: [defaultWorkspace, fooWorkspace, archivedWorkspace],
        activeWorkspace: fooWorkspace,
        activeWorkspaceId: fooWorkspace.id,
        isLoading: false,
        error: null,
        selectWorkspace: () => undefined,
        createWorkspace: async () => fooWorkspace,
        refreshWorkspaces: async () => undefined,
      },
    });

    const table = screen.getByRole('table', { name: 'Workspaces' });
    expect(within(table).queryByText('legacy')).toBeNull();

    const searchBox = screen.getByRole('searchbox', { name: 'Search workspaces' });
    fireEvent.change(searchBox, { target: { value: 'foo' } });
    expect(within(table).getByText('foo')).toBeTruthy();
    expect(within(table).queryByText('Default')).toBeNull();

    fireEvent.change(searchBox, { target: { value: '' } });
    expect(
      filterWorkspaces([defaultWorkspace, fooWorkspace, archivedWorkspace], '', 'active').map(
        (workspace) => workspace.name,
      ),
    ).toEqual(['Default', 'foo']);
    expect(
      filterWorkspaces([defaultWorkspace, fooWorkspace, archivedWorkspace], '', 'archived').map(
        (workspace) => workspace.name,
      ),
    ).toEqual(['legacy']);
    expect(
      filterWorkspaces([defaultWorkspace, fooWorkspace, archivedWorkspace], 'wrkspc_archived', 'archived').map(
        (workspace) => workspace.name,
      ),
    ).toEqual(['legacy']);
    expect(filterWorkspaces([defaultWorkspace, fooWorkspace, archivedWorkspace], 'no-hit', 'active')).toEqual([]);
  });

  test('builds the shared workspace create payload with name, color, and US residency', () => {
    expect(buildCreateWorkspaceInput('bar', '#D8D2A6')).toEqual({
      name: 'bar',
      display_color: '#D8D2A6',
      data_residency: {
        workspace_geo: 'us',
      },
    });
  });

  test('renders a retry alert when loading workspaces fails', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces');
    const refreshWorkspaces = mock(async () => undefined);

    renderWorkspacesSettings({
      workspaceValue: {
        orgUuid: 'org_test',
        canManageWorkspaces: true,
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
