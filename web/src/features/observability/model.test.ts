import { describe, expect, test } from 'bun:test';
import {
  clampedCustomRange,
  customRangeFieldErrors,
  customRangeIsValid,
  bucketIntervalMs,
  fillTimeseriesRows,
  formatDateTimeInput,
  interpolateSeriesValue,
  isPanelDataEmpty,
  mergeTimeseriesRows,
  nextHiddenSeries,
  panelIdFromSearch,
  panelVariablesFromFilters,
  parseDateTimeInput,
  pickDeclaredVariables,
  refreshedTimeRange,
  replaceDateKeepingTime,
  selectedMultistatName,
  multistatItemNames,
  plotLocalPoint,
  plotOverlayFrame,
  plotYAtValue,
  tabForPanelId,
  timeAtPlotX,
  timeAxisDomain,
  timeAxisTickOptions,
  timeseriesAllowsDecimalTicks,
  timeseriesDotVisible,
  timeseriesHoverCardAnchor,
  timeseriesTooltipSide,
  timeseriesValueDomain,
  toggleAgentVersion,
  traceIdFromSearch,
  zoomOutTimeRange,
} from './model';
import {
  connectorHeight,
  connectorOffset,
  durationLabelPlacement,
  resolveAgentName,
  serviceColorMap,
  showTreeConnector,
  spanAgentID,
  spanColorKey,
  spanIdentity,
  spanTokenTotal,
  TRACE_ROW_HEIGHT,
} from './traces/traceLayout';
import {
  buildTraceTree,
  filterCallTreeRows,
  flattenTraceTree,
  spanDisplayName,
  spanPreview,
  spanServiceName,
  tracePreview,
  traceStats,
  traceSummary,
  waterfallTickMs,
  waterfallTotalMs,
} from './traces/traceTree';
import type { ObservabilitySpan } from './types';

