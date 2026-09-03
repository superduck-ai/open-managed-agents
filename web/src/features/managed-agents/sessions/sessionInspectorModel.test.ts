import { describe, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { type AgentApiResponse, type QuickstartSessionEvent } from '../types';
import {
  buildInspectorEventListItems,
  buildInspectorEventRows,
  buildInspectorCostPoints,
  buildInspectorContextPoints,
  buildInspectorThreadRows,
  buildInspectorToolRows,
  buildInspectorToolTotals,
  filterInspectorEventRows,
  inspectorBackingEventIds,
  inspectorSessionListCost,
  readSessionInspectorTab,
  sessionInspectorTabHref,
  writeSessionInspectorUrlState,
} from './sessionInspectorModel';
import { buildSessionEventEntries } from './sessionTraceModel';

describe('session inspector event order', () => {
  test('uses real URLs for inspector tabs and restores the selected tab', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test?event=evt_1&inspector=tools');

    expect(readSessionInspectorTab()).toBe('tools');
    expect(sessionInspectorTabHref('events')).toBe(
      '/workspaces/default/sessions/sesn_test?event=evt_1&inspector=events',
    );
    writeSessionInspectorUrlState('session');
    expect(window.location.pathname + window.location.search).toBe(
      '/workspaces/default/sessions/sesn_test?event=evt_1',
    );
  });

  test('opens trace deep links in the traces tab and clears the trace when leaving it', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test?trace_id=trace_1');

    expect(readSessionInspectorTab()).toBe('traces');
    expect(sessionInspectorTabHref('events')).toBe('/workspaces/default/sessions/sesn_test?inspector=events');
  });

  test('preserves the backend response order when timestamps run backwards', () => {
    const events: QuickstartSessionEvent[] = [
      { id: 'sevt_first', type: 'user.message', processed_at: '2026-08-27T08:00:02.000Z' },
      { id: 'sevt_second', type: 'agent.message', processed_at: '2026-08-27T08:00:01.000Z' },
    ];

    expect(buildInspectorEventRows(events).map((row) => row.id)).toEqual(['sevt_first', 'sevt_second']);
  });

  test('keeps unprocessed events queued even when they have a creation time', () => {
    const [row] = buildInspectorEventRows([
      {
        id: 'sevt_queued',
        type: 'user.message',
        created_at: '2026-08-27T08:00:02.000Z',
        processed_at: null,
      },
    ]);

    expect(row?.processedAtMs).toBe(0);
  });

  test('intersects Claude transcript and wire-type filters without reordering rows', () => {
    const rows = buildInspectorEventRows([
      { id: 'evt_agent', type: 'agent.message', created_at: '2026-08-27T08:00:02.000Z', content: 'Agent' },
      { id: 'evt_status', type: 'session.status_running', created_at: '2026-08-27T08:00:01.000Z' },
      { id: 'evt_system', type: 'system.message', created_at: '2026-08-27T08:00:00.000Z', content: 'System' },
    ]);

    expect(filterInspectorEventRows(rows, { transcriptOnly: false, types: [] }).map((row) => row.id)).toEqual([
      'evt_agent',
      'evt_status',
      'evt_system',
    ]);
    expect(filterInspectorEventRows(rows, { transcriptOnly: true, types: [] }).map((row) => row.id)).toEqual([
      'evt_agent',
    ]);
    expect(
      filterInspectorEventRows(rows, { transcriptOnly: true, types: ['session.status_running'] }).map((row) => row.id),
    ).toEqual([]);
  });

  test('groups filtered events by their original turns without merging across a hidden boundary', () => {
    const events: QuickstartSessionEvent[] = [
      modelStart('span_first'),
      { id: 'evt_first', type: 'agent.message', content: 'First turn' },
      { id: 'evt_idle', type: 'session.status_idle' },
      modelStart('span_second'),
      { id: 'evt_second', type: 'agent.message', content: 'Second turn' },
      modelEnd('span_second_end', 'span_second'),
    ];
    const rows = buildInspectorEventRows(events);
    const allItems = buildInspectorEventListItems(events, rows);
    const filteredItems = buildInspectorEventListItems(
      events,
      rows.filter((row) => row.id !== 'evt_idle'),
    );

    expect(allItems.map((item) => item.type)).toEqual(['turn', 'event', 'turn']);
    expect(filteredItems.map((item) => item.type)).toEqual(['turn', 'turn']);
    expect(
      filteredItems.map((item) => (item.type === 'turn' ? item.rows.map((row) => row.id) : [item.row.id])),
    ).toEqual([
      ['span_first', 'evt_first'],
      ['span_second', 'evt_second', 'span_second_end'],
    ]);
  });

  test('backs only the selected tool use, confirmation, and result', () => {
    const events: QuickstartSessionEvent[] = [
      modelStart('span_tool'),
      { ...toolUse('tool_bash', 'Bash', '2026-08-27T08:00:00.000Z'), evaluated_permission: 'ask' },
      {
        id: 'confirmation_bash',
        type: 'user.tool_confirmation',
        tool_use_id: 'tool_bash',
        result: 'allow',
      },
      toolResult('result_bash', 'tool_bash', '2026-08-27T08:00:01.000Z'),
      modelEnd('span_tool_end', 'span_tool'),
    ];
    const selectedEntry = buildSessionEventEntries(events, 'debug').find((entry) =>
      'traceEntry' in entry ? entry.traceEntry.rawEventId === 'result_bash' : false,
    );

    expect([...inspectorBackingEventIds(events, selectedEntry ?? null)]).toEqual(
      expect.arrayContaining(['tool_bash', 'confirmation_bash', 'result_bash']),
    );
  });
});

