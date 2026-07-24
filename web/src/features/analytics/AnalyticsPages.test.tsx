import { afterEach, describe, expect, test } from 'bun:test';
import { useMemo, type ReactNode } from 'react';
import { defaultWorkspace, type Workspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { CachingPage, CostPage, LogsPage, RateLimitsPage, UsagePage } from './AnalyticsPages';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor } = testingLibrary;

afterEach(() => {
  cleanup();
});

function selectOption(name: string) {
  const option = screen.getByRole('option', { name });
  fireEvent.pointerDown(option);
  fireEvent.mouseDown(option);
  fireEvent.pointerUp(option);
  fireEvent.mouseUp(option);
  fireEvent.click(option);
}

describe('Analytics pages', () => {
  test('renders the official usage analytics empty dashboard', () => {
    resetTestDom('https://oma.duck.ai/usage');

    renderWithWorkspace(<UsagePage />);

    expect(screen.getByRole('heading', { name: 'Usage' })).toBeTruthy();
    expect(screen.getByText('Workspace')).toBeTruthy();
    expect(screen.getByText('June 2026')).toBeTruthy();
    expect(screen.getByText('Total tokens in')).toBeTruthy();
    expect(screen.getByText('Total tokens out')).toBeTruthy();
    expect(screen.getByText('Total web searches')).toBeTruthy();
    expect(screen.getByText('Token usage')).toBeTruthy();
    expect(screen.getByText('Rate limits now have a dedicated dashboard.')).toBeTruthy();
    const viewRateLimits = screen.getByRole('link', { name: /View rate limits/i });
    expect(viewRateLimits.getAttribute('href')).toBe('/usage/limits');
    expect(viewRateLimits.dataset.slot).toBe('button');
    expect(screen.queryByText('Claude Console')).toBeNull();
  });

  test('uses shared interactive selects for usage filters', async () => {
    resetTestDom('https://oma.duck.ai/usage');

    renderWithWorkspace(<UsagePage />);

    const workspaceSelect = screen.getByRole('combobox', { name: 'Workspace: All' }) as HTMLButtonElement;
    expect(workspaceSelect.disabled).toBe(false);

    fireEvent.click(workspaceSelect);
    expect(screen.getByRole('listbox')).toBeTruthy();
    selectOption('Default');

    await waitFor(() =>
      expect(screen.getByRole('combobox', { name: 'Workspace: Default' }).textContent).toContain('Default'),
    );

    fireEvent.click(screen.getByRole('combobox', { name: 'View by: Month' }));
    selectOption('Week');

    await waitFor(() => expect(screen.getByRole('combobox', { name: 'View by: Week' }).textContent).toContain('Week'));
  });

  test('renders the prompt caching empty state at /usage/cache', () => {
    resetTestDom('https://oma.duck.ai/usage/cache');

    renderWithWorkspace(<CachingPage />);

    expect(screen.getByRole('heading', { name: 'Caching' })).toBeTruthy();
    expect(screen.getByText("You're not using prompt caching")).toBeTruthy();
    expect(screen.getByText('cache_control')).toBeTruthy();
    const learnMore = screen.getByRole('link', { name: /Learn more/i });
    expect(learnMore).toBeTruthy();
    expect(learnMore.getAttribute('href')).toBe('https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching');
    expect(learnMore.dataset.slot).toBe('button');
  });

  test('renders rate limit usage tables at /usage/limits', () => {
    resetTestDom('https://oma.duck.ai/usage/limits');

    renderWithWorkspace(<RateLimitsPage />);

    expect(screen.getByRole('heading', { name: 'Rate limits' })).toBeTruthy();
    expect(screen.getByText('Requests per Minute')).toBeTruthy();
    expect(screen.getByText('Input Tokens per Minute')).toBeTruthy();
    expect(screen.getByText('Output Tokens per Minute')).toBeTruthy();
    expect(screen.getByText('Model limits')).toBeTruthy();
    expect(screen.getByText('Sonnet 4.6')).toBeTruthy();
  });

  test('uses standard card chrome and a live refresh action on analytics routes', () => {
    resetTestDom('https://oma.duck.ai/usage/limits');

    renderWithWorkspace(<RateLimitsPage />);

    const analyticsPage = screen.getByTestId('analytics-page');
    expect(analyticsPage.querySelector('.surface-card')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(screen.getByText('Updated just now.')).toBeTruthy();

    cleanup();
    resetTestDom('https://oma.duck.ai/usage');

    renderWithWorkspace(<UsagePage />);
    expect(screen.getByTestId('analytics-page').querySelector('.surface-card')).toBeNull();

    cleanup();
    resetTestDom('https://oma.duck.ai/usage/cache');

    renderWithWorkspace(<CachingPage />);
    expect(screen.getByTestId('analytics-page').querySelector('.surface-card')).toBeNull();

    cleanup();
    resetTestDom('https://oma.duck.ai/cost');

    renderWithWorkspace(<CostPage />);
    expect(screen.getByTestId('analytics-page').querySelector('.surface-card')).toBeNull();

    cleanup();
    resetTestDom('https://oma.duck.ai/workspaces/default/logs');

    renderWithWorkspace(<LogsPage />);
    expect(screen.getByTestId('analytics-page').querySelector('.surface-card')).toBeNull();
  });

  test('renders all-workspace and scoped cost dashboards', () => {
    resetTestDom('https://oma.duck.ai/cost');

    renderWithWorkspace(<CostPage />);

    expect(screen.getByRole('heading', { name: 'Cost' })).toBeTruthy();
    expect(screen.getByText('All workspaces')).toBeTruthy();
    expect(screen.getByText('Total token cost')).toBeTruthy();
    expect(screen.getAllByText('USD 0.00')).toHaveLength(3);
    expect(screen.getByText('Daily token cost')).toBeTruthy();

    cleanup();
    resetTestDom('https://oma.duck.ai/workspaces/default/cost');
    renderWithWorkspace(<CostPage />);
    expect(screen.getByText('Default')).toBeTruthy();
  });

  test('lists all workspaces in the logs workspace filter on the non-scoped /logs route', () => {
    resetTestDom('https://oma.duck.ai/logs');

    const extraWorkspace: Workspace = {
      id: 'wrkspc_extra',
      type: 'workspace',
      name: 'Extra Workspace',
    };

    renderWithWorkspace(<LogsPage />, [defaultWorkspace, extraWorkspace]);

    const workspaceSelect = screen.getByRole('combobox', { name: /Workspace/i }) as HTMLButtonElement;
    expect(workspaceSelect.disabled).toBe(false);

    fireEvent.click(workspaceSelect);
    const listbox = screen.getByRole('listbox');
    expect(listbox).toBeTruthy();
    expect(screen.getByRole('option', { name: 'All workspaces' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'Default' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'Extra Workspace' })).toBeTruthy();
  });

  test('disables and scopes the logs workspace filter on a scoped /workspaces/:id/logs route', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/logs');

    renderWithWorkspace(<LogsPage />);

    const workspaceSelect = screen.getByRole('combobox', { name: /Workspace/i }) as HTMLButtonElement;
    expect(workspaceSelect.disabled).toBe(true);
    expect(workspaceSelect.textContent).toContain('Default');
  });

  test('renders workspace logs with official table headers and pagination', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/logs');

    renderWithWorkspace(<LogsPage />);

    expect(screen.getByRole('heading', { name: 'Logs' })).toBeTruthy();
    expect(screen.getByText('June 18, 2026 at 11:34 PM GMT+8')).toBeTruthy();
    for (const heading of ['Time', 'ID', 'Model', 'Input Tokens', 'Output Tokens', 'Type', 'Service Tier', 'Request']) {
      expect(screen.getAllByText(heading).length).toBeGreaterThan(0);
    }
    expect(screen.getByText('No logs found')).toBeTruthy();
    expect(screen.getByText('Lines per page')).toBeTruthy();
    expect(screen.getByText('0-0 of 0')).toBeTruthy();
  });

  test('uses the shared lines-per-page select on logs', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/logs');

    renderWithWorkspace(<LogsPage />);

    const linesPerPageSelect = screen.getByRole('combobox', { name: 'Lines per page' }) as HTMLButtonElement;
    expect(linesPerPageSelect.disabled).toBe(false);

    fireEvent.click(linesPerPageSelect);
    expect(screen.getByRole('option', { name: '25' })).toBeTruthy();
    selectOption('25');

    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Lines per page' }).textContent).toContain('25'));
  });

  test('uses a shared filters menu on logs', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/logs');

    renderWithWorkspace(<LogsPage />);

    const filtersButton = screen.getByRole('button', { name: 'Filters' });
    fireEvent.click(filtersButton);

    expect(screen.getByRole('menuitemcheckbox', { name: 'Messages' })).toBeTruthy();
    expect(screen.getByRole('menuitemcheckbox', { name: 'Standard' })).toBeTruthy();

    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'Messages' }));

    await waitFor(() => expect(screen.getByRole('button', { name: 'Filters, 1 active' }).textContent).toContain('1'));
    expect(screen.getByText('No logs match the current filters')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Clear filters' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));

    await waitFor(() => expect(screen.getByRole('button', { name: 'Filters' }).textContent).not.toContain('1'));
    expect(screen.getByText('No logs found')).toBeTruthy();
  });
});

function renderWithWorkspace(children: ReactNode, workspaces = [defaultWorkspace]) {
  return render(<WorkspaceHarness workspaces={workspaces}>{children}</WorkspaceHarness>);
}

function WorkspaceHarness({
  children,
  workspaces = [defaultWorkspace],
}: {
  children: ReactNode;
  workspaces?: Workspace[];
}) {
  const workspaceValue = useMemo<WorkspaceContextValue>(
    () => ({
      orgUuid: 'org_test',
      workspaces,
      activeWorkspace: workspaces[0] ?? defaultWorkspace,
      activeWorkspaceId: (workspaces[0] ?? defaultWorkspace).id,
      isLoading: false,
      error: null,
      selectWorkspace: () => undefined,
      createWorkspace: async () => defaultWorkspace,
      refreshWorkspaces: async () => undefined,
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [workspaces],
  );

  return <WorkspaceContext.Provider value={workspaceValue}>{children}</WorkspaceContext.Provider>;
}