describe('observability model', () => {
  test('rejects custom ranges longer than 30 days', () => {
    expect(customRangeIsValid('2026-08-01T00:00:00.000Z', '2026-08-10T00:00:00.000Z')).toBe(true);
    expect(customRangeIsValid('2026-07-01T00:00:00.000Z', '2026-08-10T00:00:00.000Z')).toBe(false);
    expect(customRangeIsValid('2026-08-10T00:00:00.000Z', '2026-08-01T00:00:00.000Z')).toBe(false);
  });

  test('round-trips typed range timestamps through local time', () => {
    const typed = parseDateTimeInput('2026-07-14 16:56:03');
    expect(typed).not.toBeNull();
    expect(formatDateTimeInput(typed as Date)).toBe('2026-07-14 16:56:03');
    expect(formatDateTimeInput(parseDateTimeInput('2026-07-14 16:56') as Date)).toBe('2026-07-14 16:56:00');
    expect(formatDateTimeInput('not-a-date')).toBe('');
  });

  test('rejects malformed or overflowing typed timestamps', () => {
    expect(parseDateTimeInput('2026-07-14')).toBeNull();
    expect(parseDateTimeInput('2026-07-14 25:00:00')).toBeNull();
    expect(parseDateTimeInput('2026-02-31 10:00:00')).toBeNull();
    expect(parseDateTimeInput('yesterday')).toBeNull();
  });

  test('keeps the clock when the calendar moves a bound to another day', () => {
    expect(replaceDateKeepingTime('2026-08-13 10:36:07', new Date(2026, 7, 14), '00:00:00')).toBe(
      '2026-08-14 10:36:07',
    );
    expect(replaceDateKeepingTime('not-a-date', new Date(2026, 7, 14), '00:00:00')).toBe('2026-08-14 00:00:00');
    expect(replaceDateKeepingTime('2026-08-13T10:36:07', new Date(2026, 7, 14), '23:59:59')).toBe(
      '2026-08-14 10:36:07',
    );
  });

  test('attaches custom range errors to the failing bound', () => {
    const messages = { format: 'format', range: 'range' };
    expect(customRangeFieldErrors(null, new Date(2026, 7, 14, 10, 0, 0), messages)).toEqual({ start: 'format' });
    expect(customRangeFieldErrors(new Date(2026, 7, 14, 12, 0, 0), new Date(2026, 7, 14, 10, 0, 0), messages)).toEqual({
      end: 'range',
    });
    expect(customRangeFieldErrors(new Date(2026, 7, 13, 10, 0, 0), new Date(2026, 7, 14, 10, 0, 0), messages)).toEqual(
      {},
    );
  });

  test('re-anchors relative presets to the current clock on refresh and leaves custom ranges alone', () => {
    const stale = {
      preset: '1h' as const,
      start: '2026-08-13T00:00:00.000Z',
      end: '2026-08-13T01:00:00.000Z',
      agentId: 'agent_01',
      sessionId: '',
      agentVersions: [],
      models: [],
      tools: [],
      traceId: '',
      status: '' as const,
    };
    const now = Date.parse('2026-08-13T12:00:00.000Z');
    const refreshed = refreshedTimeRange(stale, now);
    expect(refreshed.start).toBe('2026-08-13T11:00:00.000Z');
    expect(refreshed.end).toBe('2026-08-13T12:00:00.000Z');
    expect(refreshed.agentId).toBe('agent_01');
    const custom = { ...stale, preset: 'custom' as const };
    expect(refreshedTimeRange(custom, now)).toBe(custom);
  });

  test('clamps a brushed range onto the dashboard clock and caps it at 30 days', () => {
    expect(clampedCustomRange('2026-08-13T10:00:00.000Z', '2026-08-13T10:00:00.000Z')).toBeNull();
    expect(clampedCustomRange('2026-08-13T12:00:00.000Z', '2026-08-13T10:00:00.000Z')).toEqual({
      preset: 'custom',
      start: '2026-08-13T10:00:00.000Z',
      end: '2026-08-13T12:00:00.000Z',
    });
    const clamped = clampedCustomRange('2026-06-01T00:00:00.000Z', '2026-08-10T00:00:00.000Z');
    expect(clamped?.preset).toBe('custom');
    expect(clamped?.start).toBe('2026-06-01T00:00:00.000Z');
    expect(Date.parse(clamped?.end ?? '') - Date.parse(clamped?.start ?? '')).toBe(30 * 24 * 60 * 60 * 1000);
  });

  test('zooms out by doubling the window without passing now or 30 days', () => {
    const now = Date.parse('2026-08-13T12:00:00.000Z');
    expect(zoomOutTimeRange('2026-08-13T10:00:00.000Z', '2026-08-13T11:00:00.000Z', now)).toEqual({
      preset: 'custom',
      start: '2026-08-13T09:30:00.000Z',
      end: '2026-08-13T11:30:00.000Z',
    });
    const wide = zoomOutTimeRange('2026-07-01T00:00:00.000Z', '2026-07-31T00:00:00.000Z', now);
    expect(Date.parse(wide.end) - Date.parse(wide.start)).toBe(30 * 24 * 60 * 60 * 1000);
    expect(Date.parse(wide.end)).toBeLessThanOrEqual(now);
  });

  test('keeps a sparse timeseries on the dashboard clock instead of collapsing to the data point', () => {
    const start = '2026-08-06T04:50:00.000Z';
    const end = '2026-08-13T04:50:00.000Z';
    expect(timeAxisDomain(start, end)).toEqual([Date.parse(start), Date.parse(end)]);
    expect(timeAxisTickOptions(Date.parse(end) - Date.parse(start))).toEqual({
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
    expect(timeAxisTickOptions(6 * 60 * 60 * 1000)).toEqual({ hour: '2-digit', minute: '2-digit' });
    expect(timeAxisTickOptions(30 * 24 * 60 * 60 * 1000)).toEqual({ month: 'short', day: 'numeric' });
    expect(timeAtPlotX(0, 700, Date.parse(start), Date.parse(end))).toBe(Date.parse(start));
    expect(timeAtPlotX(700, 700, Date.parse(start), Date.parse(end))).toBe(Date.parse(end));
    expect(timeAtPlotX(350, 700, Date.parse(start), Date.parse(end))).toBe(
      Date.parse(start) + (Date.parse(end) - Date.parse(start)) / 2,
    );
  });

  test('maps pointer coordinates onto the plot and rejects points outside it', () => {
    const plot = { left: 100, top: 40, width: 400, height: 160 };
    const host = { left: 80, top: 20 };
    expect(plotLocalPoint(90, 80, plot)).toBeNull();
    expect(plotLocalPoint(300, 20, plot)).toBeNull();
    expect(plotLocalPoint(300, 120, plot)).toEqual({ x: 200, y: 80 });
    expect(plotOverlayFrame(300, 120, host, plot)).toEqual({
      x: 200,
      y: 80,
      width: 400,
      height: 160,
      left: 20,
      top: 20,
    });
  });

  test('maps dashboard windows onto the same histogram bucket sizes as the server', () => {
    expect(bucketIntervalMs(15 * 60 * 1000)).toBe(30 * 1000);
    expect(bucketIntervalMs(60 * 60 * 1000)).toBe(60 * 1000);
    expect(bucketIntervalMs(6 * 60 * 60 * 1000)).toBe(5 * 60 * 1000);
    expect(bucketIntervalMs(24 * 60 * 60 * 1000)).toBe(30 * 60 * 1000);
    expect(bucketIntervalMs(7 * 24 * 60 * 60 * 1000)).toBe(3 * 60 * 60 * 1000);
    expect(bucketIntervalMs(30 * 24 * 60 * 60 * 1000)).toBe(12 * 60 * 60 * 1000);
  });

  test('zero-fills empty UTC histogram buckets so token charts do not invent a slope across idle days', () => {
    const start = Date.parse('2026-07-14T00:00:00.000Z');
    const end = Date.parse('2026-07-22T00:00:00.000Z');
    const sample = Date.parse('2026-07-15T00:00:00.000Z');
    const rows = fillTimeseriesRows([{ t: sample, input: 100, output: 20 }], ['input', 'output'], start, end, 'zero');
    const noon = Date.parse('2026-07-14T12:00:00.000Z');
    const after = Date.parse('2026-07-15T12:00:00.000Z');
    expect(rows).toHaveLength(16);
    expect(rows[0]).toEqual({ t: start, input: 0, output: 0 });
    expect(rows.find((row) => row.t === noon)).toEqual({ t: noon, input: 0, output: 0 });
    expect(rows.find((row) => row.t === sample)).toEqual({ t: sample, input: 100, output: 20 });
    expect(rows.find((row) => row.t === after)).toEqual({ t: after, input: 0, output: 0 });
    expect(interpolateSeriesValue(rows, 'input', Date.parse('2026-07-14T06:00:00.000Z'))).toBe(0);
  });

  test('breaks non-stacked series across empty buckets instead of connecting distant samples', () => {
    const start = Date.parse('2026-07-14T00:00:00.000Z');
    const end = Date.parse('2026-07-16T00:00:00.000Z');
    const left = Date.parse('2026-07-14T00:00:00.000Z');
    const right = Date.parse('2026-07-15T12:00:00.000Z');
    const rows = fillTimeseriesRows(
      [
        { t: left, p95: 40 },
        { t: right, p95: 80 },
      ],
      ['p95'],
      start,
      end,
      'gap',
    );
    const gap = Date.parse('2026-07-14T12:00:00.000Z');
    expect(rows.find((row) => row.t === gap)).toEqual({ t: gap, p95: null });
    expect(interpolateSeriesValue(rows, 'p95', Date.parse('2026-07-15T00:00:00.000Z'))).toBeNull();
    expect(interpolateSeriesValue(rows, 'p95', left)).toBe(40);
    expect(interpolateSeriesValue(rows, 'p95', right)).toBe(80);
  });

  test('places timeseries points on a numeric unix-ms axis', () => {
    const rows = mergeTimeseriesRows([
      {
        name: 'avg',
        points: [{ timestamp: '2026-08-06T09:00:00.000Z', value: 1500 }],
      },
      {
        name: 'p95',
        points: [{ timestamp: '2026-08-06T09:00:00.000Z', value: 3500 }],
      },
    ]);
    expect(rows).toEqual([{ t: Date.parse('2026-08-06T09:00:00.000Z'), avg: 1500, p95: 3500 }]);
  });

  test('interpolates series values between sparse samples so hover can sit on the line', () => {
    const left = Date.parse('2026-07-25T00:00:00.000Z');
    const right = Date.parse('2026-08-04T00:00:00.000Z');
    const rows = [
      { t: left, input: 10, output: 2 },
      { t: right, input: 30, output: 6 },
    ];
    const mid = left + (right - left) / 2;
    expect(interpolateSeriesValue(rows, 'input', left)).toBe(10);
    expect(interpolateSeriesValue(rows, 'input', mid)).toBe(20);
    expect(interpolateSeriesValue(rows, 'output', mid)).toBe(4);
    expect(interpolateSeriesValue(rows, 'input', left - 1)).toBeNull();
    expect(interpolateSeriesValue(rows, 'input', right + 1)).toBeNull();
    expect(interpolateSeriesValue(rows, 'cacheRead', mid)).toBeNull();
    expect(plotYAtValue(0, 0, 100, 200)).toBe(200);
    expect(plotYAtValue(100, 0, 100, 200)).toBe(0);
    expect(plotYAtValue(50, 0, 100, 200)).toBe(100);
    expect(timeseriesValueDomain(rows, ['input', 'output'], true)).toEqual([0, 36]);
    expect(timeseriesValueDomain(rows, ['input'], false)).toEqual([0, 30]);
    expect(timeseriesValueDomain([{ t: 1, errors: 0 }], ['errors'], false)).toEqual([0, 1]);
  });

  test('hides baseline dots so a zero series does not render clipped humps', () => {
    const real = new Set([1, 2]);
    expect(timeseriesDotVisible(1, 0, real)).toBe(false);
    expect(timeseriesDotVisible(1, 1e-9, real)).toBe(false);
    expect(timeseriesDotVisible(3, 4, real)).toBe(false);
    expect(timeseriesDotVisible(1, null, real)).toBe(false);
    expect(timeseriesDotVisible(1, 4, real)).toBe(true);
    expect(timeseriesAllowsDecimalTicks('count')).toBe(false);
    expect(timeseriesAllowsDecimalTicks('duration_ms')).toBe(true);
  });

  test('places the hover tooltip on the side that stays inside the plot', () => {
    expect(timeseriesTooltipSide(40, 400)).toBe('right');
    expect(timeseriesTooltipSide(280, 400)).toBe('left');
    expect(timeseriesTooltipSide(0, 0)).toBe('right');
  });

  test('anchors the hover card to remaining plot space instead of half the plot width', () => {
    const frame = { x: 40, left: 48, top: 8, width: 300, hostWidth: 360 };
    expect(timeseriesHoverCardAnchor(frame)).toEqual({
      side: 'right',
      top: 16,
      left: 100,
      right: 20,
    });
    expect(timeseriesHoverCardAnchor({ ...frame, x: 200 })).toEqual({
      side: 'left',
      top: 16,
      left: 56,
      right: 124,
    });
  });

  test('isolates a legend series on click and restores all on the second click', () => {
    const names = ['input', 'output', 'cacheRead'];
    const isolated = nextHiddenSeries(new Set(), 'output', names, true);
    expect([...isolated].sort()).toEqual(['cacheRead', 'input']);
    expect([...nextHiddenSeries(isolated, 'output', names, true)]).toEqual([]);
  });

  test('toggles a single legend series with modifier-click and keeps one visible', () => {
    const names = ['input', 'output'];
    const hidden = nextHiddenSeries(new Set(), 'input', names, false);
    expect([...hidden]).toEqual(['input']);
    expect([...nextHiddenSeries(hidden, 'output', names, false)]).toEqual(['input']);
    expect([...nextHiddenSeries(hidden, 'input', names, false)]).toEqual([]);
  });

  test('treats multistat empty unless items or sparkline points exist', () => {
    expect(isPanelDataEmpty(undefined, 'multistat')).toBe(true);
    expect(
      isPanelDataEmpty(
        {
          query_ref: 'overview.session_turns_percentiles',
          render_type: 'multistat',
          data_as_of: '',
          data: { items: [] },
        },
        'multistat',
      ),
    ).toBe(true);
    expect(
      isPanelDataEmpty(
        {
          query_ref: 'overview.session_turns_percentiles',
          render_type: 'multistat',
          data_as_of: '',
          data: { items: [], series: [{ name: 'p50', points: [{ timestamp: '2026-08-06T00:00:00Z', value: 3 }] }] },
        },
        'multistat',
      ),
    ).toBe(false);
    expect(
      isPanelDataEmpty(
        {
          query_ref: 'overview.session_turns_percentiles',
          render_type: 'multistat',
          data_as_of: '',
          data: { items: [{ name: 'p50', value: 2 }] },
        },
        'multistat',
      ),
    ).toBe(false);
    expect(selectedMultistatName([{ name: 'p95' }, { name: 'p50' }], 'p90')).toBe('p50');
    expect(selectedMultistatName([{ name: 'p95' }, { name: 'p50' }], 'p95')).toBe('p95');
    expect(selectedMultistatName([{ name: 'ok' }, { name: 'error' }], 'error')).toBe('error');
    expect(multistatItemNames([{ name: 'p95' }, { name: 'p50' }, { name: 'p90' }])).toEqual(['p50', 'p90', 'p95']);
    expect(multistatItemNames([{ name: 'error' }, { name: 'ok' }])).toEqual(['error', 'ok']);
  });

  test('omits empty optional variables', () => {
    const bound = pickDeclaredVariables(
      [
        { name: 'start_time', type: 'time', required: true },
        { name: 'end_time', type: 'time', required: true },
        { name: 'agent_id', type: 'string', required: false },
        { name: 'agent_version', type: 'int_list', required: false },
        { name: 'model', type: 'string_list', required: false },
      ],
      { start_time: '2026-08-12T00:00:00Z', end_time: '2026-08-13T00:00:00Z', agent_id: '', model: [] },
    );
    expect(bound).toEqual({
      start_time: '2026-08-12T00:00:00Z',
      end_time: '2026-08-13T00:00:00Z',
    });
  });

  test('sends selected agent versions and toggles the multi-select', () => {
    const filters = {
      preset: '24h' as const,
      start: '2026-08-12T00:00:00.000Z',
      end: '2026-08-13T00:00:00.000Z',
      agentId: '',
      sessionId: '',
      agentVersions: [3, 4],
      models: [],
      tools: [],
      traceId: '',
      status: '' as const,
    };
    expect(panelVariablesFromFilters(filters, { agentId: 'agent_01' }).agent_version).toEqual([3, 4]);
    expect(
      panelVariablesFromFilters({ ...filters, agentVersions: [] }, { agentId: 'agent_01' }).agent_version,
    ).toBeUndefined();
    expect(toggleAgentVersion([4], 3)).toEqual([4, 3]);
    expect(toggleAgentVersion([4, 3], 3)).toEqual([4]);
  });

  test('rejects empty and oversized trace ids from the query string', () => {
    expect(traceIdFromSearch(null)).toBeNull();
    expect(traceIdFromSearch('   ')).toBeNull();
    expect(traceIdFromSearch('abc')).toBe('abc');
    expect(traceIdFromSearch('x'.repeat(129))).toBeNull();
  });

  test('accepts dashboard panel ids from the query string', () => {
    expect(panelIdFromSearch(null)).toBeNull();
    expect(panelIdFromSearch('overview.token_trend')).toBe('overview.token_trend');
    expect(panelIdFromSearch('trace.trend.count')).toBe('trace.trend.count');
    expect(panelIdFromSearch('not a panel')).toBeNull();
    expect(panelIdFromSearch('x'.repeat(129))).toBeNull();
    expect(
      tabForPanelId(
        [
          { id: 'overview', panels: [{ id: 'overview.token_trend' }] },
          { id: 'model', panels: [{ id: 'model.ttft_avg' }] },
        ],
        'model.ttft_avg',
      ),
    ).toBe('model');
    expect(tabForPanelId([{ id: 'overview', panels: [] }], 'trace.trend.errors')).toBe('traces');
    expect(tabForPanelId([{ id: 'overview', panels: [] }], 'missing.panel')).toBeNull();
  });
});

describe('traceTree', () => {
  test('builds parent links, hangs orphans on a virtual root, and computes waterfall percents', () => {
    const spans: ObservabilitySpan[] = [
      span({ span_id: 'root', parent_span_id: '', start_time: '2026-08-12T10:00:00.000Z', duration_ms: 1000 }),
      span({ span_id: 'child', parent_span_id: 'root', start_time: '2026-08-12T10:00:00.200Z', duration_ms: 400 }),
      span({ span_id: 'lost', parent_span_id: 'missing', start_time: '2026-08-12T10:00:00.500Z', duration_ms: 100 }),
    ];
    const tree = buildTraceTree(spans);
    const flat = flattenTraceTree(tree);
    expect(flat.map((node) => node.span_id)).toEqual(['root', 'child', 'lost']);
    const child = flat.find((node) => node.span_id === 'child');
    expect(child?.offsetPct).toBeCloseTo(20, 0);
    expect(child?.widthPct).toBeCloseTo(40, 0);
    expect(flat.find((node) => node.span_id === 'lost')?.depth).toBe(0);
    expect(flat.find((node) => node.span_id === 'root')?.childCount).toBe(1);
    expect(flat.find((node) => node.span_id === 'child')?.isLast).toBe(true);
    expect(flat.find((node) => node.span_id === 'child')?.ancestorLast).toEqual([false]);
    expect(flat.find((node) => node.span_id === 'lost')?.isLast).toBe(true);
  });

  test('hides collapsed children and summarizes the trace', () => {
    const spans: ObservabilitySpan[] = [
      span({
        span_id: 'root',
        name: 'claude_code.interaction',
        start_time: '2026-08-12T10:00:00.000Z',
        duration_ms: 1000,
        attributes: { service_oma_session_id: 'sess-1', service_name: 'claude-code' },
      }),
      span({
        span_id: 'child',
        parent_span_id: 'root',
        name: 'claude_code.tool',
        start_time: '2026-08-12T10:00:00.200Z',
        duration_ms: 400,
        status: 'error',
      }),
      span({
        span_id: 'grand',
        parent_span_id: 'child',
        start_time: '2026-08-12T10:00:00.250Z',
        duration_ms: 100,
      }),
    ];
    const tree = buildTraceTree(spans);
    const collapsed = flattenTraceTree(tree, 0, new Set(['child']));
    expect(collapsed.map((node) => node.span_id)).toEqual(['root', 'child']);
    expect(filterCallTreeRows(flattenTraceTree(tree), 'tool').map((node) => node.span_id)).toEqual(['root', 'child']);
    expect(filterCallTreeRows(flattenTraceTree(tree), '').map((node) => node.span_id)).toEqual([
      'root',
      'child',
      'grand',
    ]);
    expect(filterCallTreeRows(flattenTraceTree(tree), 'missing')).toEqual([]);
    expect(traceSummary(spans)).toEqual({
      name: 'claude_code.interaction',
      startTime: '2026-08-12T10:00:00.000Z',
      durationMs: 1000,
      spanCount: 3,
      errorCount: 1,
      sessionId: 'sess-1',
    });
    expect(spanServiceName(spans[0])).toBe('claude-code');
    expect(waterfallTickMs(1000)).toEqual([0, 250, 500, 750, 1000]);
    expect(waterfallTotalMs([{ offsetMs: 200, durationMs: 400 }])).toBe(600);
  });

  test('derives input from the root prompt and output from the last successful llm span', () => {
    const spans: ObservabilitySpan[] = [
      span({
        span_id: 'root',
        kind: 'interaction',
        attributes: { user_prompt: 'hello' },
        start_time: '2026-08-12T10:00:00.000Z',
      }),
      span({
        span_id: 'llm1',
        kind: 'llm',
        status: 'ok',
        attributes: { response_model_output: 'first' },
        start_time: '2026-08-12T10:00:01.000Z',
      }),
      span({
        span_id: 'llm2',
        kind: 'llm',
        status: 'error',
        attributes: { response_model_output: 'bad' },
        start_time: '2026-08-12T10:00:02.000Z',
      }),
      span({
        span_id: 'llm3',
        kind: 'llm',
        status: 'ok',
        attributes: { response_model_output: 'final' },
        start_time: '2026-08-12T10:00:03.000Z',
      }),
    ];
    expect(tracePreview(spans)).toEqual({ input: 'hello', output: 'final' });
  });

  test('names spans by tool, model, or shortened operation', () => {
    expect(spanDisplayName(span({ kind: 'tool', name: 'claude_code.tool', attributes: { tool_name: 'Read' } }))).toBe(
      'Read',
    );
    expect(
      spanDisplayName(
        span({
          kind: 'tool_execution',
          name: 'claude_code.tool.execution',
          attributes: { tool_name: 'Bash' },
        }),
      ),
    ).toBe('Bash');
    expect(
      spanDisplayName(span({ kind: 'llm', name: 'claude_code.llm_request', attributes: { model: 'sonnet-4' } })),
    ).toBe('sonnet-4');
    expect(spanDisplayName(span({ kind: 'interaction', name: 'claude_code.interaction' }))).toBe('interaction');
  });

  test('returns empty stats for a trace without spans', () => {
    expect(traceStats([])).toEqual({
      durationMs: 0,
      spanCount: 0,
      llmCallCount: 0,
      toolCallCount: 0,
      inputTokens: 0,
      outputTokens: 0,
      totalTokens: 0,
      cacheHitRate: null,
      errorCount: 0,
    });
  });

  test('aggregates duration, calls, tokens, cache hit rate, and errors across spans', () => {
    const spans: ObservabilitySpan[] = [
      span({
        span_id: 'root',
        kind: 'interaction',
        start_time: '2026-08-12T10:00:00.000Z',
        duration_ms: 1000,
      }),
      span({
        span_id: 'llm1',
        parent_span_id: 'root',
        kind: 'llm',
        start_time: '2026-08-12T10:00:00.100Z',
        duration_ms: 400,
        attributes: { input_tokens: '100', output_tokens: '50', cache_read_tokens: '300', cache_creation_tokens: '10' },
      }),
      span({
        span_id: 'tool1',
        parent_span_id: 'root',
        kind: 'tool',
        status: 'error',
        start_time: '2026-08-12T10:00:00.600Z',
        duration_ms: 900,
      }),
    ];
    expect(traceStats(spans)).toEqual({
      durationMs: 1500,
      spanCount: 3,
      llmCallCount: 1,
      toolCallCount: 1,
      inputTokens: 100,
      outputTokens: 50,
      totalTokens: 460,
      cacheHitRate: 75,
      errorCount: 1,
    });
  });

  test('reads per-kind input and output from attributes or events', () => {
    expect(spanPreview(span({ kind: 'interaction', attributes: { user_prompt: ' hi ' } }))).toEqual({
      input: 'hi',
      output: '',
    });
    expect(spanPreview(span({ kind: 'llm', attributes: { prompt: 'q', response_model_output: 'a' } }))).toEqual({
      input: 'q',
      output: 'a',
    });
    expect(
      spanPreview(
        span({
          kind: 'tool',
          events: [
            { name: 'tool.input', timestamp: '', attributes: { input: '{"path":"a"}' } },
            { name: 'tool.output', timestamp: '', attributes: { output: 'ok' } },
          ],
        }),
      ),
    ).toEqual({ input: '{"path":"a"}', output: 'ok' });
    expect(
      spanPreview(
        span({
          kind: 'tool_execution',
          attributes: { tool_input: 'ls', tool_result: 'file.txt' },
        }),
      ),
    ).toEqual({ input: 'ls', output: 'file.txt' });
  });

  test('keeps matching ancestors when searching by tool name', () => {
    const spans: ObservabilitySpan[] = [
      span({ span_id: 'root', name: 'claude_code.interaction', start_time: '2026-08-12T10:00:00.000Z' }),
      span({
        span_id: 'child',
        parent_span_id: 'root',
        kind: 'tool',
        name: 'claude_code.tool',
        attributes: { tool_name: 'Read' },
        start_time: '2026-08-12T10:00:00.200Z',
      }),
    ];
    expect(filterCallTreeRows(flattenTraceTree(buildTraceTree(spans)), 'read').map((row) => row.span_id)).toEqual([
      'root',
      'child',
    ]);
  });
});

describe('traceLayout', () => {
  test('colors by inferred service or kind, and places duration labels like OpenObserve', () => {
    expect(spanIdentity(span({ attributes: { service_name: 'claude-code' } }))).toBe('claude-code');
    expect(spanColorKey(span({ kind: 'llm', attributes: { service_name: 'claude-code' } }))).toBe('llm');
    expect(
      spanColorKey(span({ kind: 'llm', attributes: { infer_service_name: 'anthropic', service_name: 'claude-code' } })),
    ).toBe('anthropic');
    expect(spanTokenTotal(span({ attributes: { input_tokens: '10', output_tokens: '5' } }))).toBe(15);
    const colors = serviceColorMap(['llm', 'tool', 'llm']);
    expect(colors.get('llm')).toBe('var(--chart-1)');
    expect(colors.get('tool')).toBe('var(--chart-2)');
    expect(durationLabelPlacement(10, 20, 400)).toEqual({ top: '0.625rem', left: '120px' });
    expect(durationLabelPlacement(80, 15, 400)).toEqual({ top: '0.125rem', right: 0 });
    expect(durationLabelPlacement(60, 10, 400).left).toBe('180px');
  });

  test('draws OpenObserve tree connectors at the badge column', () => {
    expect(connectorOffset(2, 1)).toBe(30);
    expect(connectorOffset(2, 2)).toBe(15);
    expect(showTreeConnector([false, true], 2, 1)).toBe(true);
    expect(showTreeConnector([false, true], 2, 2)).toBe(false);
    expect(connectorHeight(true, 1)).toBe(TRACE_ROW_HEIGHT / 2);
    expect(connectorHeight(false, 1)).toBe(TRACE_ROW_HEIGHT);
  });

  test('reads injected agent id and resolves the catalog name', () => {
    expect(spanAgentID(span({ attributes: { service_oma_agent_id: 'agent_01' } }))).toBe('agent_01');
    expect(spanAgentID(span({ attributes: { 'oma.agent.id': 'agent_02' } }))).toBe('agent_02');
    expect(spanAgentID(span({}))).toBe('');
    const names = new Map([['agent_01', 'Support bot']]);
    expect(resolveAgentName('agent_01', names)).toBe('Support bot');
    expect(resolveAgentName('agent_missing', names)).toBe('agent_missing');
    expect(resolveAgentName('', names)).toBe('');
  });
});

function span(partial: Partial<ObservabilitySpan>): ObservabilitySpan {
  return {
    span_id: 'span',
    parent_span_id: '',
    kind: 'other',
    name: 'span',
    start_time: '2026-08-12T10:00:00.000Z',
    end_time: '2026-08-12T10:00:01.000Z',
    duration_ms: 10,
    status: 'ok',
    attributes: {},
    ...partial,
  };
}