describe('session inspector tool metrics', () => {
  test('counts calls across threads and calculates failures and finished-call p50 latency', () => {
    const events: QuickstartSessionEvent[] = [
      toolUse('tool_bash_ok', 'Bash', '2026-08-27T08:00:00.000Z'),
      toolResult('result_bash_ok', 'tool_bash_ok', '2026-08-27T08:00:01.000Z'),
      {
        ...toolUse('tool_bash_failed', 'Bash', '2026-08-27T08:00:02.000Z'),
        session_thread_id: 'sthr_worker',
      },
      {
        ...toolResult('result_bash_failed', 'tool_bash_failed', '2026-08-27T08:00:05.000Z'),
        is_error: true,
        session_thread_id: 'sthr_worker',
      },
      toolUse('tool_write_running', 'Write', '2026-08-27T08:00:06.000Z'),
    ];

    const rows = buildInspectorToolRows(events, null);
    const bash = rows.find((row) => row.name === 'bash');
    const write = rows.find((row) => row.name === 'write');

    expect(bash?.calls).toHaveLength(2);
    expect(bash?.failed).toBe(1);
    expect(bash?.p50Ms).toBe(2000);
    expect(write?.calls).toHaveLength(1);
    expect(write?.p50Ms).toBeUndefined();
    expect(buildInspectorToolTotals(rows)).toEqual({
      calls: 3,
      completed: 1,
      denied: 0,
      executingMs: 4000,
      failed: 1,
      inFlight: 1,
      timeInToolsMs: 4000,
      tools: 2,
      used: 2,
      waitingMs: 0,
    });
  });

  test('counts parallel calls in one thread by wall-clock time', () => {
    const events: QuickstartSessionEvent[] = [
      modelStart('span_parallel_tools'),
      {
        ...toolUse('tool_bash_approval', 'Bash', '2026-08-27T08:00:00.000Z'),
        evaluated_permission: 'ask',
      },
      toolUse('tool_write_parallel', 'Write', '2026-08-27T08:00:01.000Z'),
      {
        id: 'confirmation_bash',
        type: 'user.tool_confirmation',
        created_at: '2026-08-27T08:00:02.000Z',
        tool_use_id: 'tool_bash_approval',
        result: 'allow',
      },
      toolResult('result_write_parallel', 'tool_write_parallel', '2026-08-27T08:00:03.000Z'),
      toolResult('result_bash_approval', 'tool_bash_approval', '2026-08-27T08:00:05.000Z'),
      modelEnd('span_parallel_tools_end', 'span_parallel_tools'),
    ];

    expect(buildInspectorToolTotals(buildInspectorToolRows(events, null))).toMatchObject({
      calls: 2,
      completed: 2,
      executingMs: 4000,
      inFlight: 0,
      timeInToolsMs: 5000,
      waitingMs: 2000,
    });
  });

  test('counts completed calls even when another call remains open', () => {
    const openBatch: QuickstartSessionEvent[] = [
      modelStart('span_open_batch'),
      toolUse('tool_bash_done', 'Bash', '2026-08-27T08:00:00.000Z'),
      toolUse('tool_write_open', 'Write', '2026-08-27T08:00:01.000Z'),
      toolResult('result_bash_done', 'tool_bash_done', '2026-08-27T08:00:03.000Z'),
    ];
    expect(buildInspectorToolTotals(buildInspectorToolRows(openBatch, null))).toMatchObject({
      executingMs: 3000,
      timeInToolsMs: 3000,
      waitingMs: 0,
    });

    const closedAcrossThreads: QuickstartSessionEvent[] = [
      toolUse('tool_main', 'Bash', '2026-08-27T08:00:00.000Z'),
      toolResult('result_main', 'tool_main', '2026-08-27T08:00:03.000Z'),
      {
        ...toolUse('tool_worker', 'Read', '2026-08-27T08:00:01.000Z'),
        session_thread_id: 'sthr_worker',
      },
      {
        ...toolResult('result_worker', 'tool_worker', '2026-08-27T08:00:05.000Z'),
        session_thread_id: 'sthr_worker',
      },
    ];
    expect(buildInspectorToolTotals(buildInspectorToolRows(closedAcrossThreads, null))).toMatchObject({
      executingMs: 7000,
      timeInToolsMs: 7000,
      waitingMs: 0,
    });
  });

  test('uses pinned agent permissions and leaves missing facts unknown', () => {
    const events = [toolUse('tool_bash', 'Bash', '2026-08-27T08:00:00.000Z')];

    expect(buildInspectorToolRows(events, null).find((row) => row.name === 'bash')?.permission).toBe('unknown');
    expect(
      buildInspectorToolRows(
        events,
        agentFixture({
          tools: [
            {
              type: 'agent_toolset_20260401',
              default_config: { permission_policy: { type: 'always_ask' } },
            },
          ],
        }),
      ).find((row) => row.name === 'bash')?.permission,
    ).toBe('ask');
  });

  test('separates configured built-in, custom, MCP, and unconfigured calls', () => {
    const agent = agentFixture({
      mcp_servers: [{ name: 'private_docs', url: 'https://docs.example.com/mcp' }],
      tools: [
        { type: 'agent_toolset_20260401' },
        { type: 'custom', name: 'review_change' },
        { type: 'mcp_toolset', mcp_server_name: 'private_docs' },
      ],
    });
    const rows = buildInspectorToolRows(
      [
        { ...toolUse('tool_mcp', 'mcp__private_docs__search', '2026-08-27T08:00:00.000Z'), type: 'agent.mcp_tool_use' },
        { ...toolUse('tool_unknown', 'Deploy', '2026-08-27T08:00:01.000Z'), type: 'agent.custom_tool_use' },
      ],
      agent,
    );

    expect(rows.find((row) => row.name === 'bash')).toMatchObject({
      configuredOn: 'Fixture agent',
      group: 'Built-in tools',
      kind: 'built-in',
    });
    expect(rows.find((row) => row.name === 'review_change')).toMatchObject({
      group: 'Custom tools',
      kind: 'custom',
    });
    expect(rows.find((row) => row.key === 'mcp__private_docs__search')).toMatchObject({
      configuredOn: 'Fixture agent',
      group: 'Private Docs',
      kind: 'mcp',
      permission: 'ask',
    });
    expect(rows.find((row) => row.name === 'deploy')).toMatchObject({
      group: 'Called, not configured',
      kind: 'unconfigured',
    });
  });
});

