import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
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

  test('loads runtime cost for the selected model and range', async () => {
    const urls: string[] = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      urls.push(String(input));
      return new Response(
        JSON.stringify({
          currency: 'USD',
          total: '0.001484',
          daily: [
            {
              date: '2026-10-14',
              amount: '0.001484',
              groups: [{ id: 'claude-opus-4-6', label: 'claude-opus-4-6', amount: '0.001484' }],
            },
          ],
          groups: [],
          models: ['claude-opus-4-6'],
          service_accounts: [],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );
    }) as typeof fetch;
    try {
      resetTestDom('https://oma.duck.ai/cost');
      renderWithWorkspace(<CostPage />);
      expect(await screen.findByText('$0.001484')).toBeTruthy();
      expect(urls[0]).toContain('/api/organizations/org_test/runtime_cost');
      expect(urls[0]).toContain('group_by=model');

      fireEvent.click(screen.getByRole('combobox', { name: 'Model: All' }));
      selectOption('claude-opus-4-6');
      await waitFor(() => expect(urls.some((url) => url.includes('model=claude-opus-4-6'))).toBe(true));

      fireEvent.click(screen.getByRole('combobox', { name: 'Group by: Model' }));
      selectOption('Workspace');
      await waitFor(() => expect(urls.some((url) => url.includes('group_by=workspace'))).toBe(true));

      fireEvent.click(screen.getByRole('combobox', { name: 'Range: Month to date' }));
      selectOption('Last 7 days');
      await waitFor(() => expect(urls.some((url) => url.includes('starting_on='))).toBe(true));

      fireEvent.click(screen.getByRole('combobox', { name: 'Workspace: All workspaces' }));
      selectOption('Default');
      await waitFor(() => expect(urls.some((url) => url.includes('workspace_id=default'))).toBe(true));
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test('renders all-workspace and scoped cost dashboards', () => {
    resetTestDom('https://oma.duck.ai/cost');

    renderWithWorkspace(<CostPage />);

    expect(screen.getByRole('heading', { name: 'Cost' })).toBeTruthy();
    expect(screen.getByText('All workspaces')).toBeTruthy();
    expect(screen.getByText('Total token cost')).toBeTruthy();
    expect(screen.getByText('Total runtime cost')).toBeTruthy();
    expect(screen.getAllByText('$0.00')).toHaveLength(2);
    expect(screen.getByText('Loading...')).toBeTruthy();
    expect(screen.getByText('Daily token cost')).toBeTruthy();
    expect(screen.getByText('Daily runtime cost')).toBeTruthy();

    cleanup();
    resetTestDom('https://oma.duck.ai/workspaces/default/cost');
    renderWithWorkspace(<CostPage />);
    expect(screen.getByText('Default')).toBeTruthy();
  });

  test('shows an access error instead of a zero runtime cost', async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async () => new Response('{}', { status: 403 })) as typeof fetch;
    try {
      resetTestDom('https://oma.duck.ai/cost');
      renderWithWorkspace(<CostPage />);
      expect(await screen.findByText('Only organization administrators can view cost across workspaces.')).toBeTruthy();
      expect(screen.getAllByText('$0.00')).toHaveLength(2);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test('prompts when the runtime price is not configured', async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async () =>
      new Response(
        JSON.stringify({
          configured: false,
          currency: 'USD',
          total: '0.00',
          daily: [],
          groups: [],
          models: [],
          service_accounts: [],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )) as typeof fetch;
    try {
      resetTestDom('https://oma.duck.ai/cost');
      renderWithWorkspace(<CostPage />);
      expect(await screen.findByText('Runtime price is not configured')).toBeTruthy();
      expect(screen.getByText('Not configured')).toBeTruthy();
    } finally {
      globalThis.fetch = originalFetch;
    }
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

  test('updates the scoped logs workspace filter when the route workspace id changes in place', () => {
    resetTestDom('https://oma.duck.ai/workspaces/wrkspc_alpha/logs');

    const alphaWorkspace: Workspace = {
      id: 'wrkspc_alpha',
      type: 'workspace',
      name: 'Alpha Workspace',
    };
    const betaWorkspace: Workspace = {
      id: 'wrkspc_beta',
      type: 'workspace',
      name: 'Beta Workspace',
    };
    const workspaces = [alphaWorkspace, betaWorkspace];

    const { rerender } = renderWithWorkspace(<LogsPage />, workspaces);

    expect(screen.getByRole('combobox', { name: /Workspace/i }).textContent).toContain('Alpha Workspace');

    window.history.replaceState(null, '', 'https://oma.duck.ai/workspaces/wrkspc_beta/logs');
    rerender(
      <WorkspaceHarness workspaces={workspaces}>
        <LogsPage />
      </WorkspaceHarness>,
    );

    const workspaceSelect = screen.getByRole('combobox', { name: /Workspace/i }) as HTMLButtonElement;
    expect(workspaceSelect.disabled).toBe(true);
    expect(workspaceSelect.textContent).toContain('Beta Workspace');
    expect(workspaceSelect.textContent).not.toContain('Alpha Workspace');
    expect(workspaceSelect.getAttribute('aria-label')).toContain('Beta Workspace');
  });

  test('renders workspace logs with official table headers and pagination', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/logs');

    renderWithWorkspace(<LogsPage />);

    expect(screen.getByRole('heading', { name: 'Logs' })).toBeTruthy();
    expect(screen.getByRole('searchbox', { name: 'Search logs' }).getAttribute('type')).toBe('search');
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
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <WorkspaceHarness workspaces={workspaces}>{children}</WorkspaceHarness>
    </QueryClientProvider>,
  );
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
