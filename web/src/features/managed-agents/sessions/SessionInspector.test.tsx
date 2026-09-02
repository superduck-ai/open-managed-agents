import { afterEach, describe, expect, mock, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import { TooltipProvider } from '../../../shared/ui/tooltip';
import { resetTestDom } from '../../../test/setup';
import { type DisplayEventEntry, type QuickstartSessionEvent, type SessionApiResponse } from '../types';
import { SessionInspector } from './SessionInspector';

const { act, cleanup, fireEvent, render, screen } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function renderInspector(
  overrides: Partial<Parameters<typeof SessionInspector>[0]> = {},
  initialLocale: 'en' | 'zh-CN' = 'en',
) {
  return render(
    <I18nProvider initialLocale={initialLocale}>
      <TooltipProvider>
        <SessionInspector
          activeTab="session"
          activeLane=""
          events={[]}
          eventsByLaneId={new Map()}
          lanes={[]}
          onActiveTabChange={() => {}}
          onClose={() => {}}
          onSelectEntry={() => {}}
          onSelectLane={() => {}}
          refreshKey={0}
          selectedEntry={null}
          session={session()}
          workspaceId="default"
          {...overrides}
        />
      </TooltipProvider>
    </I18nProvider>,
  );
}

describe('SessionInspector', () => {
  test('does not invent a zero cost when usage is missing', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');

    renderInspector();
    await act(async () => Promise.resolve());

    expect(screen.getByText('Cost').parentElement?.textContent).toBe('Cost—');
    expect(screen.getByText('No cost tracked yet.')).toBeTruthy();
  });

  test('renders cumulative session cost and usage snapshots with event linkage', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    const onHoverEvent = mock(() => {});
    const onSelectEntry = mock(() => {});
    const events: QuickstartSessionEvent[] = [
      { id: 'user_first', type: 'user.message', created_at: '2026-08-27T08:00:00.000Z' },
      {
        id: 'usage_first',
        type: 'session.usage',
        created_at: '2026-08-27T08:00:01.000Z',
        usage: {
          input_tokens: 100,
          output_tokens: 20,
          cache_read_input_tokens: 30,
          cache_creation: { ephemeral_5m_input_tokens: 40, ephemeral_1h_input_tokens: 5 },
          list_cost: { amount: '125', currency: 'USD' },
          active_seconds: 2.5,
          server_tool_use: { web_search_requests: 3 },
        },
      },
      {
        id: 'usage_second',
        type: 'session.usage',
        created_at: '2026-08-27T08:00:02.000Z',
        usage: {
          input_tokens: 150,
          output_tokens: 25,
          cache_read_input_tokens: 35,
          cache_creation: { ephemeral_5m_input_tokens: 45, ephemeral_1h_input_tokens: 5 },
          list_cost: { amount: '175', currency: 'USD' },
          active_seconds: 4,
          server_tool_use: { web_search_requests: 4 },
        },
      },
    ];

    const view = renderInspector({
      events,
      onHoverEvent,
      onSelectEntry,
      session: { ...session(), usage: events.at(-1)?.usage },
    });
    await act(async () => Promise.resolve());

    const chart = screen.getByRole('img', { name: 'Cumulative session cost over time' });
    const firstPoint = view.container.querySelector('[data-cost-event-id="usage_first"]')!;
    expect(chart.classList.contains('h-[120px]')).toBe(true);
    expect(chart.querySelectorAll('path')).toHaveLength(2);
    expect(screen.getAllByText('$1.75')).toHaveLength(2);
    expect(screen.getByText('Input tokens').parentElement?.textContent).toContain('150');
    expect(screen.getByText('Cache write').parentElement?.textContent).toContain('50');
    expect(screen.getByText('Web searches').parentElement?.textContent).toContain('4');
    expect(screen.getByText('Active time').parentElement?.textContent).toContain('4.0s');
    expect(firstPoint.getAttribute('aria-label')).toContain('This step $1.25');
    fireEvent.pointerEnter(firstPoint);
    expect(onHoverEvent).toHaveBeenCalledWith('usage_first');
    fireEvent.pointerLeave(firstPoint);
    expect(onHoverEvent).toHaveBeenLastCalledWith(null);
    fireEvent.click(firstPoint);
    expect(onSelectEntry).toHaveBeenCalledWith('usage_first');
  });

  test('opens event detail in the resizable Claude-style list/detail split', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    const event = sessionEvent();
    const nextEvent = { ...sessionEvent(), id: 'evt_agent_message_2', created_at: '2026-08-27T07:25:12.000Z' };
    const entry = sessionEventEntry(event);
    const onSelectEntry = mock(() => {});

    const view = renderInspector({
      activeTab: 'events',
      events: [event, nextEvent],
      onSelectEntry,
      selectedEntry: entry,
    });
    await act(async () => Promise.resolve());

    const inspector = screen.getByTestId('session-inspector');
    const [selectedOption, nextOption] = screen.getAllByRole('option');
    expect(inspector.className).toContain('session-inspector-shell');
    expect(selectedOption.getAttribute('aria-selected')).toBe('true');
    expect(selectedOption.className).toContain('bg-accent');
    expect(selectedOption.className).not.toContain('shadow-[');
    expect(selectedOption.className).toContain('outline-none');
    expect(selectedOption.className).not.toContain('border-foreground');
    expect(selectedOption.getAttribute('data-backed')).toBe('true');

    fireEvent.click(selectedOption);
    expect(onSelectEntry).toHaveBeenCalledWith(event.id);
    fireEvent.keyDown(selectedOption, { key: 'j' });
    expect(onSelectEntry).toHaveBeenLastCalledWith(nextEvent.id);
    expect(document.activeElement).toBe(nextOption);

    const eventList = screen.getByTestId('session-inspector-events-list');
    const eventDetail = screen.getByTestId('session-inspector-event-detail-scroll');
    const detailPanel = screen.getByTestId('session-inspector-event-detail-content');
    const eventsHeader = screen.getByTestId('session-inspector-events-header');
    expect(screen.getByRole('combobox', { name: 'Filter events' }).textContent).toContain('All events');
    expect(eventsHeader.textContent).toContain('Event');
    expect(eventsHeader.textContent).toContain('Preview');
    expect(eventsHeader.textContent).toContain('Time');
    expect(selectedOption.querySelector('.min-w-48')).toBeTruthy();
    expect(eventList.querySelectorAll('[data-slot="scroll-area-viewport"]')).toHaveLength(1);
    expect(eventDetail.querySelectorAll('[data-slot="scroll-area-viewport"]')).toHaveLength(1);
    expect(view.container.querySelectorAll('[data-slot="scroll-area-viewport"]')).toHaveLength(2);
    expect(screen.getByRole('separator', { name: 'Resize event detail' })).toBeTruthy();
    expect(detailPanel.className).toContain('h-full');
    expect(detailPanel.className).toContain('bg-card');
    expect(detailPanel.className).toContain('session-inspector-detail-card');
    expect(detailPanel.className).toContain('z-20');
    expect(detailPanel.className).not.toContain('rounded-t-2xl');
    expect(detailPanel.className).not.toContain('absolute');
    expect(screen.getByTestId('session-trace-detail').getAttribute('data-placement')).toBe('side');
    expect(screen.getByTestId('session-trace-detail').className).toContain('bg-transparent');
    expect(screen.getByTestId('session-trace-detail').querySelector('.overflow-auto')).toBeNull();

    fireEvent.keyDown(inspector, { key: 'Escape' });
    expect(onSelectEntry).toHaveBeenCalledWith(null);
  });

  test('renders the Claude events filter without changing backend event order or selected detail', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    const agentEvent = sessionEvent();
    const statusEvent: QuickstartSessionEvent = {
      id: 'evt_status_running',
      type: 'session.status_running',
      created_at: '2026-08-27T07:25:12.000Z',
    };
    const systemEvent: QuickstartSessionEvent = {
      id: 'evt_system_message',
      type: 'system.message',
      created_at: '2026-08-27T07:25:13.000Z',
      content: 'Internal status update.',
    };

    renderInspector({
      activeTab: 'events',
      events: [agentEvent, statusEvent, systemEvent],
      selectedEntry: sessionEventEntry(systemEvent),
    });
    await act(async () => Promise.resolve());

    const eventIds = () => screen.queryAllByRole('option').map((row) => row.getAttribute('data-event-id'));
    const trigger = screen.getByRole('combobox', { name: 'Filter events' });
    expect(eventIds()).toEqual(['evt_agent_message', 'evt_status_running', 'evt_system_message']);
    expect(trigger.textContent).toContain('All events');
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(screen.getByTestId('session-inspector-event-detail-content')).toBeTruthy();
  });

  test('keeps related entity links usable when metadata requests fail', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    globalThis.fetch = mock(async () => {
      throw new Error('metadata unavailable');
    }) as typeof fetch;

    renderInspector({
      session: {
        ...session(),
        agent: { id: 'agent_missing', version: 3 },
        environment_id: 'env_missing',
        vault_ids: ['vault_first', 'vault_second'],
      },
    });
    await act(async () => Promise.resolve());

    expect(screen.getByTestId('session-inspector').className).toContain('bg-card');
    expect(screen.getByRole('link', { name: 'agent_missing' }).getAttribute('href')).toBe(
      '/workspaces/default/agents/agent_missing',
    );
    expect(screen.getByRole('link', { name: 'env_missing' }).getAttribute('href')).toBe(
      '/workspaces/default/environments/env_missing',
    );
    expect(screen.getByRole('link', { name: 'vault_first' }).getAttribute('href')).toBe(
      '/workspaces/default/vaults/vault_first',
    );
    expect(screen.getByRole('link', { name: 'vault_second' }).getAttribute('href')).toBe(
      '/workspaces/default/vaults/vault_second',
    );
  });

  test('distinguishes an empty event stream from an empty filter result', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    renderInspector({ activeTab: 'events' });
    await act(async () => Promise.resolve());

    expect(screen.getByText('No events yet')).toBeTruthy();
    expect(screen.queryByText('No matching events.')).toBeNull();
  });

  test('renders calls, failures, finished-call p50, and outcome totals from raw events', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    const onHoverEvent = mock(() => {});
    const onSelectEntry = mock(() => {});
    const events = [
      toolUseEvent('tool_bash_ok', 'Bash', '2026-08-27T08:00:00.000Z'),
      toolResultEvent('result_bash_ok', 'tool_bash_ok', '2026-08-27T08:00:01.000Z'),
      toolUseEvent('tool_bash_failed', 'Bash', '2026-08-27T08:00:02.000Z'),
      {
        ...toolResultEvent('result_bash_failed', 'tool_bash_failed', '2026-08-27T08:00:05.000Z'),
        is_error: true,
      },
      toolUseEvent('tool_write_running', 'Write', '2026-08-27T08:00:06.000Z'),
    ];

    renderInspector({ activeTab: 'tools', events, onHoverEvent, onSelectEntry });
    await act(async () => Promise.resolve());

    const bashCells = screen.getByText('bash').closest('tr')?.querySelectorAll('td');
    const writeCells = screen.getByText('write').closest('tr')?.querySelectorAll('td');
    expect(screen.getByRole('searchbox', { name: 'Filter tools' })).toBeTruthy();
    expect(screen.queryByRole('combobox', { name: 'Scope' })).toBeNull();
    const toolHeaders = screen.getAllByRole('columnheader');
    expect(toolHeaders[0]?.className).toContain('min-w-24');
    expect(toolHeaders[1]?.className).toContain('w-[88px]');
    expect(toolHeaders[2]?.className).toContain('w-14');
    expect(toolHeaders[3]?.className).toContain('w-[72px]');
    expect(toolHeaders[4]?.className).toContain('w-16');
    expect(bashCells?.[0]?.className).toContain('h-6');
    expect(bashCells?.[2]?.textContent).toBe('2');
    expect(bashCells?.[3]?.textContent).toBe('1');
    expect(bashCells?.[3]?.className).toContain('text-destructive');
    expect(bashCells?.[4]?.textContent).toBe('2.0s');
    expect(writeCells?.[2]?.textContent).toBe('1');
    expect(writeCells?.[3]?.textContent).toBe('0');
    expect(writeCells?.[4]?.textContent).toBe('—');
    expect(screen.getByText('2 tools · 2 used · 3 calls')).toBeTruthy();
    const outcomeRing = screen.getByRole('img', { name: '1 completed, 1 failed, 0 denied, 1 in flight' });
    expect(outcomeRing.tagName).toBe('DIV');
    expect(outcomeRing.style.background).toContain('conic-gradient');
    expect(outcomeRing.querySelector('svg')).toBeNull();
    expect(screen.getByText('In flight')).toBeTruthy();
    expect(screen.getByText('Executing')).toBeTruthy();
    const failedLegend = screen.getAllByText('Failed').find((element) => element.tagName === 'DT');
    expect(failedLegend?.nextElementSibling?.textContent).toBe('1 (33%)');
    expect(screen.getByRole('separator', { name: 'Resize tool detail' })).toBeTruthy();

    const bashRow = screen.getByText('bash').closest('tr')!;
    fireEvent.click(bashRow);
    expect(bashRow.getAttribute('data-state')).toBe('selected');
    expect(screen.getAllByText('bash')).toHaveLength(2);
    expect(screen.getByRole('button', { name: 'Close tool detail' })).toBeTruthy();
    expect(screen.queryByRole('columnheader', { name: 'Waited' })).toBeNull();
    expect(screen.queryByRole('columnheader', { name: 'Thread' })).toBeNull();
    const completedCallRow = screen.getByText('completed').closest('tr')!;
    fireEvent.pointerEnter(completedCallRow);
    expect(onHoverEvent).toHaveBeenCalledWith('tool_bash_ok');
    fireEvent.pointerLeave(completedCallRow);
    expect(onHoverEvent).toHaveBeenLastCalledWith(null);
    fireEvent.click(completedCallRow);
    expect(onSelectEntry).toHaveBeenCalledWith('tool_bash_ok');
    fireEvent.keyDown(bashRow, { key: 'Escape' });
    expect(screen.queryByRole('button', { name: 'Close tool detail' })).toBeNull();
    expect(screen.getByText('Overview')).toBeTruthy();

    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter tools' }), { target: { value: 'write' } });
    expect(screen.queryByText('bash')).toBeNull();
    expect(screen.getByText('write')).toBeTruthy();
  });

  test('localizes empty exceptional tool outcomes while keeping completed numeric', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    renderInspector(
      {
        activeTab: 'tools',
        events: [toolUseEvent('tool_write_running', 'Write', '2026-08-27T08:00:00.000Z')],
      },
      'zh-CN',
    );
    await act(async () => Promise.resolve());

    const failed = screen.getAllByText('失败').find((element) => element.tagName === 'DT');
    const denied = screen.getByText('已拒绝');
    const completed = screen.getByText('已完成');
    expect(failed?.nextElementSibling?.textContent).toBe('无');
    expect(denied.nextElementSibling?.textContent).toBe('无');
    expect(completed.nextElementSibling?.textContent).toBe('0');
    expect(completed.closest('dl')?.parentElement?.className).toContain('gap-4');
  });

  test('shows Waited and Thread only when the selected calls need them', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    const toolUse = {
      ...toolUseEvent('tool_bash', 'Bash', '2026-08-27T08:00:00.000Z'),
      evaluated_permission: 'ask',
    };
    const confirmation: QuickstartSessionEvent = {
      id: 'confirmation_bash',
      type: 'user.tool_confirmation',
      created_at: '2026-08-27T08:00:01.000Z',
      tool_use_id: 'tool_bash',
      result: 'allow',
    };
    const result = toolResultEvent('result_bash', 'tool_bash', '2026-08-27T08:00:02.000Z');
    const child = {
      ...toolUseEvent('tool_child', 'Read', '2026-08-27T08:00:03.000Z'),
      session_thread_id: 'child',
    };
    const events = [toolUse, confirmation, result, child];

    renderInspector({
      activeTab: 'tools',
      activeLane: 'main',
      events,
      eventsByLaneId: new Map([
        ['main', [toolUse, confirmation, result]],
        ['child', [child]],
      ]),
      lanes: [
        { id: 'main', label: 'Main', group: 'Main', isMain: true },
        { id: 'child', label: 'Researcher', group: 'Researcher' },
      ],
    });
    await act(async () => Promise.resolve());

    fireEvent.click(screen.getByText('bash').closest('tr')!);
    expect(screen.getByRole('columnheader', { name: 'Waited' })).toBeTruthy();
    expect(screen.getByRole('columnheader', { name: 'Thread' })).toBeTruthy();
  });

  test('renders the file-only resource menu trigger', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    renderInspector({ activeTab: 'resources' });
    await act(async () => Promise.resolve());

    const trigger = screen.getByRole('button', { name: 'Resource' });
    expect(trigger.getAttribute('aria-haspopup')).toBe('menu');
    expect(screen.queryByRole('menuitem', { name: /GitHub|Memory store/i })).toBeNull();
  });

  test('uses the Claude thread columns backed by current data', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    const onHoverEvent = mock(() => {});
    const onSelectLane = mock(() => {});
    renderInspector({
      activeTab: 'threads',
      activeLane: 'child',
      eventsByLaneId: new Map([
        ['main', []],
        [
          'child',
          [
            {
              id: 'model_request_end',
              type: 'span.model_request_end',
              processed_at: '2026-08-27T08:00:02.000Z',
              model_usage: {
                input_tokens: 100,
                cache_read_input_tokens: 20,
                cache_creation_input_tokens: 5,
              },
            },
            { id: 'thread_rescheduled', type: 'session.thread_status_rescheduled' },
          ],
        ],
      ]),
      lanes: [
        { id: 'main', label: 'Main', isMain: true },
        { id: 'child', label: 'Researcher' },
      ],
      onHoverEvent,
      onSelectLane,
    });
    await act(async () => Promise.resolve());

    const headers = screen.getAllByRole('columnheader');
    expect(headers.map((header) => header.textContent)).toEqual(['Thread', 'Status', 'Context']);
    expect(headers[1]?.className).toContain('w-24');
    expect(headers[2]?.className).toContain('w-[72px]');
    const threadCell = screen.getAllByText('Main').find((element) => element.tagName === 'TD');
    const childCell = screen.getAllByText('Researcher').find((element) => element.tagName === 'TD');
    expect(childCell?.closest('tr')?.getAttribute('data-state')).toBe('selected');
    expect(screen.getAllByText('Rescheduling')[0]?.className).toContain('text-warning');
    expect(screen.getByRole('separator', { name: 'Resize thread detail' })).toBeTruthy();
    const contextChart = screen.getByRole('img', { name: 'Context size at each model request' });
    expect(contextChart.getAttribute('class')).toContain('h-[140px]');
    expect(contextChart.querySelectorAll('path')).toHaveLength(2);
    expect(contextChart.querySelector('path')?.getAttribute('fill')).toBe('var(--chart-1)');
    const contextPoint = contextChart.querySelector('[data-context-event-id="model_request_end"]')!;
    fireEvent.pointerEnter(contextPoint);
    expect(onHoverEvent).toHaveBeenCalledWith('model_request_end');
    fireEvent.pointerLeave(contextPoint);
    expect(onHoverEvent).toHaveBeenLastCalledWith(null);
    fireEvent.click(threadCell!.closest('tr')!);
    expect(onSelectLane).toHaveBeenCalledWith('main');
    expect(childCell?.closest('tr')?.getAttribute('data-state')).toBe('selected');
  });
});