describe('session inspector cost', () => {
  test('builds monotonic cumulative cost snapshots with Claude usage fields', () => {
    const points = buildInspectorCostPoints([
      { id: 'user', type: 'user.message', created_at: '2026-08-27T08:00:00.000Z' },
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
        id: 'usage_stale',
        type: 'session.usage',
        processed_at: '2026-08-27T08:00:02.000Z',
        usage: { list_cost: { amount: '120', currency: 'USD' } },
      },
      { id: 'usage_missing_cost', type: 'session.usage', created_at: '2026-08-27T08:00:03.000Z', usage: {} },
    ]);

    expect(points).toEqual([
      {
        at: Date.parse('2026-08-27T08:00:01.000Z'),
        cents: 125,
        currency: 'USD',
        eventId: 'usage_first',
        stepCents: 125,
        usage: {
          activeSeconds: 2.5,
          cacheRead: 30,
          cacheWrite: 45,
          input: 100,
          output: 20,
          webSearches: 3,
        },
      },
      {
        at: Date.parse('2026-08-27T08:00:02.000Z'),
        cents: 125,
        currency: 'USD',
        eventId: 'usage_stale',
        stepCents: 0,
        usage: {
          activeSeconds: undefined,
          cacheRead: 0,
          cacheWrite: 0,
          input: 0,
          output: 0,
          webSearches: 0,
        },
      },
    ]);
    expect(inspectorSessionListCost({ list_cost: { amount: '125', currency: 'USD' } })).toEqual({
      amount: 1.25,
      currency: 'USD',
    });
    expect(inspectorSessionListCost({ list_cost: 0.0123 })).toEqual({ amount: 0.0123, currency: 'USD' });
  });
});

