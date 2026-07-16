import { expect, test } from 'bun:test';
import {
  ManagedAgentsPage,
  WorkspaceContext,
  agentResponse,
  cleanup,
  codeBlockContaining,
  createAgentRequestFixture,
  expectPageTextToContain,
  fireEvent,
  mock,
  mockAgentsApi,
  mockManagedResourceApi,
  render,
  renderManagedAgentsPage,
  resetTestDom,
  screen,
  selectManagedComboboxOption,
  serverAgent,
  sessionStatusValuesFromUrl,
  setAgentConfigEditorValue,
  waitFor,
  within,
  workspaceContextValue,
} from './ManagedAgentsPage.test-utils';

function requestUrl(input: RequestInfo | URL) {
  return typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
}

function requestMethod(input: RequestInfo | URL, init?: RequestInit) {
  return init?.method ?? (input instanceof Request ? input.method : 'GET');
}

export function registerManagedAgentsResourceTests() {
  test('renders managed resource rows from the real v1 resource endpoints', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions');
    const api = mockManagedResourceApi();

    const sections = [
      ['sessions', 'Session one'],
      ['deployments', 'Deployment one'],
      ['environments', 'Environment one'],
      ['credential-vaults', 'Vault one'],
      ['memory-stores', 'Memory one'],
    ] as const;

    for (const [section, rowText] of sections) {
      cleanup();
      render(<ManagedAgentsPage section={section} />);
      expect(await screen.findByText(rowText)).toBeTruthy();
      expect(screen.queryByText('This local console mock keeps creation client-side for now.')).toBeNull();
      expect(screen.getByRole('button', { name: 'Previous page' }).className.includes('bg-secondary')).toBe(false);
      expect(screen.getByRole('button', { name: 'Next page' }).className.includes('bg-secondary')).toBe(false);
    }

    expect(api.requests.some((request) => request.url.startsWith('/v1/sessions?') && request.method === 'GET')).toBe(
      true,
    );
    expect(api.requests.some((request) => request.url.startsWith('/v1/deployments?') && request.method === 'GET')).toBe(
      true,
    );
    expect(
      api.requests.some((request) => request.url.startsWith('/v1/environments?') && request.method === 'GET'),
    ).toBe(true);
    expect(api.requests.some((request) => request.url.startsWith('/v1/vaults?') && request.method === 'GET')).toBe(
      true,
    );
    expect(
      api.requests.some((request) => request.url.startsWith('/v1/memory_stores?') && request.method === 'GET'),
    ).toBe(true);
  });

  test('uses client-side navigation for managed resource detail links', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions');
    mockManagedResourceApi();
    const popstateHandler = mock(() => undefined);
    window.addEventListener('popstate', popstateHandler);

    try {
      renderManagedAgentsPage('sessions');

      const sessionLink = await screen.findByRole('link', { name: 'Session one' });
      expect(sessionLink.getAttribute('href')).toBe('/workspaces/default/sessions/sesn_one123456');

      fireEvent.click(sessionLink);

      expect(window.location.pathname).toBe('/workspaces/default/sessions/sesn_one123456');
      expect(popstateHandler).toHaveBeenCalledTimes(1);
    } finally {
      window.removeEventListener('popstate', popstateHandler);
    }
  });

  test('uses shared alerts for managed resource list failures', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments');
    mockManagedResourceApi();
    const baseFetch = globalThis.fetch;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (requestUrl(input).startsWith('/v1/environments?') && requestMethod(input, init) === 'GET') {
        return new Response(JSON.stringify({ error: { message: 'environment list failed' } }), {
          status: 500,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return baseFetch(input, init);
    }) as typeof fetch;

    renderManagedAgentsPage('environments');

    const alert = await screen.findByRole('alert');
    expect(alert.dataset.slot).toBe('alert');
    expect(alert.textContent).toContain('environment list failed');
  });

  test('uses shared vault credential row actions with confirmation dialogs', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/vaults/vlt_one123456');
    const api = mockManagedResourceApi();

    render(<ManagedAgentsPage section="credential-vaults" />);

    expect(await screen.findByRole('heading', { name: 'Vault one' })).toBeTruthy();
    const row = (await screen.findByText('Vault credential one')).closest('tr') as HTMLElement;
    expect(row).toBeTruthy();
    expect(within(row).queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(within(row).queryByRole('button', { name: 'Archive' })).toBeNull();
    expect(within(row).queryByRole('button', { name: 'Delete' })).toBeNull();

    fireEvent.click(within(row).getByRole('button', { name: 'More actions' }));
    const credentialMenu = screen
      .getByRole('menuitem', { name: 'Edit' })
      .closest('[data-slot="dropdown-menu-content"]');
    expect(credentialMenu?.className).toContain('bg-popover');
    expect(credentialMenu?.className.includes('bg-secondary')).toBe(false);
    expect(screen.getByRole('menuitem', { name: 'Edit' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Archive' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Delete' })).toBeTruthy();

    fireEvent.click(screen.getByRole('menuitem', { name: 'Archive' }));
    const archiveDialog = await screen.findByRole('alertdialog', { name: /Archive credential/i });
    expect(archiveDialog.textContent).toContain('Vault credential one will be hidden from active lists.');
    fireEvent.click(within(archiveDialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('alertdialog', { name: /Archive credential/i })).toBeNull());

    fireEvent.click(within(row).getByRole('button', { name: 'More actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }));
    const deleteDialog = await screen.findByRole('alertdialog', { name: /Delete credential/i });
    expect(deleteDialog.textContent).toContain('Vault credential one will be permanently removed from this workspace.');
    fireEvent.click(within(deleteDialog).getByRole('button', { name: 'Delete' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url === '/v1/vaults/vlt_one123456/credentials/vcrd_one123456?beta=true' &&
            request.method === 'DELETE',
        ),
      ).toBe(true),
    );
    await waitFor(() => expect(screen.queryByText('Vault credential one')).toBeNull());
  });

  test('keeps managed resource fields from inheriting the global focus ring', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/memory-stores');
    mockManagedResourceApi();
    render(<ManagedAgentsPage section="memory-stores" />);

    expect(await screen.findByText('Memory one')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Create memory store' }));

    const dialog = screen.getByRole('dialog', { name: 'Create memory store' });
    expect(within(dialog).getByLabelText('Name').className).toContain('managed-resource-field');
    expect(within(dialog).getByLabelText('Name').className).toContain('focus-visible:shadow-none');
    expect(within(dialog).getByLabelText('Description').className).toContain('managed-resource-field');
    expect(within(dialog).getByLabelText('Description').className).toContain('focus-visible:shadow-none');
    expect(within(dialog).getByRole('button', { name: 'Create memory store' })).toBeTruthy();
  });

  test('renders the official-style session detail trace workspace', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    window.localStorage.removeItem('oma.sessionDetail.showArchivedLanes');
    const api = mockManagedResourceApi();
    const clipboardWrites: string[] = [];
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: mock(async (value: string) => {
          clipboardWrites.push(value);
        }),
      },
    });

    renderManagedAgentsPage('sessions');

    const page = await screen.findByTestId('session-detail-page');
    expect(page.firstElementChild?.getAttribute('data-slot')).toBe('breadcrumb');
    const breadcrumb = screen.getByRole('navigation', { name: 'Breadcrumb' });
    expect(breadcrumb.dataset.slot).toBe('breadcrumb');
    const sessionsLink = within(breadcrumb).getByRole('link', { name: 'Sessions' });
    expect(sessionsLink.getAttribute('href')).toBe('/workspaces/default/sessions');
    expect(sessionsLink.querySelector('svg')).toBeNull();
    expect(breadcrumb.querySelector('[data-slot="breadcrumb-page"]')?.textContent).toBe('sesn_one123456');
    expect(screen.getByRole('heading', { name: 'Session one' })).toBeTruthy();
    expect(screen.getAllByText('Running')[0]?.className).toContain('bg-emerald-500/10');
    expect(screen.getByText('Ecommerce Basket Analysis Agent')).toBeTruthy();
    expect(screen.getByText('1 file')).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Transcript' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'All events' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Copy all' })).toBeTruthy();
    expect(screen.getByTestId('events-tab')).toBeTruthy();
    expect(screen.getByTestId('events-tab').className).not.toContain('bg-secondary');
    expect(Array.from(page.children).some((child) => child.getAttribute('data-testid') === 'events-tab')).toBe(true);
    expect(screen.getByTestId('session-trace-shell').className).not.toContain('bg-card');
    expect(screen.getByTestId('session-trace-list-pane').className).toContain('overflow-x-hidden');
    expect(screen.getByTestId('session-trace-list-pane').className).toContain('px-0');
    expect(screen.getByTestId('events-minimap')).toBeTruthy();
    const laneTabStrip = screen.getByTestId('lane-tab-strip');
    expect(laneTabStrip).toBeTruthy();
    expect(laneTabStrip.className).toContain('px-0');
    const laneTabList = within(laneTabStrip).getByRole('tablist', { name: 'Session threads' });
    expect(laneTabList.dataset.slot).toBe('tabs-list');
    expect(screen.getByRole('tab', { name: 'reporter' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'analyst' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'forecaster' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'reporter' }).dataset.slot).toBe('tabs-trigger');
    expect(screen.getByRole('tab', { name: 'reporter' }).getAttribute('title')).toBeNull();
    expect(screen.getByRole('button', { name: '+1 archived' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '+1 archived' }).dataset.slot).toBe('toggle');
    expect(screen.queryByRole('tab', { name: 'archived' })).toBeNull();
    expect(screen.queryByRole('tab', { name: /sthr_orp/ })).toBeNull();
    expect(screen.queryByText('(Bash completed with no output)')).toBeNull();
    expect(screen.queryByText('Starting Claude Code')).toBeNull();
    expect(screen.queryByText('Model request start')).toBeNull();
    expect(document.querySelector('[data-event-id^="evt_agent_tool_subagent-"]')).toBeNull();
    expect(await screen.findByText('Data Analysis Task - orders')).toBeTruthy();
    const minimap = screen.getByTestId('events-minimap');
    const minimapTicks = minimap.querySelectorAll<HTMLElement>('[data-timeline-tick-id]');
    const minimapRows = minimap.querySelectorAll<HTMLElement>('[data-lane-index]');
    expect(minimapRows.length).toBe(4);
    expect(within(minimap).queryByText('Orchestrator')).toBeNull();
    expect(minimap.className).toContain('oma-session-timeline');
    expect(minimap.className).toContain('px-0');
    expect(minimapRows[0].className).toContain('oma-session-timeline-track-active');
    expect(minimapRows[0].className).toContain('h-7');
    expect(minimapRows[0].className).not.toContain('border');
    const minimapTrack = minimap.firstElementChild?.firstElementChild as HTMLDivElement | null;
    expect(minimapTrack).toBeTruthy();
    minimapTrack.getBoundingClientRect = () => ({
      x: 0,
      y: 0,
      left: 0,
      top: 0,
      right: 1000,
      bottom: 80,
      width: 1000,
      height: 80,
      toJSON: () => ({}),
    });
    fireEvent.pointerEnter(minimapRows[1]);
    await waitFor(() => expect(minimapRows[1].className).toContain('oma-session-timeline-track-hover'));
    expect(minimapRows[1].className).toContain('h-7');
    const secondLaneTick = minimapRows[1].querySelector<HTMLElement>('[data-timeline-tick-id]');
    expect(secondLaneTick).toBeTruthy();
    const hoverPct = Number.parseFloat(secondLaneTick!.style.left) + Number.parseFloat(secondLaneTick!.style.width) / 2;
    fireEvent.mouseMove(minimapTrack, { clientX: hoverPct * 10, clientY: 260 });
    expect(minimapRows[1].className).toContain('oma-session-timeline-track-hover');
    await waitFor(() => expect(document.querySelector('[id^="session-timeline-tooltip-"]')).toBeTruthy());
    fireEvent.pointerLeave(minimapRows[1], { relatedTarget: minimap });
    await waitFor(() => expect(minimapRows[1].className).toContain('oma-session-timeline-track-inactive'));
    await waitFor(() => expect(document.querySelector('[id^="session-timeline-tooltip-"]')).toBeNull());
    expect(minimapRows[1].className).toContain('h-7');
    expect(minimapTicks.length).toBeGreaterThan(6);
    expect(minimapTicks[0].style.left.endsWith('%')).toBe(true);
    expect(Number.parseFloat(minimapTicks[0].style.width)).toBeGreaterThanOrEqual(0.4);
    expect(screen.getByText("I'll start by unzipping the dataset, then prepare the unified CSV files.")).toBeTruthy();
    const agentPrepareRow = document.querySelector(
      '[data-event-id^="evt_agent_prepare-"][data-entry-kind="message"]',
    ) as HTMLElement;
    expect(agentPrepareRow.querySelector('[data-transcript-header]')?.className).toContain('px-4');
    expect(agentPrepareRow?.textContent).toContain('18.2k');
    expect(agentPrepareRow?.textContent).toContain('425');
    expect(screen.getAllByText('Bash').length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText('Write')).toBeTruthy();
    expect(document.querySelector('[data-entry-kind="tool_call"]')).toBeTruthy();
    expect(document.querySelector('[data-entry-kind="tool_batch"]')).toBeTruthy();
    const toolBatchRow = document.querySelector('[data-entry-kind="tool_batch"]') as HTMLElement;
    expect(within(toolBatchRow).getByText('Tool')).toBeTruthy();
    expect(within(toolBatchRow).queryByText('Tools')).toBeNull();
    expect(toolBatchRow?.textContent).toContain('Read, Glob');
    expect(toolBatchRow?.textContent).not.toContain('2 tool calls');
    expect(screen.getByText('1 queued message')).toBeTruthy();
    expect(screen.getByText('Queued warmup request')).toBeTruthy();
    const queuedRow = document.querySelector(
      '[data-event-id^="evt_user_queued-"][data-entry-kind="message"]',
    ) as HTMLElement;
    expect(queuedRow).toBeTruthy();
    expect(queuedRow.querySelector('[data-cds="ShimmerText"]')?.textContent).toContain('Queued warmup request');
    expect(within(queuedRow).queryByText('Generating')).toBeNull();
    expect(screen.getAllByTestId('session-meta-strip').length).toBeGreaterThan(0);
    expect(screen.getByText('unzip /mnt/session/uploads/orders.zip -d /workspace/data/')).toBeTruthy();
    const unzipRow = document.querySelector(
      '[data-event-id^="evt_tool_unzip-"][data-display-kind="command"]',
    ) as HTMLElement;
    expect(unzipRow?.textContent).toContain('unzip /mnt/session/uploads/orders.zip -d /workspace/data/');
    const toolBadge = within(unzipRow).getByText('Tool').parentElement;
    expect(toolBadge?.className).toContain('h-5');
    expect(toolBadge?.className).toContain('rounded-md');
    expect(toolBadge?.className).toContain('text-[10px]');
    expect(toolBadge?.className).toContain('bg-accent');
    expect(toolBadge?.hasAttribute('aria-hidden')).toBe(false);
    expect(within(unzipRow).getByText('Bash')).toBeTruthy();
    expect(unzipRow?.textContent).not.toContain('805 / 0');
    expect(screen.getByText('Result')).toBeTruthy();
    expect(screen.queryByText('Archive extracted.')).toBeNull();
    fireEvent.click(screen.getByText('unzip /mnt/session/uploads/orders.zip -d /workspace/data/'));
    const toolDetail = await screen.findByTestId('session-trace-detail');
    expect(toolDetail.getAttribute('data-placement')).toBe('side');
    expect(toolDetail.className).not.toContain('bg-secondary');
    expect(toolDetail.className).not.toContain('min-h-[420px]');
    expect(toolDetail.textContent).toContain('Tool result');
    expect(toolDetail.textContent).toContain('Archive extracted.');
    fireEvent.click(within(toolDetail).getByRole('button', { name: 'Close detail panel' }));
    expect(screen.getByText('/workspace/prepare.py')).toBeTruthy();
    expect(screen.getByText('cd /workspace && python3 prepare.py')).toBeTruthy();
    expect(screen.getAllByText('Subagent').length).toBeGreaterThanOrEqual(1);
    const coordinatorSubagentSentRow = document.querySelector(
      '[data-event-id^="evt_thread_message_sent-"]',
    ) as HTMLElement;
    expect(coordinatorSubagentSentRow?.textContent).toContain('reporter');
    expect(within(coordinatorSubagentSentRow).getByText('Subagent')).toBeTruthy();
    const coordinatorSubagentRow = document.querySelector(
      '[data-event-id^="evt_thread_message_received-"]',
    ) as HTMLElement;
    expect(coordinatorSubagentRow?.textContent).toContain('reporter');
    expect(coordinatorSubagentRow?.textContent).not.toContain('sthr_reporter123456');
    expect(document.querySelector('[data-event-id^="evt_subagent_reporter-"]')).toBeNull();
    expect(document.querySelector('[data-event-id^="evt_subagent_analyst-"]')).toBeNull();
    expect(document.querySelector('[data-event-id^="evt_subagent_forecaster-"]')).toBeNull();
    expect(screen.queryByText('Reporter is summarizing order cohorts.')).toBeNull();
    expect(screen.queryByText('{"command":"unzip /mnt/session/uploads/orders.zip -d /workspace/data/"}')).toBeNull();
    const resultPreview = screen.getByText(/^Verification with/);
    const resultRow = resultPreview.closest('[data-slot="toggle"]');
    expect(resultRow?.className).toContain('h-9');
    expect(resultPreview.className).toContain('truncate');
    const resultBadge = within(resultRow as HTMLElement).getByText('Result').parentElement;
    expect(resultBadge?.className).toContain('h-5');
    expect(resultBadge?.className).toContain('bg-accent');
    expect(resultBadge?.className).toContain('text-muted-foreground');
    expect(resultBadge?.className).not.toContain('bg-foreground');
    expect(resultRow?.textContent).toContain('Verification with');
    expect(resultRow?.textContent).toContain('…');
    expect(resultRow?.textContent).not.toContain('Japanese (ja)');
    fireEvent.click(resultPreview);
    const resultDetail = await screen.findByTestId('session-trace-detail');
    const markdown = within(resultDetail).getByTestId('session-trace-markdown');
    expect(markdown.querySelector('ul')?.textContent).toContain('.bashrc');
    expect(markdown.querySelector('table')?.textContent).toContain('Chinese Simplified');
    expect(markdown.querySelector('strong')?.textContent).toBe('你好，世界');
    expect(markdown.querySelector('code')?.textContent).toBe('Glob("*")');
    fireEvent.click(within(resultDetail).getByRole('button', { name: 'Close detail panel' }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/v1/sessions/sesn_one123456?beta=true')).toBe(true),
    );
    expect(api.requests.some((request) => request.url.startsWith('/v1/sessions/sesn_one123456/resources?'))).toBe(true);
    expect(api.requests.some((request) => request.url.startsWith('/v1/sessions/sesn_one123456/threads?'))).toBe(true);
    expect(api.requests.some((request) => request.url.startsWith('/v1/sessions/sesn_one123456/events?'))).toBe(true);
    expect(
      api.requests.some((request) =>
        request.url.startsWith('/v1/sessions/sesn_one123456/threads/sthr_reporter123456/events?'),
      ),
    ).toBe(true);
    expect(
      api.requests.some((request) =>
        request.url.startsWith('/v1/sessions/sesn_one123456/threads/sthr_archived123456/events?'),
      ),
    ).toBe(false);

    fireEvent.click(screen.getByRole('button', { name: '+1 archived' }));
    await waitFor(() => expect(new URL(window.location.href).searchParams.get('archived_lanes')).toBe('true'));
    expect(await screen.findByRole('tab', { name: 'archived' })).toBeTruthy();
    await waitFor(() =>
      expect(
        api.requests.some((request) =>
          request.url.startsWith('/v1/sessions/sesn_one123456/threads/sthr_archived123456/events?'),
        ),
      ).toBe(true),
    );
    fireEvent.click(screen.getByRole('tab', { name: 'archived' }));
    await waitFor(() => expect(new URL(window.location.href).searchParams.get('lane')).toBe('sthr_archived123456'));
    await waitFor(() => {
      const archivedRow = document.querySelector('[data-event-id^="evt_archived-"]') as HTMLElement | null;
      expect(archivedRow?.textContent).toContain('Archived thread details.');
    });
    expect(screen.getByTestId('session-event-detail-panel').textContent).toContain('Archived thread details.');
    fireEvent.click(screen.getByRole('tab', { name: 'Orchestrator' }));
    await waitFor(() => expect(new URL(window.location.href).searchParams.get('lane')).toBeNull());

    fireEvent.click(screen.getByRole('button', { name: 'Open search filter' }));
    fireEvent.change(screen.getByLabelText('Filter events'), { target: { value: 'unzip' } });
    expect(screen.getByText("I'll start by unzipping the dataset, then prepare the unified CSV files.")).toBeTruthy();
    expect(screen.queryByText('Reporter is summarizing order cohorts.')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Clear filter' }));
    const visibleToolRows = () => Array.from(document.querySelectorAll<HTMLElement>('[data-entry-kind="tool_call"]'));
    expect(visibleToolRows().some((row) => row.textContent?.includes('Weather Service'))).toBe(false);

    fireEvent.click(screen.getByRole('tab', { name: 'reporter' }));
    await waitFor(() => expect(new URL(window.location.href).searchParams.get('lane')).toBe('sthr_reporter123456'));
    const reporterSubagentRow = document.querySelector('[data-event-id^="evt_subagent_reporter-"]') as HTMLElement;
    expect(reporterSubagentRow?.textContent).toContain('reporter');
    expect(screen.getByText('Reporter is summarizing order cohorts.')).toBeTruthy();
    expect(visibleToolRows().some((row) => row.textContent?.includes('Weather Service'))).toBe(true);
    expect(screen.queryByText('Analyst is calculating basket lift.')).toBeNull();

    fireEvent.click(screen.getByRole('tab', { name: 'forecaster' }));
    expect(screen.getByText('Forecaster is projecting reorder risk.')).toBeTruthy();
    expect(screen.queryByText('Reporter is summarizing order cohorts.')).toBeNull();

    fireEvent.click(screen.getByRole('tab', { name: 'reporter' }));
    expect(screen.getAllByText('Reporter is summarizing order cohorts.').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByRole('tab', { name: 'reporter' }).getAttribute('aria-selected')).toBe('true');
    const reporterAgentRow = document.querySelector(
      '[data-event-id^="evt_reporter-"][data-entry-kind="message"]',
    ) as HTMLElement;
    expect(reporterAgentRow?.textContent).toContain('7.2k');
    expect(reporterAgentRow?.textContent).toContain('168');
    expect(screen.queryByText('7223 input → 168 output')).toBeNull();
    expect(document.querySelector('[data-event-id^="evt_reporter_span-"][data-display-kind="metric"]')).toBeNull();

    fireEvent.click(screen.getByRole('tab', { name: 'Debug' }));
    expect(screen.getByText('7223 input → 168 output')).toBeTruthy();
    const modelRow = document.querySelector(
      '[data-event-id^="evt_reporter_span-"][data-display-kind="metric"]',
    ) as HTMLElement;
    const modelBadge = within(modelRow).getByText('span.model…end').parentElement;
    expect(modelBadge?.className).toContain('ring-1');
    expect(modelBadge?.className).toContain('bg-transparent');
    const debugUserRow = document.querySelector(
      '[data-event-id^="evt_reporter-"][data-entry-kind="debug"]',
    ) as HTMLElement;
    const debugBadge = within(debugUserRow).getByText('agent.message').parentElement;
    expect(debugBadge?.className).toContain('rounded-md');
    expect(debugBadge?.className).toContain('text-[10px]');
    const deltasButton = within(debugUserRow).getByRole('button', { name: 'Deltas' });
    expect(deltasButton.dataset.slot).toBe('button');
    expect(deltasButton.getAttribute('title')).toBeNull();
    const deltasTooltipTrigger = deltasButton.parentElement;
    expect(deltasTooltipTrigger?.dataset.slot).toBe('tooltip-trigger');
    fireEvent.mouseEnter(deltasTooltipTrigger as HTMLElement);
    await waitFor(() =>
      expect(document.querySelector('[data-slot="tooltip-content"]')?.textContent).toContain('Open deltas'),
    );
    fireEvent.mouseLeave(deltasTooltipTrigger as HTMLElement);
    await waitFor(() => expect(document.querySelector('[data-slot="tooltip-content"]')).toBeNull());
    fireEvent.click(deltasButton);
    const debugDetail = await screen.findByTestId('session-trace-detail');
    expect(within(debugDetail).getByText('Content')).toBeTruthy();
    expect(within(debugDetail).getByText('Deltas')).toBeTruthy();
    expect(debugDetail.textContent).toContain('No deltas captured.');
    const debugSearchParams = new URL(window.location.href).searchParams;
    expect(debugSearchParams.get('segment')).toBe('debug');
    expect(debugSearchParams.get('event')).toBe('evt_reporter');
    fireEvent.click(screen.getByRole('tab', { name: 'Transcript' }));
    await waitFor(() =>
      expect(screen.getAllByText('Reporter is summarizing order cohorts.').length).toBeGreaterThan(0),
    );
    const selectedReporterRow = document.querySelector(
      '[data-event-id^="evt_reporter-"][data-entry-kind="message"]',
    ) as HTMLElement;
    expect(selectedReporterRow.querySelector('[data-transcript-header]')?.getAttribute('data-slot')).toBe('toggle');
    expect(selectedReporterRow.querySelector('[data-transcript-header]')?.getAttribute('aria-pressed')).toBe('true');
    expect(new URL(window.location.href).searchParams.get('event')).toBe('evt_reporter');
    fireEvent.click(screen.getByRole('tab', { name: 'Debug' }));
    const debugUserRowAfterMapping = document.querySelector(
      '[data-event-id^="evt_reporter-"][data-entry-kind="debug"]',
    ) as HTMLElement;
    const debugBadgeAfterMapping = within(debugUserRowAfterMapping).getByText('agent.message').parentElement;
    expect(debugBadgeAfterMapping?.className).not.toContain('rounded-full');
    expect(within(debugUserRowAfterMapping).getByText('Deltas')).toBeTruthy();
    fireEvent.click(debugUserRowAfterMapping.querySelector('[data-transcript-header]') as HTMLElement);
    const detail = await screen.findByTestId('session-trace-detail');
    expect(detail.getAttribute('data-placement')).toBe('side');
    expect(within(detail).getByTestId('session-trace-code-block').textContent).toContain('sthr_reporter123456');
    fireEvent.click(within(detail).getByRole('button', { name: 'Close detail panel' }));

    fireEvent.click(
      (
        document.querySelector('[data-event-id^="evt_reporter-"][data-entry-kind="debug"]') as HTMLElement
      ).querySelector('[data-transcript-header]') as HTMLElement,
    );
    expect((await screen.findByTestId('session-trace-detail')).getAttribute('data-placement')).toBe('side');
    fireEvent.pointerDown(screen.getByTestId('session-trace-list-pane'));
    await waitFor(() => expect(screen.queryByTestId('session-trace-detail')).toBeNull());

    fireEvent.click(screen.getByRole('tab', { name: 'Transcript' }));
    fireEvent.click(screen.getByRole('tab', { name: 'analyst' }));
    expect(screen.queryByText(/session\.thread_status_running/)).toBeNull();
    expect(screen.queryByText(/sevt_thread_running/)).toBeNull();
    expect(screen.getByText('Thinking...')).toBeTruthy();
    expect(
      document.querySelector('[data-event-id^="evt_analyst_thinking-"][data-display-kind="thinking"]'),
    ).toBeTruthy();
    expect(screen.queryByText('Reviewing basket pair frequencies.')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /Thinking\.\.\./ }));
    const thinkingDetail = await screen.findByTestId('session-trace-detail');
    expect(thinkingDetail.getAttribute('data-placement')).toBe('side');
    expect(within(thinkingDetail).getByText('Reviewing basket pair frequencies.')).toBeTruthy();
    fireEvent.click(screen.getByRole('tab', { name: 'reporter' }));

    fireEvent.click(screen.getByRole('button', { name: 'Copy all' }));
    await waitFor(() =>
      expect(clipboardWrites.some((value) => value.includes('Reporter is summarizing order cohorts.'))).toBe(true),
    );

    fireEvent.click(screen.getByRole('button', { name: 'Actions' }));
    expect(screen.getByRole('menuitem', { name: 'Refresh' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Copy session ID' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Copy current view' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Archive' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Delete' })).toBeTruthy();

    fireEvent.click(screen.getByRole('menuitem', { name: 'Copy session ID' }));
    await waitFor(() => expect(clipboardWrites).toContain('sesn_one123456'));

    fireEvent.click(screen.getByRole('button', { name: 'Actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }));
    const deleteDialog = await screen.findByRole('alertdialog', { name: /Delete session/i });
    expect(deleteDialog.textContent).toContain('Session one will be permanently removed from this workspace.');
    fireEvent.click(within(deleteDialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('alertdialog', { name: /Delete session/i })).toBeNull());
  });

  test('renders transcript idle gaps with the original striped separator', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    window.localStorage.removeItem('oma.sessionDetail.showArchivedLanes');
    const api = mockManagedResourceApi();
    const base = Date.now() - 180_000;
    api.resources.sessionThreads = [
      {
        id: 'sthr_idle_gap_worker123456',
        type: 'session_thread',
        role: 'worker',
        parent_thread_id: 'sthr_orchestrator123456',
        archived_at: null,
        created_at: new Date(base + 1_000).toISOString(),
        updated_at: new Date(base + 88_000).toISOString(),
      },
    ];
    api.resources.sessionEvents = [
      {
        id: 'evt_idle_gap_user_before',
        type: 'user.message',
        created_at: new Date(base).toISOString(),
        content: [{ type: 'text', text: 'Start the idle gap fixture.' }],
      },
      {
        id: 'evt_idle_gap_agent_before',
        type: 'agent.message',
        created_at: new Date(base + 2_000).toISOString(),
        content: [{ type: 'text', text: 'I will pause before continuing.' }],
      },
      {
        id: 'evt_idle_gap_idle',
        type: 'session.status_idle',
        created_at: new Date(base + 4_000).toISOString(),
      },
      {
        id: 'evt_idle_gap_user_after',
        type: 'user.message',
        created_at: new Date(base + 88_000).toISOString(),
        content: [{ type: 'text', text: 'Continue after the idle gap.' }],
      },
    ];

    renderManagedAgentsPage('sessions');

    expect(await screen.findByText('Continue after the idle gap.')).toBeTruthy();
    const idleGapRow = document.querySelector('[data-entry-kind="idle_gap"]') as HTMLElement;
    expect(idleGapRow).toBeTruthy();
    expect(idleGapRow.className).toContain('oma-session-idle-gap');
    expect(idleGapRow.querySelector('.oma-session-idle-gap-stripes')).toBeTruthy();
    expect(idleGapRow.textContent).toContain('Session idle');
    const minimap = screen.getByTestId('events-minimap');
    const minimapRows = minimap.querySelectorAll<HTMLElement>('[data-lane-index]');
    expect(minimapRows.length).toBe(2);
    const idleTimelineTick = minimapRows[0].querySelector<HTMLElement>('[data-timeline-tick-type="status_idle"]');
    expect(idleTimelineTick).toBeTruthy();
    expect(Number.parseFloat(idleTimelineTick!.style.width)).toBeGreaterThan(20);
  });

  test('restores the session detail lane from the URL query', async () => {
    resetTestDom(
      'https://oma.duck.ai/workspaces/default/sessions/sesn_one123456?lane=sthr_reporter123456&event=evt_reporter',
    );
    window.localStorage.removeItem('oma.sessionDetail.showArchivedLanes');
    mockManagedResourceApi();

    renderManagedAgentsPage('sessions');

    expect(await screen.findByTestId('session-detail-page')).toBeTruthy();
    await waitFor(() =>
      expect(screen.getByRole('tab', { name: 'reporter' }).getAttribute('aria-selected')).toBe('true'),
    );
    expect(screen.getAllByText('Reporter is summarizing order cohorts.').length).toBeGreaterThan(0);
    expect(new URL(window.location.href).searchParams.get('lane')).toBe('sthr_reporter123456');
    expect(new URL(window.location.href).searchParams.get('event')).toBe('evt_reporter');
  });

  test('uses shared alerts for missing sessions and failed session mutations', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_missing123456');
    mockManagedResourceApi();
    renderManagedAgentsPage('sessions');

    const missingAlert = await screen.findByRole('alert');
    expect(missingAlert.dataset.slot).toBe('alert');
    expect(missingAlert.textContent).toContain('not found');

    cleanup();
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    mockManagedResourceApi();
    const baseFetch = globalThis.fetch;
    globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (requestUrl(input) === '/v1/sessions/sesn_one123456?beta=true' && requestMethod(input, init) === 'DELETE') {
        return new Response(JSON.stringify({ error: { message: 'forced delete failure' } }), {
          status: 500,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return baseFetch(input, init);
    }) as typeof fetch;

    renderManagedAgentsPage('sessions');

    expect(await screen.findByRole('heading', { name: 'Session one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }));
    fireEvent.click(
      within(await screen.findByRole('alertdialog', { name: /Delete session/i })).getByRole('button', {
        name: 'Delete',
      }),
    );

    const mutationAlert = await screen.findByRole('alert');
    expect(mutationAlert.dataset.slot).toBe('alert');
    expect(mutationAlert.textContent).toContain('forced delete failure');
  });

  test('folds tool confirmations into transcript tool rows while keeping debug audit events', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    const api = mockManagedResourceApi();
    api.resources.sessions[0].status = 'idle';
    api.resources.sessionThreads = [];
    api.resources.sessionThreadEvents = {};
    const base = Date.now() - 60_000;
    api.resources.sessionEvents = [
      {
        id: 'evt_user_tool_permissions',
        type: 'user.message',
        created_at: new Date(base).toISOString(),
        content: [{ type: 'text', text: 'Check tool permissions' }],
      },
      {
        id: 'evt_tool_wait',
        type: 'agent.tool_use',
        created_at: new Date(base + 1_000).toISOString(),
        name: 'Bash',
        evaluated_permission: 'ask',
        input: { command: 'npm test -- --watch=false' },
      },
      {
        id: 'evt_tool_policy_object_wait',
        type: 'agent.tool_use',
        created_at: new Date(base + 1_500).toISOString(),
        name: 'Bash',
        status: 'running',
        permission_policy: { type: 'always_ask' },
        input: { command: 'echo permission policy' },
      },
      {
        id: 'evt_tool_allow',
        type: 'agent.tool_use',
        created_at: new Date(base + 2_000).toISOString(),
        name: 'Bash',
        evaluated_permission: 'always_ask',
        input: { command: 'npm run build' },
      },
      {
        id: 'evt_tool_allow_confirmation',
        type: 'user.tool_confirmation',
        created_at: new Date(base + 2_500).toISOString(),
        tool_use_id: 'evt_tool_allow',
        result: 'allow',
      },
      {
        id: 'evt_tool_denied',
        type: 'agent.mcp_tool_use',
        created_at: new Date(base + 3_000).toISOString(),
        name: 'mcp__github__search_repositories',
        evaluated_permission: 'ask',
        input: { query: 'private repository scan' },
      },
      {
        id: 'evt_tool_requires_action_details_wait',
        type: 'agent.mcp_tool_use',
        created_at: new Date(base + 3_250).toISOString(),
        name: 'mcp__weather_service__get_weather',
        requires_action_details: { type: 'requires_action' },
        input: { location: 'Beijing' },
      },
      {
        id: 'evt_tool_deny_confirmation',
        type: 'user.tool_confirmation',
        created_at: new Date(base + 3_500).toISOString(),
        tool_use_id: 'evt_tool_denied',
        result: 'deny',
        deny_message: 'Needs owner approval',
      },
      {
        id: 'evt_tool_batch_read',
        type: 'agent.tool_use',
        created_at: new Date(base + 4_000).toISOString(),
        name: 'Read',
        bracket_id: 'bracket_mixed_tools',
        input: { file_path: '/workspace/a.md' },
      },
      {
        id: 'evt_agent_batch_message',
        type: 'agent.message',
        created_at: new Date(base + 4_200).toISOString(),
        bracket_id: 'bracket_mixed_tools',
        content: [{ type: 'text', text: 'I will inspect both files before editing.' }],
      },
      {
        id: 'evt_tool_batch_glob',
        type: 'agent.tool_use',
        created_at: new Date(base + 4_400).toISOString(),
        name: 'Glob',
        bracket_id: 'bracket_mixed_tools',
        input: { pattern: '*.md' },
      },
      {
        id: 'evt_session_idle_permissions',
        type: 'session.status_idle',
        created_at: new Date(base + 5_000).toISOString(),
      },
    ];

    renderManagedAgentsPage('sessions');

    expect(await screen.findByTestId('session-detail-page')).toBeTruthy();
    expect(await screen.findByText('npm test -- --watch=false')).toBeTruthy();
    const singleLaneMinimap = screen.getByTestId('events-minimap');
    expect(singleLaneMinimap.querySelectorAll('[data-lane-index]').length).toBe(0);
    const singleLaneTrack = singleLaneMinimap.querySelector<HTMLElement>('.oma-session-timeline-track-active');
    expect(singleLaneTrack).toBeTruthy();
    expect(singleLaneTrack?.className).toContain('h-9');
    expect(singleLaneTrack?.className).toContain('rounded');
    expect(singleLaneTrack?.querySelectorAll('[data-timeline-tick-id]').length).toBeGreaterThan(2);
    const waitingRow = document.querySelector(
      '[data-event-id^="evt_tool_wait-"][data-entry-kind="tool_call"]',
    ) as HTMLElement;
    const policyObjectWaitingRow = document.querySelector(
      '[data-event-id^="evt_tool_policy_object_wait-"][data-entry-kind="tool_call"]',
    ) as HTMLElement;
    const allowedRow = document.querySelector(
      '[data-event-id^="evt_tool_allow-"][data-entry-kind="tool_call"]',
    ) as HTMLElement;
    const deniedRow = document.querySelector(
      '[data-event-id^="evt_tool_denied-"][data-entry-kind="tool_call"]',
    ) as HTMLElement;
    const requiresActionWaitingRow = document.querySelector(
      '[data-event-id^="evt_tool_requires_action_details_wait-"][data-entry-kind="tool_call"]',
    ) as HTMLElement;
    const batchRow = document.querySelector('[data-entry-kind="tool_batch"]') as HTMLElement;

    expect(waitingRow.textContent).toContain('awaiting approval');
    expect(policyObjectWaitingRow.textContent).toContain('echo permission policy');
    expect(policyObjectWaitingRow.textContent).toContain('awaiting approval');
    expect(allowedRow.textContent).toContain('npm run build');
    expect(allowedRow.textContent).not.toContain('awaiting approval');
    expect(deniedRow.textContent).toContain('denied');
    expect(deniedRow.textContent).toContain(' Github  Search Repositories');
    expect(requiresActionWaitingRow.textContent).toContain('Beijing');
    expect(requiresActionWaitingRow.textContent).toContain('awaiting approval');
    expect(screen.queryByText('Needs owner approval')).toBeNull();
    expect(batchRow.textContent).toContain('Read, Glob');
    expect(screen.getByText('I will inspect both files before editing.')).toBeTruthy();
    expect(document.querySelector('[data-event-id^="evt_tool_batch_read-"][data-entry-kind="tool_call"]')).toBeNull();
    expect(document.querySelector('[data-event-id^="evt_tool_batch_glob-"][data-entry-kind="tool_call"]')).toBeNull();

    fireEvent.click(deniedRow.querySelector('[data-transcript-header]') as HTMLElement);
    const deniedDetail = await screen.findByTestId('session-trace-detail');
    expect(within(deniedDetail).getByText('Tool confirmation')).toBeTruthy();
    expect(deniedDetail.textContent).toContain('Needs owner approval');

    fireEvent.click(screen.getByRole('tab', { name: 'Debug' }));
    const confirmationDebugRow = document.querySelector(
      '[data-event-id^="evt_tool_deny_confirmation-"][data-entry-kind="debug"]',
    ) as HTMLElement;
    expect(confirmationDebugRow).toBeTruthy();
    expect(confirmationDebugRow.textContent).toContain('Tool confirmation submitted.');
  });

  test('keeps subagent tool confirmations on the originating lane', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    const api = mockManagedResourceApi();
    api.resources.sessions[0].status = 'idle';
    const base = Date.now() - 60_000;
    api.resources.sessionThreads = [
      {
        id: 'sthr_reporter123456',
        type: 'session_thread',
        role: 'reporter',
        parent_thread_id: 'sthr_orchestrator123456',
        archived_at: null,
        created_at: new Date(Date.now() - 45_000).toISOString(),
        updated_at: new Date().toISOString(),
      },
    ];
    api.resources.sessionThreadEvents = {
      sthr_reporter123456: [
        {
          id: 'evt_reporter_shared_permission_child',
          type: 'agent.mcp_tool_use',
          tool_use_id: 'toolu_shared_permission',
          created_at: new Date(base + 2_000).toISOString(),
          name: 'mcp__weather_service__get_forecast',
          evaluated_permission: 'ask',
          input: { location: 'Paris' },
        },
      ],
    };
    api.resources.sessionEvents = [
      {
        id: 'evt_subagent_approval_user',
        type: 'user.message',
        created_at: new Date(base).toISOString(),
        content: [{ type: 'text', text: 'Check lane-scoped approvals' }],
      },
      {
        id: 'evt_main_shared_permission',
        type: 'agent.mcp_tool_use',
        tool_use_id: 'toolu_shared_permission',
        created_at: new Date(base + 1_000).toISOString(),
        name: 'mcp__filesystem__write_file',
        evaluated_permission: 'ask',
        input: { path: '/workspace/main.txt' },
      },
      {
        id: 'evt_reporter_shared_permission',
        type: 'agent.mcp_tool_use',
        session_thread_id: 'sthr_reporter123456',
        tool_use_id: 'toolu_shared_permission',
        created_at: new Date(base + 2_000).toISOString(),
        name: 'mcp__weather_service__get_forecast',
        evaluated_permission: 'ask',
        input: { location: 'Paris' },
      },
      {
        id: 'evt_reporter_shared_confirmation',
        type: 'user.tool_confirmation',
        session_thread_id: 'sthr_reporter123456',
        tool_use_id: 'toolu_shared_permission',
        created_at: new Date(base + 2_500).toISOString(),
        result: 'deny',
        deny_message: 'Reporter cannot call external weather',
      },
      {
        id: 'evt_subagent_approval_idle',
        type: 'session.status_idle',
        created_at: new Date(base + 3_000).toISOString(),
      },
    ];

    renderManagedAgentsPage('sessions');

    expect(await screen.findByTestId('session-detail-page')).toBeTruthy();
    let mainRow: HTMLElement | null = null;
    await waitFor(() => {
      mainRow = document.querySelector<HTMLElement>(
        '[data-event-id^="evt_main_shared_permission-"][data-entry-kind="tool_call"]',
      );
      expect(mainRow).toBeTruthy();
    });
    expect(mainRow?.textContent).toContain('/workspace/main.txt');
    expect(mainRow?.textContent).toContain('awaiting approval');
    expect(screen.queryByText('Paris')).toBeNull();
    expect(screen.queryByText('Reporter cannot call external weather')).toBeNull();

    fireEvent.click(screen.getByRole('tab', { name: 'reporter' }));
    await waitFor(() => expect(new URL(window.location.href).searchParams.get('lane')).toBe('sthr_reporter123456'));
    let reporterRow: HTMLElement | null = null;
    await waitFor(() => {
      reporterRow =
        Array.from(document.querySelectorAll<HTMLElement>('[data-entry-kind="tool_call"]')).find((row) =>
          row.textContent?.includes('Paris'),
        ) ?? null;
      expect(reporterRow).toBeTruthy();
    });
    expect(reporterRow?.textContent).toContain('Paris');
    expect(reporterRow?.textContent).toContain('denied');
    expect(
      Array.from(document.querySelectorAll<HTMLElement>('[data-entry-kind="tool_call"]')).filter((row) =>
        row.textContent?.includes('Paris'),
      ).length,
    ).toBe(1);
    expect(screen.queryByText('/workspace/main.txt')).toBeNull();
    expect(screen.queryByText('Reporter cannot call external weather')).toBeNull();

    fireEvent.click(reporterRow?.querySelector('[data-transcript-header]') as HTMLElement);
    const reporterDetail = await screen.findByTestId('session-trace-detail');
    expect(within(reporterDetail).getByText('Tool confirmation')).toBeTruthy();
    expect(reporterDetail.textContent).toContain('Reporter cannot call external weather');

    fireEvent.click(screen.getByRole('tab', { name: 'Debug' }));
    const confirmationDebugRow = document.querySelector(
      '[data-event-id^="evt_reporter_shared_confirmation-"][data-entry-kind="debug"]',
    ) as HTMLElement;
    expect(confirmationDebugRow).toBeTruthy();
    expect(confirmationDebugRow.textContent).toContain('Tool confirmation submitted.');
  });

  test('keeps transcript tool batches scoped to each lane', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    const api = mockManagedResourceApi();
    api.resources.sessions[0].status = 'idle';
    api.resources.sessionThreads = [
      {
        id: 'sthr_reporter123456',
        type: 'session_thread',
        role: 'reporter',
        parent_thread_id: 'sthr_orchestrator123456',
        archived_at: null,
        created_at: new Date(Date.now() - 45_000).toISOString(),
        updated_at: new Date().toISOString(),
      },
    ];
    api.resources.sessionThreadEvents = {};
    const base = Date.now() - 60_000;
    api.resources.sessionEvents = [
      {
        id: 'evt_lane_batch_user',
        type: 'user.message',
        created_at: new Date(base).toISOString(),
        content: [{ type: 'text', text: 'Compare lane scoped tool batches' }],
      },
      {
        id: 'evt_lane_batch_main_tool',
        type: 'agent.tool_use',
        bracket_id: 'span_shared_lane_batch',
        created_at: new Date(base + 1_000).toISOString(),
        name: 'Read',
        input: { file_path: '/workspace/main.json' },
      },
      {
        id: 'evt_lane_batch_reporter_tool',
        type: 'agent.tool_use',
        session_thread_id: 'sthr_reporter123456',
        bracket_id: 'span_shared_lane_batch',
        created_at: new Date(base + 1_100).toISOString(),
        name: 'Bash',
        input: { command: 'echo reporter lane' },
      },
      {
        id: 'evt_lane_batch_idle',
        type: 'session.status_idle',
        created_at: new Date(base + 3_000).toISOString(),
      },
    ];

    renderManagedAgentsPage('sessions');

    expect(await screen.findByTestId('session-detail-page')).toBeTruthy();
    await waitFor(() => {
      const mainToolRow = document.querySelector<HTMLElement>(
        '[data-event-id^="evt_lane_batch_main_tool-"][data-entry-kind="tool_call"]',
      );
      expect(mainToolRow?.textContent).toContain('/workspace/main.json');
    });
    expect(document.querySelector('[data-entry-kind="tool_batch"]')).toBeNull();
    expect(screen.queryByText('echo reporter lane')).toBeNull();

    fireEvent.click(screen.getByRole('tab', { name: 'reporter' }));
    await waitFor(() => expect(new URL(window.location.href).searchParams.get('lane')).toBe('sthr_reporter123456'));
    await waitFor(() => {
      const reporterToolRow = document.querySelector<HTMLElement>(
        '[data-event-id^="evt_lane_batch_reporter_tool-"][data-entry-kind="tool_call"]',
      );
      expect(reporterToolRow?.textContent).toContain('echo reporter lane');
    });
    expect(document.querySelector('[data-entry-kind="tool_batch"]')).toBeNull();
    expect(screen.queryByText('/workspace/main.json')).toBeNull();
  });

  test('subscribes to session streams after a running session detail loads', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    const api = mockManagedResourceApi();
    api.resources.sessions[0].status = 'running';
    api.resources.sessions[0].updated_at = new Date().toISOString();
    api.resources.sessionEvents = api.resources.sessionEvents.filter((event) => event.type !== 'session.status_idle');

    renderManagedAgentsPage('sessions');

    expect(await screen.findByTestId('session-detail-page')).toBeTruthy();
    await waitFor(() =>
      expect(api.requests.some((request) => request.url.startsWith('/v1/sessions/sesn_one123456/events?'))).toBe(true),
    );
    await waitFor(() =>
      expect(
        api.requests.some((request) =>
          request.url.startsWith('/v1/sessions/sesn_one123456/threads/sthr_reporter123456/events?'),
        ),
      ).toBe(true),
    );
    await waitFor(() =>
      expect(api.requests.some((request) => request.url.startsWith('/v1/sessions/sesn_one123456/events/stream?'))).toBe(
        true,
      ),
    );
    expect(api.requests.some((request) => request.url.includes('/threads/sthr_reporter123456/stream?'))).toBe(false);
  });

  test('does not subscribe to session streams when running metadata has completed history', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    const api = mockManagedResourceApi();
    api.resources.sessions[0].status = 'running';
    api.resources.sessions[0].updated_at = new Date().toISOString();

    renderManagedAgentsPage('sessions');

    expect(await screen.findByTestId('session-detail-page')).toBeTruthy();
    await waitFor(() =>
      expect(api.requests.some((request) => request.url.startsWith('/v1/sessions/sesn_one123456/events?'))).toBe(true),
    );
    await waitFor(() =>
      expect(
        api.requests.some((request) =>
          request.url.startsWith('/v1/sessions/sesn_one123456/threads/sthr_reporter123456/events?'),
        ),
      ).toBe(true),
    );
    expect(api.requests.some((request) => request.url.includes('/stream?'))).toBe(false);
  });

  test('does not subscribe to session streams after an idle session detail loads', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    const api = mockManagedResourceApi();
    api.resources.sessions[0].status = 'idle';
    api.resources.sessions[0].updated_at = new Date(Date.now() - 40_000).toISOString();

    renderManagedAgentsPage('sessions');

    expect(await screen.findByTestId('session-detail-page')).toBeTruthy();
    await waitFor(() =>
      expect(api.requests.some((request) => request.url.startsWith('/v1/sessions/sesn_one123456/events?'))).toBe(true),
    );
    await waitFor(() =>
      expect(
        api.requests.some((request) =>
          request.url.startsWith('/v1/sessions/sesn_one123456/threads/sthr_reporter123456/events?'),
        ),
      ).toBe(true),
    );
    expect(api.requests.some((request) => request.url.includes('/stream?'))).toBe(false);
  });

  test('does not subscribe to session streams after a terminated session detail loads', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    const api = mockManagedResourceApi();
    api.resources.sessions[0].status = 'terminated';
    api.resources.sessions[0].updated_at = new Date(Date.now() - 40_000).toISOString();

    renderManagedAgentsPage('sessions');

    expect(await screen.findByTestId('session-detail-page')).toBeTruthy();
    await waitFor(() =>
      expect(api.requests.some((request) => request.url.startsWith('/v1/sessions/sesn_one123456/events?'))).toBe(true),
    );
    await waitFor(() =>
      expect(
        api.requests.some((request) =>
          request.url.startsWith('/v1/sessions/sesn_one123456/threads/sthr_reporter123456/events?'),
        ),
      ).toBe(true),
    );
    expect(api.requests.some((request) => request.url.includes('/stream?'))).toBe(false);
  });

  test('renders session detail controls in Chinese', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    mockManagedResourceApi();
    renderManagedAgentsPage('sessions', 'zh-CN');

    expect(await screen.findByTestId('session-detail-page')).toBeTruthy();
    expect(screen.getByRole('tab', { name: '转录' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '全部事件' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '复制全部' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '刷新' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '操作' }));
    expect(screen.getByRole('menuitem', { name: '复制会话 ID' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: '复制当前视图' })).toBeTruthy();
  });

  test('renders the official memory store selection column', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/memory-stores');
    mockManagedResourceApi();
    render(<ManagedAgentsPage section="memory-stores" />);

    expect(await screen.findByText('Memory one')).toBeTruthy();
    const selectAll = screen.getByRole('checkbox', { name: 'Select all rows' });
    const selectRow = screen.getByRole('checkbox', { name: 'Select Memory one' });

    expect(selectAll.getAttribute('aria-checked')).toBe('false');
    expect(selectRow.getAttribute('aria-checked')).toBe('false');
    fireEvent.click(selectRow);
    expect(selectRow.getAttribute('aria-checked')).toBe('true');
    expect(selectAll.getAttribute('aria-checked')).toBe('true');
  });

  test('renders the official-style memory store detail workspace', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/memory-stores/memstore_one123456?memory=mem_one123456');
    const api = mockManagedResourceApi();
    render(<ManagedAgentsPage section="memory-stores" />);

    expect(await screen.findByRole('heading', { name: 'Memory one' })).toBeTruthy();
    const breadcrumb = screen.getByRole('navigation', { name: 'Breadcrumb' });
    expect(breadcrumb.dataset.slot).toBe('breadcrumb');
    expect(within(breadcrumb).getByRole('link', { name: 'Memory stores' }).getAttribute('href')).toBe(
      '/workspaces/default/memory-stores',
    );
    expect(breadcrumb.querySelector('[data-slot="breadcrumb-page"]')?.textContent).toBe('Memory one');
    expect(screen.queryByRole('heading', { name: 'Overview' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Add memory' })).toBeTruthy();
    const workspaceCard = screen.getByText('Memories').closest('[data-slot="card"]') as HTMLElement | null;
    expect(workspaceCard).toBeTruthy();
    const workspaceCardContent = workspaceCard?.querySelector('[data-slot="card-content"]') as HTMLElement | null;
    expect(workspaceCardContent?.className).toContain('lg:grid-cols-[280px_minmax(0,1fr)]');
    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url ===
              '/v1/memory_stores/memstore_one123456/memories?beta=true&path_prefix=%2F&depth=1&limit=100&order_by=path' &&
            request.method === 'GET',
        ),
      ).toBe(true),
    );

    expect(screen.getByRole('heading', { name: '/project/brief.md' })).toBeTruthy();
    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url === '/v1/memory_stores/memstore_one123456/memories/mem_one123456?beta=true&view=full' &&
            request.method === 'GET',
        ),
      ).toBe(true),
    );
    expect(await screen.findByText('Remember the release plan.')).toBeTruthy();
    expect(screen.queryByText(/localeCompare/)).toBeNull();
    const viewModeTabs = screen.getByRole('tablist', { name: 'View mode' });
    expect(viewModeTabs).toBeTruthy();
    expect(viewModeTabs.dataset.slot).toBe('tabs-list');
    expect(viewModeTabs.className.includes('bg-secondary')).toBe(false);
    expect(screen.getByRole('tab', { name: 'Preview' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Source' })).toBeTruthy();

    fireEvent.click(screen.getByRole('tab', { name: 'Source' }));
    expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('aria-selected')).toBe('true');
    const sourceBlock = screen.getByText('Remember the release plan.');
    expect(sourceBlock.tagName).toBe('PRE');
    expect(sourceBlock.className).not.toContain('border');
    expect(sourceBlock.className).not.toContain('bg-secondary');

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    const memoryActionMenu = screen
      .getByRole('menuitem', { name: 'Download' })
      .closest('[data-slot="dropdown-menu-content"]');
    expect(memoryActionMenu?.className).toContain('bg-popover');
    expect(memoryActionMenu?.className.includes('bg-secondary')).toBe(false);
    expect(screen.getByRole('menuitem', { name: 'Download' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Delete' })).toBeTruthy();
    expect(screen.queryByRole('menuitem', { name: 'Copy ID' })).toBeNull();
    fireEvent.keyDown(document, { key: 'Escape' });

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const editor = screen.getByRole('textbox', { name: 'Memory content' }) as HTMLTextAreaElement;
    expect(editor.value).toBe('Remember the release plan.');
    expect(editor.dataset.slot).toBe('textarea');
    expect(editor.className.includes('bg-secondary')).toBe(false);
    fireEvent.change(editor, { target: { value: 'Updated release memory.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url === '/v1/memory_stores/memstore_one123456/memories/mem_one123456?beta=true&view=full' &&
            request.method === 'POST',
        ),
      ).toBe(true),
    );
    const updateRequest = api.requests.find(
      (request) =>
        request.url === '/v1/memory_stores/memstore_one123456/memories/mem_one123456?beta=true&view=full' &&
        request.method === 'POST',
    );
    expect(updateRequest?.body?.path).toBe('/project/brief.md');
    expect(updateRequest?.body?.content).toBe('Updated release memory.');
    expect(await screen.findByText('Updated release memory.')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Add memory' }));
    const dialog = screen.getByRole('dialog', { name: 'Add memory' });
    fireEvent.change(within(dialog).getByLabelText('Path'), { target: { value: '/notes/new.md' } });
    fireEvent.change(within(dialog).getByLabelText('Content'), { target: { value: 'New memory content' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add memory' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url === '/v1/memory_stores/memstore_one123456/memories?beta=true&view=full' &&
            request.method === 'POST',
        ),
      ).toBe(true),
    );
    const createRequest = api.requests.find(
      (request) =>
        request.url === '/v1/memory_stores/memstore_one123456/memories?beta=true&view=full' &&
        request.method === 'POST',
    );
    expect(createRequest?.body?.path).toBe('/notes/new.md');
    expect(createRequest?.body?.content).toBe('New memory content');
  });

  test('uses the shared delete confirmation dialog for memory detail items', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/memory-stores/memstore_one123456?memory=mem_one123456');
    const api = mockManagedResourceApi();
    render(<ManagedAgentsPage section="memory-stores" />);

    expect(await screen.findByRole('heading', { name: 'Memory one' })).toBeTruthy();
    expect(await screen.findByRole('heading', { name: '/project/brief.md' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }));

    const deleteDialog = await screen.findByRole('alertdialog', { name: /Delete memory/i });
    expect(deleteDialog.textContent).toContain('/project/brief.md will be permanently removed from this workspace.');
    fireEvent.click(within(deleteDialog).getByRole('button', { name: 'Delete' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url === '/v1/memory_stores/memstore_one123456/memories/mem_one123456?beta=true' &&
            request.method === 'DELETE',
        ),
      ).toBe(true),
    );
    expect(await screen.findByText('Select a memory')).toBeTruthy();
  });

  test('renders and expands the official memory store directory tree', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/memory-stores/memstore_one123456');
    const api = mockManagedResourceApi();
    render(<ManagedAgentsPage section="memory-stores" />);

    expect(await screen.findByRole('heading', { name: 'Memory one' })).toBeTruthy();
    expect(await screen.findByText('Select a memory')).toBeTruthy();
    expect(screen.getByText('Choose a file from the tree to view its contents.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Expand all' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Expand folder aaa' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Expand folder cccc' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Expand folder aaa' }));
    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url ===
              '/v1/memory_stores/memstore_one123456/memories?beta=true&path_prefix=%2Faaa%2F&depth=1&limit=100&order_by=path' &&
            request.method === 'GET',
        ),
      ).toBe(true),
    );
    expect(await screen.findByRole('button', { name: 'bbbb 4 B' })).toBeTruthy();
    expect(screen.getByText('Choose a file from the tree to view its contents.')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'bbbb 4 B' }));
    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url === '/v1/memory_stores/memstore_one123456/memories/mem_nested123456?beta=true&view=full' &&
            request.method === 'GET',
        ),
      ).toBe(true),
    );
    expect(await screen.findByRole('heading', { name: '/aaa/bbbb' })).toBeTruthy();
    expect(screen.getByText('aaaa')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Expand all' }));
    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url ===
              '/v1/memory_stores/memstore_one123456/memories?beta=true&path_prefix=%2Fcccc%2Fdddd%2Fccc%2F&depth=1&limit=100&order_by=path' &&
            request.method === 'GET',
        ),
      ).toBe(true),
    );
    expect(await screen.findByRole('button', { name: 'Collapse all' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'xxxx 4 B' })).toBeTruthy();

    const rootListUrl =
      '/v1/memory_stores/memstore_one123456/memories?beta=true&path_prefix=%2F&depth=1&limit=100&order_by=path';
    const rootListRequestsBeforeSave = api.requests.filter(
      (request) => request.url === rootListUrl && request.method === 'GET',
    ).length;
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const editor = screen.getByRole('textbox', { name: 'Memory content' });
    fireEvent.change(editor, { target: { value: 'updated nested memory' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url === '/v1/memory_stores/memstore_one123456/memories/mem_nested123456?beta=true&view=full' &&
            request.method === 'POST',
        ),
      ).toBe(true),
    );
    expect(api.requests.filter((request) => request.url === rootListUrl && request.method === 'GET')).toHaveLength(
      rootListRequestsBeforeSave,
    );
    expect(screen.getByRole('button', { name: 'Collapse folder aaa' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Collapse folder cccc' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'xxxx 4 B' })).toBeTruthy();
    expect(await screen.findByText('updated nested memory')).toBeTruthy();
  });

  test('creates a session with selected agent and environment references', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions');
    const api = mockManagedResourceApi();
    render(<ManagedAgentsPage section="sessions" />);

    expect(await screen.findByText('Session one')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Create session' }));

    const dialog = screen.getByRole('dialog', { name: 'Create session' });
    fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Console session' } });
    const resourceTrigger = within(dialog).getByRole('button', { name: 'Resource' });
    expect(resourceTrigger.dataset.slot).toBe('collapsible-trigger');
    const resourceCard = resourceTrigger.closest('[data-slot="card"]');
    expect(resourceCard).toBeTruthy();
    expect(resourceCard?.className.includes('bg-secondary')).toBe(false);
    expect(resourceTrigger.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(resourceTrigger);
    expect(resourceTrigger.getAttribute('aria-expanded')).toBe('true');
    const resourceCopy = within(dialog).getByText(
      'No resource attachments are configured. Add files, repositories, or memory stores after creation.',
    );
    expect(resourceCopy.closest('[data-slot="collapsible-content"]')).toBeTruthy();

    await waitFor(() =>
      expect(within(dialog).getByRole('combobox', { name: 'Agent' }).textContent).toContain('Option agent'),
    );
    await selectManagedComboboxOption(dialog, 'Environment', 'Option environment');
    expect(within(dialog).getByRole('combobox', { name: 'Environment' }).textContent).toContain('Option environment');

    fireEvent.click(within(dialog).getByRole('button', { name: 'Create session' }));

    await waitFor(() =>
      expect(
        api.requests.some((request) => request.url === '/v1/sessions?beta=true' && request.method === 'POST'),
      ).toBe(true),
    );
    const createRequest = api.requests.find(
      (request) => request.url === '/v1/sessions?beta=true' && request.method === 'POST',
    );
    expect(createRequest?.body?.title).toBe('Console session');
    expect(createRequest?.body?.agent).toBe('agent_option123456');
    expect(createRequest?.body?.environment_id).toBe('env_option123456');
    expect(createRequest?.headers['x-workspace-id']).toBe('default');
  });

  test('renders the official-style create deployment dialog and submits deployment payload', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/deployments');
    const api = mockManagedResourceApi();
    render(<ManagedAgentsPage section="deployments" />);

    expect(await screen.findByText('Deployment one')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Create deployment' }));

    const dialog = screen.getByRole('dialog', { name: 'Create deployment' });
    expect(dialog.dataset.slot).toBe('dialog-content');
    expect(dialog.className.includes('bg-secondary')).toBe(false);
    expect(dialog.className.includes('bg-popover')).toBe(true);
    expect(within(dialog).getByText('Deploy an agent with a trigger, environment, and credentials.')).toBeTruthy();
    expect(within(dialog).queryByLabelText('Description')).toBeNull();
    expect(within(dialog).queryByRole('button', { name: 'Cancel' })).toBeNull();
    expect(within(dialog).getByRole('link', { name: 'Manage agents (opens in new tab)' }).getAttribute('href')).toBe(
      '/workspaces/default/agents',
    );
    expect(within(dialog).queryByRole('button', { name: 'Resource' })).toBeNull();

    await waitFor(() =>
      expect(within(dialog).getByRole('combobox', { name: 'Agent' }).textContent).toContain('Select an agent'),
    );

    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Nightly inbox triage' } });
    await selectManagedComboboxOption(dialog, 'Agent', 'Option agent');
    fireEvent.change(within(dialog).getByLabelText('Initial message'), {
      target: { value: 'Summarize support tickets.' },
    });
    await selectManagedComboboxOption(dialog, 'Environment', 'Option environment');
    await selectManagedComboboxOption(dialog, /Credential vaults/, 'Vault one');
    await selectManagedComboboxOption(dialog, /Memory stores/, 'Memory one');
    await selectManagedComboboxOption(dialog, 'Trigger', 'Manual');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }));

    await waitFor(() =>
      expect(
        api.requests.some((request) => request.url === '/v1/deployments?beta=true' && request.method === 'POST'),
      ).toBe(true),
    );
    const createRequest = api.requests.find(
      (request) => request.url === '/v1/deployments?beta=true' && request.method === 'POST',
    );
    expect(createRequest?.body?.name).toBe('Nightly inbox triage');
    expect(createRequest?.body?.agent).toBe('agent_option123456');
    expect(createRequest?.body?.environment_id).toBe('env_option123456');
    expect(createRequest?.body?.vault_ids).toEqual(['vlt_one123456']);
    expect(createRequest?.body?.resources).toEqual([{ type: 'memory_store', memory_store_id: 'memstore_one123456' }]);
    expect(createRequest?.body?.initial_events).toEqual([
      {
        type: 'user.message',
        content: [{ type: 'text', text: 'Summarize support tickets.' }],
      },
    ]);
    expect(createRequest?.body?.schedule).toBeNull();
  });

  test('opens deployment agent and status filters and refetches the list with the selected values', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/deployments');
    const api = mockManagedResourceApi();
    const now = new Date().toISOString();
    api.resources.agents.push({
      ...serverAgent,
      archived_at: null,
      created_at: now,
      description: null,
      mcp_servers: [],
      metadata: {},
      multiagent: null,
      skills: [],
      system: null,
      tools: [],
      updated_at: now,
      version: 1,
    });
    api.resources.deployments.push(
      {
        id: 'dep_paused123456',
        agent: { type: 'agent', id: 'agent_server123456', version: 1 },
        agent_id: 'agent_server123456',
        archived_at: null,
        created_at: now,
        description: 'Paused deployment',
        environment_id: 'env_option123456',
        name: 'Paused deployment',
        paused_reason: { type: 'manual' },
        schedule: null,
        status: 'paused',
        type: 'deployment',
        updated_at: now,
        vault_ids: [],
      },
      {
        id: 'dep_archived123456',
        agent: { type: 'agent', id: 'agent_option123456', version: 1 },
        agent_id: 'agent_option123456',
        archived_at: now,
        created_at: now,
        description: 'Archived deployment',
        environment_id: 'env_option123456',
        name: 'Archived deployment',
        paused_reason: null,
        schedule: null,
        status: 'active',
        type: 'deployment',
        updated_at: now,
        vault_ids: [],
      },
    );

    render(<ManagedAgentsPage section="deployments" />);

    expect(await screen.findByText('Deployment one')).toBeTruthy();
    expect(await screen.findByText('Paused deployment')).toBeTruthy();
    expect(await screen.findByText('Archived deployment')).toBeTruthy();
    expect(
      api.requests.some(
        (request) =>
          request.url === '/v1/deployments?beta=true&limit=5&include_archived=true' && request.method === 'GET',
      ),
    ).toBe(true);

    fireEvent.click(screen.getByRole('button', { name: 'Status All' }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Paused' }));

    await waitFor(() => {
      const latestRequest = api.requests
        .filter((request) => request.url.startsWith('/v1/deployments?') && request.method === 'GET')
        .at(-1);
      expect(latestRequest).toBeTruthy();
      const params = new URL(latestRequest!.url, 'https://oma.duck.ai').searchParams;
      expect(params.get('status')).toBe('paused');
      expect(params.get('include_archived')).toBe('false');
    });
    expect(await screen.findByText('Paused deployment')).toBeTruthy();
    expect(screen.queryByText('Deployment one')).toBeNull();
    expect(screen.queryByText('Archived deployment')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Status Paused' }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'All' }));

    await waitFor(() => {
      const latestRequest = api.requests
        .filter((request) => request.url.startsWith('/v1/deployments?') && request.method === 'GET')
        .at(-1);
      expect(latestRequest).toBeTruthy();
      const params = new URL(latestRequest!.url, 'https://oma.duck.ai').searchParams;
      expect(params.get('status')).toBeNull();
      expect(params.get('include_archived')).toBe('true');
    });

    fireEvent.click(screen.getByRole('button', { name: 'Agent All' }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Server agent' }));

    await waitFor(() => {
      const latestRequest = api.requests
        .filter((request) => request.url.startsWith('/v1/deployments?') && request.method === 'GET')
        .at(-1);
      expect(latestRequest).toBeTruthy();
      const params = new URL(latestRequest!.url, 'https://oma.duck.ai').searchParams;
      expect(params.get('agent_id')).toBe('agent_server123456');
      expect(params.get('include_archived')).toBe('true');
    });
    expect(await screen.findByText('Paused deployment')).toBeTruthy();
    expect(screen.queryByText('Deployment one')).toBeNull();
    expect(screen.queryByText('Archived deployment')).toBeNull();
  });

  test('opens environment status filters and refetches the list with the selected values', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments');
    const api = mockManagedResourceApi();
    const archivedAt = new Date().toISOString();
    api.resources.environments.push({
      id: 'env_archived123456',
      archived_at: archivedAt,
      config: { type: 'cloud' },
      created_at: archivedAt,
      description: 'Archived environment',
      name: 'Archived environment',
      scope: 'workspace',
      state: 'archived',
      type: 'environment',
      updated_at: archivedAt,
    });

    render(<ManagedAgentsPage section="environments" />);

    expect(await screen.findByText('Environment one')).toBeTruthy();
    expect(await screen.findByText('Archived environment')).toBeTruthy();
    await waitFor(() => {
      const latestRequest = api.requests
        .filter((request) => request.url.startsWith('/v1/environments?') && request.method === 'GET')
        .at(-1);
      expect(latestRequest).toBeTruthy();
      const params = new URL(latestRequest!.url, 'https://oma.duck.ai').searchParams;
      expect(params.get('include_archived')).toBe('true');
    });

    fireEvent.click(screen.getByRole('button', { name: 'Status All' }));
    const statusMenu = screen
      .getByRole('menuitemradio', { name: 'All' })
      .closest('[data-slot="dropdown-menu-content"]');
    expect(statusMenu?.className).toContain('bg-popover');
    expect(statusMenu?.className.includes('bg-secondary')).toBe(false);
    expect(screen.getByRole('menuitemradio', { name: 'All' })).toBeTruthy();
    expect(screen.getByRole('menuitemradio', { name: 'Active' })).toBeTruthy();
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Active' }));

    await waitFor(() => {
      const latestRequest = api.requests
        .filter((request) => request.url.startsWith('/v1/environments?') && request.method === 'GET')
        .at(-1);
      expect(latestRequest).toBeTruthy();
      const params = new URL(latestRequest!.url, 'https://oma.duck.ai').searchParams;
      expect(params.get('include_archived')).toBe('false');
    });
    expect(await screen.findByText('Environment one')).toBeTruthy();
    expect(screen.queryByText('Archived environment')).toBeNull();
  });

  test('uses shared popover chrome for deployment filters and row actions', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/deployments');
    mockManagedResourceApi();

    render(<ManagedAgentsPage section="deployments" />);

    expect(await screen.findByText('Deployment one')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Status All' }));
    const filterMenu = screen
      .getByRole('menuitemradio', { name: 'All' })
      .closest('[data-slot="dropdown-menu-content"]');
    expect(filterMenu?.className).toContain('bg-popover');
    expect(filterMenu?.className.includes('bg-secondary')).toBe(false);

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    const actionMenu = screen.getByRole('menuitem', { name: 'Copy ID' }).closest('[data-slot="dropdown-menu-content"]');
    expect(actionMenu?.className).toContain('bg-popover');
    expect(actionMenu?.className.includes('bg-secondary')).toBe(false);
  });

  test('opens session created, agent, deployment, and status filters and refetches the list with the selected values', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions');
    const api = mockManagedResourceApi();
    const archivedAt = new Date(Date.now() - 45 * 24 * 60 * 60 * 1000).toISOString();
    api.resources.agents.push(agentResponse(serverAgent));
    api.resources.sessions.push({
      id: 'sesn_server123456',
      agent: { type: 'agent', id: serverAgent.id, version: 1 },
      archived_at: archivedAt,
      created_at: archivedAt,
      deployment_id: 'dep_server123456',
      environment_id: 'env_option123456',
      status: 'terminated',
      title: 'Server session',
      type: 'session',
      updated_at: archivedAt,
      vault_ids: [],
    });

    render(<ManagedAgentsPage section="sessions" />);

    expect(await screen.findByText('Session one')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Created All time' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Agent All' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /deployment All/i })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Status Active' })).toBeTruthy();
    await waitFor(() => {
      const latestRequest = api.requests
        .filter((request) => request.url.startsWith('/v1/sessions?') && request.method === 'GET')
        .at(-1);
      expect(latestRequest).toBeTruthy();
      expect(sessionStatusValuesFromUrl(latestRequest!.url)).toEqual(['idle', 'rescheduling', 'running']);
      expect(new URL(latestRequest!.url, 'https://oma.duck.ai').searchParams.get('include_archived')).toBe('false');
    });

    fireEvent.click(screen.getByRole('button', { name: 'Status Active' }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Terminated' }));

    await waitFor(() => {
      const latestRequest = api.requests
        .filter((request) => request.url.startsWith('/v1/sessions?') && request.method === 'GET')
        .at(-1);
      expect(latestRequest).toBeTruthy();
      expect(sessionStatusValuesFromUrl(latestRequest!.url)).toEqual(['terminated']);
      expect(new URL(latestRequest!.url, 'https://oma.duck.ai').searchParams.get('include_archived')).toBe('true');
    });
    expect(await screen.findByText('Server session')).toBeTruthy();
    expect(screen.queryByText('Session one')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Agent All' }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Server agent' }));

    await waitFor(() => {
      const latestRequest = api.requests
        .filter((request) => request.url.startsWith('/v1/sessions?') && request.method === 'GET')
        .at(-1);
      expect(latestRequest).toBeTruthy();
      const params = new URL(latestRequest!.url, 'https://oma.duck.ai').searchParams;
      expect(params.get('agent_id')).toBe('agent_server123456');
      expect(params.get('include_archived')).toBe('true');
    });

    fireEvent.click(screen.getByRole('button', { name: /deployment All/i }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'dep_server123456' }));

    await waitFor(() => {
      const latestRequest = api.requests
        .filter((request) => request.url.startsWith('/v1/sessions?') && request.method === 'GET')
        .at(-1);
      expect(latestRequest).toBeTruthy();
      const params = new URL(latestRequest!.url, 'https://oma.duck.ai').searchParams;
      expect(params.get('deployment_id')).toBe('dep_server123456');
    });

    fireEvent.click(screen.getByRole('button', { name: 'Created All time' }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Last 30 days' }));

    await waitFor(() => {
      const latestRequest = api.requests
        .filter((request) => request.url.startsWith('/v1/sessions?') && request.method === 'GET')
        .at(-1);
      expect(latestRequest).toBeTruthy();
      const params = new URL(latestRequest!.url, 'https://oma.duck.ai').searchParams;
      expect(params.get('created_at[gte]')).toBeTruthy();
      expect(params.get('created_at[lte]')).toBeTruthy();
    });
    await waitFor(() => expect(screen.queryByText('Server session')).toBeNull());
  });

  test('archives and deletes resources from row action menus', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments');
    const api = mockManagedResourceApi();
    render(<ManagedAgentsPage section="environments" />);

    expect(await screen.findByText('Environment one')).toBeTruthy();
    fireEvent.click(screen.getAllByRole('button', { name: 'More actions' })[0]);
    fireEvent.click(screen.getByRole('menuitem', { name: 'Archive environment' }));
    fireEvent.click(screen.getByRole('button', { name: 'Archive' }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/v1/environments/env_one123456/archive?beta=true')).toBe(
        true,
      ),
    );
    await waitFor(() => expect(screen.queryByText('Environment one')).toBeNull());

    cleanup();
    render(<ManagedAgentsPage section="memory-stores" />);
    expect(await screen.findByText('Memory one')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete memory store' }));
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) => request.url === '/v1/memory_stores/memstore_one123456?beta=true' && request.method === 'DELETE',
        ),
      ).toBe(true),
    );
    await waitFor(() => expect(screen.queryByText('Memory one')).toBeNull());
  });

  test('uses the shared delete confirmation dialog on environment detail pages', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    const breadcrumb = screen.getByRole('navigation', { name: 'Breadcrumb' });
    expect(breadcrumb.dataset.slot).toBe('breadcrumb');
    expect(within(breadcrumb).getByRole('link', { name: 'Environments' }).getAttribute('href')).toBe(
      '/workspaces/default/environments',
    );
    expect(breadcrumb.querySelector('[data-slot="breadcrumb-page"]')?.textContent).toBe('Environment one');
    const overviewSection = screen.getByRole('heading', { name: 'Overview' }).closest('section');
    const overviewCard = overviewSection?.querySelector('[data-slot="card"]') as HTMLElement | null;
    expect(overviewCard).toBeTruthy();
    const overviewGrid = overviewCard?.querySelector('dl') as HTMLElement | null;
    expect(overviewGrid?.className).toContain('bg-border');
    expect(overviewGrid?.className.includes('bg-secondary')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    const detailActionMenu = screen
      .getByRole('menuitem', { name: 'Archive' })
      .closest('[data-slot="dropdown-menu-content"]');
    expect(detailActionMenu?.className).toContain('bg-popover');
    expect(detailActionMenu?.className.includes('bg-secondary')).toBe(false);
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }));

    const deleteDialog = await screen.findByRole('alertdialog', { name: /Delete environment/i });
    expect(deleteDialog.textContent).toContain('Environment one will be permanently removed from this workspace.');
    fireEvent.click(within(deleteDialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('alertdialog', { name: /Delete environment/i })).toBeNull());
  });

  test('uses shared card chrome for environment inline editor sections', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));

    const networkingSection = screen.getByRole('heading', { name: 'Networking' }).closest('section');
    const packagesSection = screen.getByRole('heading', { name: 'Packages' }).closest('section');
    const metadataSection = screen.getByRole('heading', { name: 'Metadata' }).closest('section');

    const networkingCard = networkingSection?.querySelector('[data-slot="card"]') as HTMLElement | null;
    const packagesCard = packagesSection?.querySelector('[data-slot="card"]') as HTMLElement | null;
    const metadataCard = metadataSection?.querySelector('[data-slot="card"]') as HTMLElement | null;

    expect(networkingCard).toBeTruthy();
    expect(networkingCard?.className).toContain('bg-card');
    expect(packagesCard).toBeTruthy();
    expect(packagesCard?.className).toContain('bg-card');
    expect(metadataCard).toBeTruthy();
    expect(metadataCard?.className).toContain('bg-card');

    expect(screen.getByRole('combobox', { name: 'Type' }).className).not.toContain('bg-secondary');
    expect(screen.getByRole('combobox', { name: 'Package manager' }).className).not.toContain('bg-secondary');
    expect(screen.getByRole('textbox', { name: 'Package value 1' }).className).not.toContain('bg-secondary');
    expect(screen.getByRole('textbox', { name: 'Metadata key 1' }).className).not.toContain('bg-secondary');
    expect(screen.getByRole('textbox', { name: 'Metadata value 1' }).className).not.toContain('bg-secondary');
  });

  test('localizes environment details, work states, and relative times in Chinese', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    mockManagedResourceApi();
    renderManagedAgentsPage('environments', 'zh-CN');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    expect(screen.getByRole('link', { name: '环境' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '概览' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '网络访问' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '软件包' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '元数据' })).toBeTruthy();
    expect(screen.queryByText(/lowercase/i)).toBeNull();
    expect(await screen.findByRole('heading', { name: '工作队列' })).toBeTruthy();
    for (const status of ['排队中', '启动中', '运行中', '停止中', '已停止', '失败']) {
      expect(screen.getByText(status)).toBeTruthy();
    }
    expect(screen.getByText('未知状态（awaiting_review）')).toBeTruthy();
    expect(document.body.textContent).toMatch(/分钟前|现在/);
  });

  test('uses the English fallback for unknown environment work statuses', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByText('Unknown status (awaiting_review)')).toBeTruthy();
  });

  test('submits normalized Environment updates only once', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    const api = mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const nameInput = screen.getByRole('textbox', { name: 'Environment name' });
    fireEvent.click(screen.getByRole('button', { name: 'Add metadata entry' }));
    fireEvent.change(nameInput, { target: { value: 'Environment Updated' } });
    fireEvent.change(screen.getByRole('textbox', { name: 'Package value 1' }), {
      target: { value: 'httpx==2.0.0' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: 'Metadata key 2' }), { target: { value: 'Team' } });
    fireEvent.change(screen.getByRole('textbox', { name: 'Metadata value 2' }), { target: { value: 'Runtime' } });
    const addMetadataButton = screen.getByRole('button', { name: 'Add metadata entry' });
    for (let index = 0; index < 14; index += 1) {
      fireEvent.click(addMetadataButton);
    }
    expect((addMetadataButton as HTMLButtonElement).disabled).toBe(true);

    const form = screen.getByRole('button', { name: 'Save changes' }).closest('form') as HTMLFormElement;
    fireEvent.submit(form);
    expect((screen.getByRole('button', { name: 'Cancel' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.submit(form);
    expect(await screen.findByRole('heading', { name: 'Environment Updated' })).toBeTruthy();
    const updateRequests = api.requests.filter(
      (request) => request.url === '/v1/environments/env_one123456?beta=true' && request.method === 'POST',
    );
    expect(updateRequests).toHaveLength(1);
    expect(updateRequests[0]?.body?.metadata).toEqual({ Team: 'Runtime' });
    expect(updateRequests[0]?.body?.config).toMatchObject({
      type: 'cloud',
      networking: { type: 'unrestricted' },
      packages: { pip: ['httpx==2.0.0'] },
    });
    expect(screen.getAllByText('Environment updated')).toHaveLength(1);
    expect(window.dispatchEvent(new window.Event('beforeunload', { cancelable: true }))).toBe(true);
  });

  test('deletes removed environment metadata with backend PATCH semantics', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    const api = mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.click(screen.getByRole('button', { name: 'Add metadata entry' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Metadata key 2' }), { target: { value: 'Team' } });
    fireEvent.change(screen.getByRole('textbox', { name: 'Metadata value 2' }), { target: { value: 'Runtime' } });
    fireEvent.click(screen.getByRole('button', { name: 'Remove metadata row 1' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByRole('heading', { name: 'Metadata' })).toBeTruthy();
    const updateRequest = api.requests.find(
      (request) => request.url === '/v1/environments/env_one123456?beta=true' && request.method === 'POST',
    );
    expect(updateRequest?.body?.metadata).toEqual({ Owner: null, Team: 'Runtime' });
    const metadataSection = screen.getByRole('heading', { name: 'Metadata' }).closest('section');
    expect(metadataSection?.textContent).toContain('Team');
    expect(metadataSection?.textContent).toContain('Runtime');
    expect(metadataSection?.textContent).not.toContain('Owner');
    expect(metadataSection?.textContent).not.toContain('Platform');
  });

  test('preserves unchanged empty Environment metadata by omitting it from the PATCH', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    const api = mockManagedResourceApi();
    api.resources.environments[0] = {
      ...api.resources.environments[0],
      metadata: { Flag: '', Owner: 'Platform' },
    };
    renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Environment name' }), {
      target: { value: 'Environment with flag' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByRole('heading', { name: 'Environment with flag' })).toBeTruthy();
    const updateRequest = api.requests.find(
      (request) => request.url === '/v1/environments/env_one123456?beta=true' && request.method === 'POST',
    );
    expect(updateRequest?.body?.metadata).toEqual({});
    expect(api.resources.environments[0]?.metadata).toEqual({ Flag: '', Owner: 'Platform' });
    const metadataSection = screen.getByRole('heading', { name: 'Metadata' }).closest('section');
    expect(metadataSection?.textContent).toContain('Flag');
  });

  test('allows payload-equivalent Environment edits to close without confirmation', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog', { name: 'Discard unsaved changes?' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Save changes' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    const packageInput = screen.getByRole('textbox', { name: 'Package value 1' }) as HTMLInputElement;
    fireEvent.change(packageInput, { target: { value: `  ${packageInput.value}  ` } });
    fireEvent.click(screen.getByRole('button', { name: 'Add package' }));
    fireEvent.click(screen.getByRole('button', { name: 'Add metadata entry' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog', { name: 'Discard unsaved changes?' })).toBeNull();
  });

  test('exits environment editing after archiving from the detail page', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    const api = mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByRole('heading', { name: 'Environment one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Environment name' }), {
      target: { value: 'Environment changed' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Archive' }));
    fireEvent.click(screen.getByRole('button', { name: 'Archive' }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/v1/environments/env_one123456/archive?beta=true')).toBe(
        true,
      ),
    );
    expect(
      await screen.findByText(
        'This environment is read-only. Its configuration and work queue remain available for reference.',
      ),
    ).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Edit' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole('button', { name: 'Save changes' })).toBeNull();
  });

  test('renders archived environments as localized read-only details', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_one123456');
    const api = mockManagedResourceApi();
    api.resources.environments[0] = {
      ...api.resources.environments[0],
      archived_at: new Date().toISOString(),
      state: 'archived',
    };
    renderManagedAgentsPage('environments', 'zh-CN');

    expect(await screen.findByText('此环境为只读状态。其配置和工作队列仍可供查看。')).toBeTruthy();
    expect((screen.getByRole('button', { name: '编辑' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByRole('heading', { name: '网络访问' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '软件包' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '元数据' })).toBeTruthy();
    expect(await screen.findByRole('heading', { name: '工作队列' })).toBeTruthy();
    expect(screen.queryByText(/Snapshot|Version|Diff|Rollback/i)).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: '更多操作' }));
    expect((screen.getByRole('menuitem', { name: '删除' }) as HTMLElement).getAttribute('aria-disabled')).not.toBe(
      'true',
    );
    fireEvent.click(screen.getByRole('menuitem', { name: '删除' }));
    const deleteDialog = await screen.findByRole('alertdialog', { name: /删除环境/ });
    fireEvent.click(within(deleteDialog).getByRole('button', { name: '删除' }));
    await waitFor(() =>
      expect(
        api.requests.some(
          (request) => request.url === '/v1/environments/env_one123456?beta=true' && request.method === 'DELETE',
        ),
      ).toBe(true),
    );
  });

  test('closes an environment creation dialog without confirmation for payload-equivalent description whitespace', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments');
    mockManagedResourceApi();
    renderManagedAgentsPage('environments');

    expect(await screen.findByText('Environment one')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Create environment' }));
    const dialog = screen.getByRole('dialog', { name: 'Create environment' });
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Description' }), { target: { value: '   ' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));

    expect(screen.queryByRole('alertdialog', { name: 'Discard unsaved changes?' })).toBeNull();
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create environment' })).toBeNull());
  });

  test('keeps environment creation fields and default payload unchanged in Chinese', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments');
    const api = mockManagedResourceApi();
    renderManagedAgentsPage('environments', 'zh-CN');

    expect(await screen.findByText('Environment one')).toBeTruthy();
    expect(screen.getAllByText('云端').length).toBeGreaterThan(0);
    expect(screen.getAllByText('活跃').length).toBeGreaterThan(0);
    expect(document.body.textContent).toContain('现在');
    fireEvent.click(screen.getByRole('button', { name: '创建环境' }));
    const dialog = screen.getByRole('dialog', { name: '创建环境' });
    expect(within(dialog).getByText('创建可供 Agent 工具复用的云端容器模板。')).toBeTruthy();
    fireEvent.change(within(dialog).getByRole('textbox', { name: '名称' }), { target: { value: '中文环境' } });
    fireEvent.change(within(dialog).getByRole('textbox', { name: '描述' }), { target: { value: '用于测试' } });
    expect(within(dialog).getByRole('textbox', { name: '托管类型' })).toBeTruthy();
    expect(within(dialog).queryByText('网络访问')).toBeNull();
    expect(within(dialog).queryByText('软件包')).toBeNull();
    expect(within(dialog).queryByText('元数据')).toBeNull();
    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }));
    const discardDialog = await screen.findByRole('alertdialog', { name: '放弃未保存的更改？' });
    fireEvent.click(within(discardDialog).getByRole('button', { name: '继续编辑' }));

    const form = within(dialog).getByRole('button', { name: '创建' }).closest('form') as HTMLFormElement;
    fireEvent.submit(form);
    fireEvent.submit(form);
    await waitFor(() =>
      expect(
        api.requests.filter((request) => request.url === '/v1/environments?beta=true' && request.method === 'POST'),
      ).toHaveLength(1),
    );
    const request = api.requests.find((item) => item.url === '/v1/environments?beta=true' && item.method === 'POST');
    expect(request?.body).toMatchObject({
      name: '中文环境',
      description: '用于测试',
      metadata: {},
      config: {
        type: 'cloud',
        networking: { type: 'unrestricted' },
        packages: { type: 'packages', apt: [], cargo: [], gem: [], go: [], npm: [], pip: [] },
      },
    });
  });

  test('runs and pauses deployments from the official-style action menu', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/deployments');
    const api = mockManagedResourceApi();
    render(<ManagedAgentsPage section="deployments" />);

    expect(await screen.findByText('Deployment one')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Run deployment' }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/v1/deployments/dep_one123456/run?beta=true')).toBe(true),
    );

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Pause deployment' }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/v1/deployments/dep_one123456/pause?beta=true')).toBe(
        true,
      ),
    );
  });

  test('renders the dreaming loading panel', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/dreams');
    render(<ManagedAgentsPage section="dreams" />);

    expect(screen.getByRole('heading', { name: 'Dreaming' })).toBeTruthy();
    expect(screen.getByText('Captured Dreaming assets are loading.')).toBeTruthy();
  });
}