function sessionEvent(): QuickstartSessionEvent {
  return {
    id: 'evt_agent_message',
    type: 'agent.message',
    created_at: '2026-08-27T07:25:11.000Z',
    content: [{ type: 'text', text: 'A compact event preview.' }],
  };
}

function toolUseEvent(id: string, name: string, createdAt: string): QuickstartSessionEvent {
  return { id, type: 'agent.tool_use', created_at: createdAt, name, input: {} };
}

function toolResultEvent(id: string, toolUseId: string, createdAt: string): QuickstartSessionEvent {
  return { id, type: 'agent.tool_result', created_at: createdAt, tool_use_id: toolUseId, content: [] };
}

function sessionEventEntry(event: QuickstartSessionEvent): DisplayEventEntry {
  const createdAtMs = Date.parse(String(event.created_at));
  const usage = {
    input: 0,
    output: 0,
    input_tokens: 0,
    output_tokens: 0,
    cache_read_input_tokens: 0,
    cache_creation_input_tokens: 0,
  };
  return {
    id: 'entry-agent-message',
    kind: 'message',
    displayEvent: {
      id: String(event.id),
      type: 'agent',
      rawType: String(event.type),
      label: 'Agent',
      content: 'A compact event preview.',
      event,
      isQueued: false,
      isStreaming: false,
      isError: false,
      createdAtMs,
      processedAtMs: createdAtMs,
      relativeTime: '0:01',
    },
    traceEntry: {
      id: 'trace-agent-message',
      type: String(event.type),
      family: 'agent',
      label: 'Agent',
      preview: 'A compact event preview.',
      displayText: 'A compact event preview.',
      displayKind: 'prose',
      event,
      createdAtMs,
      relativeTime: '0:01',
      rawEventId: String(event.id),
      searchText: 'a compact event preview.',
      isError: false,
    },
    event,
    type: String(event.type),
    rawEventId: String(event.id),
    createdAtMs,
    processedAtMs: createdAtMs,
    relativeTime: '0:01',
    searchText: 'a compact event preview.',
    isError: false,
    usage,
    inferenceMs: 0,
    executionMs: 0,
  };
}

function session(): SessionApiResponse {
  return {
    id: 'sesn_test',
    agent: {},
    archived_at: null,
    created_at: '2026-08-27T07:25:00.000Z',
    environment_id: '',
    resources: [],
    status: 'idle',
    type: 'session',
    updated_at: '2026-08-27T07:25:22.000Z',
  };
}