describe('session inspector thread context', () => {
  test('shows a rescheduled thread as rescheduling', () => {
    const rows = buildInspectorThreadRows(
      [{ id: 'child', label: 'Researcher' }],
      new Map([
        ['child', [{ id: 'rescheduled', type: 'session.thread_status_rescheduled' } satisfies QuickstartSessionEvent]],
      ]),
      true,
    );

    expect(rows[0]?.status).toBe('rescheduling');
  });

  test('uses only successful model request ends and includes cache input tokens', () => {
    const usage = {
      input_tokens: 100,
      output_tokens: 500,
      cache_read_input_tokens: 20,
      cache_creation_input_tokens: 5,
    };
    const points = buildInspectorContextPoints([
      { id: 'agent_usage', type: 'agent.message', processed_at: '2026-08-27T08:00:00.000Z', usage },
      {
        id: 'failed_model',
        type: 'span.model_request_end',
        processed_at: '2026-08-27T08:00:01.000Z',
        is_error: true,
        model_usage: usage,
      },
      {
        id: 'successful_model',
        type: 'span.model_request_end',
        processed_at: '2026-08-27T08:00:02.000Z',
        model_usage: usage,
      },
    ]);

    expect(points).toEqual([{ at: Date.parse('2026-08-27T08:00:02.000Z'), eventId: 'successful_model', tokens: 125 }]);
  });
});

function toolUse(id: string, name: string, createdAt: string): QuickstartSessionEvent {
  return {
    id,
    type: 'agent.tool_use',
    created_at: createdAt,
    name,
    input: {},
  };
}

function toolResult(id: string, toolUseId: string, createdAt: string): QuickstartSessionEvent {
  return {
    id,
    type: 'agent.tool_result',
    created_at: createdAt,
    tool_use_id: toolUseId,
    content: [],
  };
}

function modelStart(id: string): QuickstartSessionEvent {
  return { id, type: 'span.model_request_start' };
}

function modelEnd(id: string, startId: string): QuickstartSessionEvent {
  return { id, type: 'span.model_request_end', model_request_start_id: startId };
}

function agentFixture(overrides: Partial<AgentApiResponse>): AgentApiResponse {
  return {
    id: 'agent_fixture',
    archived_at: null,
    created_at: '2026-08-27T08:00:00.000Z',
    description: null,
    mcp_servers: [],
    metadata: {},
    model: 'claude-sonnet-4-6',
    multiagent: null,
    name: 'Fixture agent',
    skills: [],
    system: null,
    tools: [],
    type: 'agent',
    updated_at: '2026-08-27T08:00:00.000Z',
    version: 1,
    ...overrides,
  };
}
